package main

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/codeskyblue/goreq"
	"github.com/codeskyblue/heartbeat"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-server/proto"
	"github.com/gorilla/websocket"
	"context"
)

var currentDeviceInfo *proto.DeviceInfo

func getDeviceInfo() *proto.DeviceInfo {
	if currentDeviceInfo != nil {
		return currentDeviceInfo
	}
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
	devInfo.Port = listenPort

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
	return currentDeviceInfo
}

type versionResponse struct {
	ServerVersion string `json:"version"`
	AgentVersion  string `json:"atx-agent"`
}

type TunnelProxy struct {
	ServerNetAddr *NetAddr
	Secret     string

	udid string
}


func (t *TunnelProxy) Heartbeat(){
	if t.ServerNetAddr == nil {
		return
	}
	targetUrl := t.ServerNetAddr.WebSocketAddr("/websocket/heartbeat")
	c, _, err := websocket.DefaultDialer.DialContext(context.TODO(), targetUrl, nil)
	if err != nil {
		log.Printf("WebSocket dial error: %v", err)
		return
	}
	defer c.Close()

	ip, err := GetOutboundIP()
	if err != nil {
		log.Printf("OutboundIP: %v", ip)
		return
	}
	c.WriteJSON(map[string]interface{}{
		"command": "handshake",
		"name": "atx-agent",
		"owner": nil,
		"secret": "",
		"url": "http://"+ip.String()+":7912",
		"priority": 1,
	})
	var resp struct {
		Success bool `json:"success"`
		ID string `json:"id"`
		Description string `json:"description"`
	}
	c.ReadJSON(&resp)
	if !resp.Success {
		log.Println("HB handshake err:", resp.Description)
		return
	}
	log.Println("HB ID:", resp.ID)
}

// Need test. Connect with server use github.com/codeskyblue/heartbeat
func (t *TunnelProxy) Heartbeat_old() {
	dinfo := getDeviceInfo()
	t.udid = dinfo.Udid
	client := &heartbeat.Client{
		Secret:     t.Secret,
		ServerAddr: t.ServerNetAddr.HTTPAddr("/heartbeat"),
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
	res, err := goreq.Request{Uri: t.ServerNetAddr.HTTPAddr("/version")}.Do()
	if err != nil {
		return err
	}
	defer res.Body.Close()
	verResp := new(versionResponse)
	if err := res.Body.FromJsonTo(verResp); err != nil {
		return err
	}
	log.Println("Disable upgrade, until code fixed")

	// if verResp.AgentVersion != version {
	// 	if version == "dev" {
	// 		log.Printf("dev version, skip version upgrade")
	// 	} else {
	// 		log.Printf("server require agent version: %v, but current %s, going to upgrade", verResp.AgentVersion, version)
	// 		if err := doUpdate(verResp.AgentVersion); err != nil {
	// 			log.Printf("upgrade error: %v", err)
	// 			return err
	// 		}
	// 		log.Printf("restarting server")
	// os.Setenv(daemon.MARK_NAME, daemon.MARK_VALUE+":reset")
	// 		runDaemon()
	// 		os.Exit(0)
	// 	}
	// }
	return nil
}

func (t *TunnelProxy) UpdateInfo(devInfo *proto.DeviceInfo) error {
	res, err := goreq.Request{
		Method: "POST",
		Uri:    t.ServerNetAddr.HTTPAddr("/devices/", t.udid, "/info"),
		Body:   devInfo,
	}.Do()
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}
