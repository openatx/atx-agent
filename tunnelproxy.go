package main

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openatx/androidutils"
)

var currentDeviceInfo *DeviceInfo

func getDeviceInfo() *DeviceInfo {
	if currentDeviceInfo != nil {
		return currentDeviceInfo
	}
	devInfo := &DeviceInfo{
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
		devInfo.Memory = &MemoryInfo{
			Total:  total,
			Around: fmt.Sprintf("%d GB", around),
		}
	}

	hardware, processors, err := androidutils.ProcessorInfo()
	if err != nil {
		log.Println("get cpuinfo error:", err)
	} else {
		devInfo.Cpu = &CpuInfo{
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

// type versionResponse struct {
// 	ServerVersion string `json:"version"`
// 	AgentVersion  string `json:"atx-agent"`
// }

// type TunnelProxy struct {
// 	ServerAddr string
// 	Secret     string

// 	udid string
// }

// // Need test. Connect with server use github.com/codeskyblue/heartbeat
// func (t *TunnelProxy) Heratbeat() {
// 	dinfo := getDeviceInfo()
// 	t.udid = dinfo.Udid
// 	client := &heartbeat.Client{
// 		Secret:     t.Secret,
// 		ServerAddr: "http://" + t.ServerAddr + "/heartbeat",
// 		Identifier: t.udid,
// 	}
// 	lostCnt := 0
// 	client.OnConnect = func() {
// 		lostCnt = 0
// 		t.checkUpdate()
// 		// send device info on first connect
// 		dinfo.Battery.Update()
// 		if err := t.UpdateInfo(dinfo); err != nil {
// 			log.Println("Update info:", err)
// 		}
// 	}
// 	client.OnError = func(err error) {
// 		if lostCnt == 0 {
// 			// open identify to make WIFI reconnected when disconnected
// 			runShellTimeout(time.Minute, "am", "start", "-n", "com.github.uiautomator/.IdentifyActivity")
// 		}
// 		lostCnt++
// 	}
// 	// send heartbeat to server every 10s
// 	client.Beat(10 * time.Second)
// }

// func (t *TunnelProxy) checkUpdate() error {
// 	res, err := goreq.Request{Uri: "http://" + t.ServerAddr + "/version"}.Do()
// 	if err != nil {
// 		return err
// 	}
// 	defer res.Body.Close()
// 	verResp := new(versionResponse)
// 	if err := res.Body.FromJsonTo(verResp); err != nil {
// 		return err
// 	}
// 	log.Println("Disable upgrade, until code fixed")

// 	// if verResp.AgentVersion != version {
// 	// 	if version == "dev" {
// 	// 		log.Printf("dev version, skip version upgrade")
// 	// 	} else {
// 	// 		log.Printf("server require agent version: %v, but current %s, going to upgrade", verResp.AgentVersion, version)
// 	// 		if err := doUpdate(verResp.AgentVersion); err != nil {
// 	// 			log.Printf("upgrade error: %v", err)
// 	// 			return err
// 	// 		}
// 	// 		log.Printf("restarting server")
// 	// os.Setenv(daemon.MARK_NAME, daemon.MARK_VALUE+":reset")
// 	// 		runDaemon()
// 	// 		os.Exit(0)
// 	// 	}
// 	// }
// 	return nil
// }

// func (t *TunnelProxy) UpdateInfo(devInfo *DeviceInfo) error {
// 	res, err := goreq.Request{
// 		Method: "POST",
// 		Uri:    "http://" + t.ServerAddr + "/devices/" + t.udid + "/info",
// 		Body:   devInfo,
// 	}.Do()
// 	if err != nil {
// 		return err
// 	}
// 	res.Body.Close()
// 	return nil
// }

type WSClient struct {
	// The websocket connection.
	conn         *websocket.Conn
	cancelFunc   context.CancelFunc
	changeEventC chan interface{}

	host    string
	udid    string
	serial  string
	brand   string
	model   string
	version string
	ip      string
}

func (c *WSClient) RunForever() {
	if c.changeEventC == nil {
		c.changeEventC = make(chan interface{}, 0)
	}
	n := 0
	for {
		if c.host == "" {
			<-c.changeEventC
		}
		start := time.Now()
		err := c.Run()
		if time.Since(start) > 10*time.Second {
			n = 0
		}
		n++
		if n > 20 {
			n = 20
		}
		waitDuration := 3*time.Second + time.Duration(n)*time.Second
		log.Println("wait", waitDuration, "error", err)
		select {
		case <-time.After(waitDuration):
		case <-c.changeEventC:
			log.Println("wait canceled")
		}
	}
}

func (c *WSClient) ChangeHost(host string) {
	c.host = host
	if c.changeEventC != nil {
		c.cancelFunc()
		c.conn.Close()
		select {
		case c.changeEventC <- nil:
		case <-time.After(1 * time.Second):
		}
	}
}

func (client *WSClient) Run() error {
	u := url.URL{Scheme: "ws", Host: client.host, Path: "/websocket/heartbeat"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	client.cancelFunc = cancel
	defer cancel()

	log.Println("Remote:", u.String())
	c, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	client.conn = c
	defer c.Close()

	c.WriteJSON(map[string]interface{}{
		"command":  "handshake",
		"name":     "phone",
		"owner":    nil,
		"secret":   "",
		"url":      client.ip + ":7912",
		"priority": 1,
	})

	var response WSResponse
	if err = c.ReadJSON(&response); err != nil {
		log.Fatal(err)
	}
	if !response.Success {
		log.Fatal(response.Description)
	}

	log.Println("update android device")
	c.WriteJSON(map[string]interface{}{
		"command":  "update",
		"platform": "android",
		"udid":     client.udid,
		"properties": map[string]string{
			"serial":  client.serial,  // ro.serialno
			"brand":   client.brand,   // ro.product.brand
			"model":   client.model,   // ro.product.model
			"version": client.version, // ro.build.version.release
		},
		"provider": map[string]string{
			"atxAgentAddress":      client.ip + ":7912",
			"remoteConnectAddress": client.ip + ":5555",
			"whatsInputAddress":    client.ip + ":6677",
		},
	})

	for {
		response = WSResponse{}
		err = c.ReadJSON(&response)
		if err != nil {
			log.Println("read:", err)
			return err
		}
		if response.Command == "release" {
			c.WriteJSON(map[string]interface{}{
				"command": "update",
				"udid":    client.udid,
				"colding": false,
			})
		}
	}
}

type WSResponse struct {
	Success     bool   `json:"success"`
	Description string `json:"description"`
	Command     string `json:"command"`
	Udid        string `json:"udid"`
}
