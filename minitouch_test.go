package main

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MockConn struct {
	buffer *bytes.Buffer
}

func (c *MockConn) Read(b []byte) (n int, err error) {
	return c.buffer.Read(b)
}

func (c *MockConn) Write(b []byte) (n int, err error) {
	return c.buffer.Write(b)
}

func (c *MockConn) Close() error                       { return nil }
func (c *MockConn) LocalAddr() net.Addr                { return nil }
func (c *MockConn) RemoteAddr() net.Addr               { return nil }
func (c *MockConn) SetDeadline(t time.Time) error      { return nil }
func (c *MockConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *MockConn) SetWriteDeadline(t time.Time) error { return nil }

func TestDrainTouchRequests(t *testing.T) {
	reqC := make(chan TouchRequest, 0)
	conn := &MockConn{
		buffer: bytes.NewBuffer(nil),
	}
	err := drainTouchRequests(conn, reqC)
	assert.Error(t, err)

	conn = &MockConn{
		buffer: bytes.NewBufferString(`v 1
^ 10 1080 1920 255
$ 25654`),
	}
	reqC = make(chan TouchRequest, 4)
	reqC <- TouchRequest{
		Operation: "d",
		Index:     1,
		PercentX:  1.0,
		PercentY:  1.0,
		Pressure:  1,
	}
	reqC <- TouchRequest{
		Operation: "c",
	}
	reqC <- TouchRequest{
		Operation: "m",
		Index:     3,
		PercentX:  0.5,
		PercentY:  0.5,
		Pressure:  1,
	}
	reqC <- TouchRequest{
		Operation: "u",
		Index:     4,
	}
	close(reqC)
	drainTouchRequests(conn, reqC)
	output := conn.buffer.String()
	assert.Equal(t, "d 1 1080 1920 255\nc\nm 3 540 960 255\nu 4\n", output)
}
