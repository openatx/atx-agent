package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"

	log "github.com/sirupsen/logrus"
)

type toucher struct {
	width, height int
	rotation      int
}

type TouchRequest struct {
	Operation string  `json:"operation"` // d, m, u
	Index     int     `json:"index"`
	PercentX  float64 `json:"pX"`
	PercentY  float64 `json:"pY"`
	Pressure  int     `json:"pressure"`
}

// coord(0, 0) is always left-top conner, no matter the rotation changes
func drainTouchRequests(conn net.Conn, reqC chan TouchRequest) error {
	var maxX, maxY int
	var flag string
	var ver int
	var maxContacts, maxPressure int
	var pid int

	lineRd := lineFormatReader{bufrd: bufio.NewReader(conn)}
	lineRd.Scanf("%s %d", &flag, &ver)
	lineRd.Scanf("%s %d %d %d %d", &flag, &maxContacts, &maxX, &maxY, &maxPressure)
	if err := lineRd.Scanf("%s %d", &flag, &pid); err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"maxX":        maxX,
		"maxY":        maxY,
		"maxPressure": maxPressure,
		"maxContacts": maxContacts,
	}).Info("handle touch requests")
	go io.Copy(ioutil.Discard, conn) // ignore the rest output
	var posX, posY int
	for req := range reqC {
		var err error
		switch req.Operation {
		case "d":
			fallthrough
		case "m":
			posX = int(req.PercentX * float64(maxX))
			posY = int(req.PercentY * float64(maxY))
			if req.Pressure == 0 {
				req.Pressure = 50
			}
			line := fmt.Sprintf("%s %d %d %d %d\n", req.Operation, req.Index, posX, posY, req.Pressure)
			log.WithFields(log.Fields{
				"touch":      req,
				"remoteAddr": conn.RemoteAddr(),
			}).Debug("write to @minitouch", line)
			_, err = conn.Write([]byte(line))
		case "u":
			_, err = conn.Write([]byte(fmt.Sprintf("u %d\n", req.Index)))
		case "c":
			_, err = conn.Write([]byte("c\n"))
		default:
			err = errors.New("unsupported operation: " + req.Operation)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

type lineFormatReader struct {
	bufrd *bufio.Reader
	err   error
}

func (r *lineFormatReader) Scanf(format string, args ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	var line []byte
	line, _, r.err = r.bufrd.ReadLine()
	if r.err != nil {
		return r.err
	}
	_, r.err = fmt.Sscanf(string(line), format, args...)
	return r.err
}
