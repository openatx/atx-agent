package main

import (
	"io"
	"net/http"
)

func handleTerminalWebsocket(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "not support windows")
}
