package main

import (
	"io"
	"net/http"
)

func handleTerminalWebsocket(w http.ResponseWriter, r *http.Request) {
	// for k, v := range r.Header {
	// 	log.Println(k, v)
	// }
	io.WriteString(w, "not support windows")
}
