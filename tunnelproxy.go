package main

import (
	"log"
	"time"

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

func unsafeRunTunnelProxy(serverAddr string) error {
	ws, _, err := websocket.DefaultDialer.Dial("ws://"+serverAddr+"/echo", nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	log.Printf("server connected")

	props, _ := androidutils.Properties()
	devInfo := &proto.DeviceInfo{
		Serial:       props["ro.serialno"],
		Brand:        props["ro.product.brand"],
		Model:        props["ro.product.model"],
		AgentVersion: version,
	}
	devInfo.HWAddr, _ = androidutils.HWAddrWLAN()

	// Udid is ${Serial}-${MacAddress}-${model}
	udid := props["ro.serialno"] + "-" + devInfo.HWAddr + "-" + props["ro.product.model"]
	devInfo.Udid = udid

	ws.WriteJSON(proto.CommonMessage{
		Type: proto.DeviceInfoMessage,
		Data: devInfo,
	})

	ws.SetPingHandler(func(string) error {
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
