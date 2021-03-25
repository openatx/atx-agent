package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
)

type toucher struct {
	width, height int
	rotation      int
}

type TouchRequest struct {
	Operation    string  `json:"operation"` // d, m, u
	Index        int     `json:"index"`
	PercentX     float64 `json:"xP"`
	PercentY     float64 `json:"yP"`
	Milliseconds int     `json:"milliseconds"`
	Pressure     float64 `json:"pressure"`
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

	log.Debugf("handle touch requests maxX:%d maxY:%d maxPressure:%d maxContacts:%d", maxX, maxY, maxPressure, maxContacts)
	go io.Copy(ioutil.Discard, conn) // ignore the rest output
	var posX, posY int
	for req := range reqC {
		var err error
		switch req.Operation {
		case "r": // reset
			_, err = conn.Write([]byte("r\n"))
		case "d":
			fallthrough
		case "m":
			posX = int(req.PercentX * float64(maxX))
			posY = int(req.PercentY * float64(maxY))
			pressure := int(req.Pressure * float64(maxPressure))
			if pressure == 0 {
				pressure = maxPressure - 1
			}
			line := fmt.Sprintf("%s %d %d %d %d\n", req.Operation, req.Index, posX, posY, pressure)
			log.Debugf("write to @minitouch %v", line)
			_, err = conn.Write([]byte(line))
		case "u":
			_, err = conn.Write([]byte(fmt.Sprintf("u %d\n", req.Index)))
		case "c":
			_, err = conn.Write([]byte("c\n"))
		case "w":
			_, err = conn.Write([]byte(fmt.Sprintf("w %d\n", req.Milliseconds)))
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
