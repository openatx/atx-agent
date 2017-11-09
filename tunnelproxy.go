package main

import (
	"log"
	"os"
	"time"

	"github.com/franela/goreq"
	"github.com/gorilla/websocket"
	"github.com/openatx/androidutils"
	"github.com/openatx/atx-server/proto"
)

func runTunnelProxy(serverAddr string) {
	var t time.Time
	n := 0
	for {
		t = time.Now()
		unsafeRunTunnelProxy(serverAddr)
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

func getDeviceInfo() *proto.DeviceInfo {
	devInfo := &proto.DeviceInfo{
		Serial:       getProperty("ro.serialno"),
		Brand:        getProperty("ro.product.brand"),
		Model:        getProperty("ro.product.model"),
		AgentVersion: version,
	}
	devInfo.HWAddr, _ = androidutils.HWAddrWLAN()

	// Udid is ${Serial}-${MacAddress}-${model}
	udid := getProperty("ro.serialno") + "-" + devInfo.HWAddr + "-" + getProperty("ro.product.model")
	devInfo.Udid = udid
	return devInfo
}

type VersionResponse struct {
	ServerVersion string `json:"version"`
	AgentVersion  string `json:"atx-agent"`
}

func unsafeRunTunnelProxy(serverAddr string) error {
	// check version update
	res, err := goreq.Request{Uri: "http://" + serverAddr + "/version"}.Do()
	if err != nil {
		return err
	}
	defer res.Body.Close()
	verResp := new(VersionResponse)
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

	// keep connection with server
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+serverAddr+"/echo", nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	log.Printf("server connected")

	devInfo := getDeviceInfo()
	ws.WriteJSON(proto.CommonMessage{
		Type: proto.DeviceInfoMessage,
		Data: devInfo,
	})

	// when network switch, connection still exists, but no ping comes
	// server ping interval now is 10s
	const wsReadWait = 60 * time.Second
	ws.SetReadDeadline(time.Now().Add(wsReadWait))
	ws.SetPingHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(wsReadWait))
		ws.WriteMessage(websocket.PongMessage, []byte{})
		return nil
	})

	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			return err
		}
	}
}
