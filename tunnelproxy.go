package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codeskyblue/heartbeat"
	"github.com/franela/goreq"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-server/proto"
)

var currentDeviceInfo *proto.DeviceInfo

func getDeviceInfo() *proto.DeviceInfo {
	if currentDeviceInfo == nil {
		devInfo := &proto.DeviceInfo{
			Serial:       getCachedProperty("ro.serialno"),
			Brand:        getCachedProperty("ro.product.brand"),
			Model:        getCachedProperty("ro.product.model"),
			Version:      getCachedProperty("ro.build.version.release"),
			AgentVersion: version,
		}
		devInfo.Sdk, _ = strconv.Atoi(getCachedProperty("ro.build.version.sdk"))
		devInfo.HWAddr, _ = androidutils.HWAddrWLAN()
		display, _ := androidutils.WindowSize()
		devInfo.Display = &display
		battery := androidutils.Battery{}
		battery.Update()
		devInfo.Battery = &battery

		memory, err := androidutils.MemoryInfo()
		if err != nil {
			log.Println("get memory error:", err)
		} else {
			total := memory["MemTotal"]
			around := int(math.Ceil(float64(total-512*1024) / 1024.0 / 1024.0)) // around GB
			devInfo.Memory = &proto.MemoryInfo{
				Total:  total,
				Around: fmt.Sprintf("%d GB", around),
			}
		}

		hardware, processors, err := androidutils.ProcessorInfo()
		if err != nil {
			log.Println("get cpuinfo error:", err)
		} else {
			devInfo.Cpu = &proto.CpuInfo{
				Hardware: hardware,
				Cores:    len(processors),
			}
		}

		// Udid is ${Serial}-${MacAddress}-${model}
		udid := fmt.Sprintf("%s-%s-%s",
			getCachedProperty("ro.serialno"),
			devInfo.HWAddr,
			strings.Replace(getCachedProperty("ro.product.model"), " ", "_", -1))
		devInfo.Udid = udid
		currentDeviceInfo = devInfo
	}
	return currentDeviceInfo
}

type versionResponse struct {
	ServerVersion string `json:"version"`
	AgentVersion  string `json:"atx-agent"`
}

type TunnelProxy struct {
	ServerAddr string
	Secret     string

	udid string
}

// Need test. Connect with server use github.com/codeskyblue/heartbeat
func (t *TunnelProxy) Heratbeat() {
	dinfo := getDeviceInfo()
	t.udid = dinfo.Udid
	client := &heartbeat.Client{
		Secret:     t.Secret,
		ServerAddr: "http://" + t.ServerAddr + "/heartbeat",
		Identifier: t.udid,
	}
	lostCnt := 0
	client.OnConnect = func() {
		lostCnt = 0
		t.checkUpdate()
		// send device info on first connect
		dinfo.Battery.Update()
		if err := t.UpdateInfo(dinfo); err != nil {
			log.Println("Update info:", err)
		}
	}
	client.OnError = func(err error) {
		if lostCnt == 0 {
			// open identify to make WIFI reconnected when disconnected
			runShellTimeout(time.Minute, "am", "start", "-n", "com.github.uiautomator/.IdentifyActivity")
		}
		lostCnt++
	}
	// send heartbeat to server every 10s
	client.Beat(10 * time.Second)
}

func (t *TunnelProxy) checkUpdate() error {
	res, err := goreq.Request{Uri: "http://" + t.ServerAddr + "/version"}.Do()
	if err != nil {
		return err
	}
	defer res.Body.Close()
	verResp := new(versionResponse)
	if err := res.Body.FromJsonTo(verResp); err != nil {
		return err
	}
	if verResp.AgentVersion != version {
		if version == "dev" {
			log.Printf("dev version, skip version upgrade")
		} else {
			log.Printf("server require agent version: %v, but current %s, going to upgrade", verResp.AgentVersion, version)
			if err := doUpdate(verResp.AgentVersion); err != nil {
				log.Printf("upgrade error: %v", err)
				return err
			}
			log.Printf("restarting server")
			runDaemon()
			os.Exit(0)
		}
	}
	return nil
}

func (t *TunnelProxy) UpdateInfo(devInfo *proto.DeviceInfo) error {
	res, err := goreq.Request{
		Method: "POST",
		Uri:    "http://" + t.ServerAddr + "/devices/" + t.udid + "/info",
		Body:   devInfo,
	}.Do()
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}
