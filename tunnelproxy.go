package main

import (
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/franela/goreq"
	"github.com/gorilla/websocket"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-server/proto"
)

var currentDeviceInfo *proto.DeviceInfo

func getDeviceInfo() *proto.DeviceInfo {
	if currentDeviceInfo == nil {
		devInfo := &proto.DeviceInfo{
			Serial:       getProperty("ro.serialno"),
			Brand:        getProperty("ro.product.brand"),
			Model:        getProperty("ro.product.model"),
			AgentVersion: version,
		}
		devInfo.HWAddr, _ = androidutils.HWAddrWLAN()
		battery := androidutils.Battery{}
		battery.Update()
		devInfo.Battery = battery

		// Udid is ${Serial}-${MacAddress}-${model}
		udid := getProperty("ro.serialno") + "-" + devInfo.HWAddr + "-" + strings.Replace(getProperty("ro.product.model"), " ", "_", -1)
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
	connected  bool
	udid       string
}

func (this *TunnelProxy) RunForever() {
	var t time.Time
	n := 0
	for {
		t = time.Now()
		this.run()
		// unsafeRunTunnelProxy(serverAddr)
		if time.Since(t) > time.Minute {
			n = 0
		}
		n++
		if n > 5 {
			n = 5
		}
		log.Printf("dial server error, reconnect after %d seconds", n*5)
		time.Sleep(time.Duration(n) * 5 * time.Second) // 5, 10, ... 50s
	}
}

func (t *TunnelProxy) run() (err error) {
	// check version update
	if err = t.checkUpdate(); err != nil {
		return err
	}
	// keep connection with server
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+t.ServerAddr+"/echo", nil)
	if err != nil {
		return err
	}
	defer func() {
		ws.Close()
		t.connected = false
	}()
	log.Printf("server connected")

	// when network switch, connection still exists, but no ping comes
	const wsReadWait = 60 * time.Second
	const wsWriteWait = 60 * time.Second

	devInfo := getDeviceInfo()
	t.udid = devInfo.Udid

	ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
	ws.WriteJSON(proto.CommonMessage{
		Type: proto.DeviceInfoMessage,
		Data: devInfo,
	})

	// server ping interval now is 10s
	log.Println("set ping handler and read/write deadline")
	ws.SetReadDeadline(time.Now().Add(wsReadWait))
	ws.SetPingHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(wsReadWait))
		// set write deadline on each write to prevent network issue
		ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
		ws.WriteMessage(websocket.PongMessage, []byte{})
		return nil
	})

	t.connected = true // set connected status
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			return err
		}
	}
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

// return if successfully updated
func (t *TunnelProxy) UpdateInfo(devInfo *proto.DeviceInfo) error {
	if !t.connected {
		return errors.New("tunnel is not established")
	}
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
