package jsonrpc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/levigross/grequests"
	"github.com/pkg/errors"
)

type ErrorCode int

const (
	E_PARSE       ErrorCode = -32700
	E_INVALID_REQ ErrorCode = -32600
	E_NO_METHOD   ErrorCode = -32601
	E_BAD_PARAMS  ErrorCode = -32602
	E_INTERNAL    ErrorCode = -32603
	E_SERVER      ErrorCode = -32000
)

const JSONRPC_VERSION = "2.0"

type Request struct {
	Version string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Response struct {
	Version string           `json:"jsonrpc"`
	ID      int64            `json:"id"`
	Result  *json.RawMessage `json:"result,omitempty"`
	Error   *json.RawMessage `json:"error,omitempty"`
}

type RPCError struct {
	Code    ErrorCode   `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (re *RPCError) Error() string {
	return fmt.Sprintf("code:%d message:%s data:%v", re.Code, re.Message, re.Data)
}

func NewRequest(method string, params ...interface{}) *Request {
	return &Request{
		Version: JSONRPC_VERSION,
		ID:      time.Now().Unix(),
		Method:  method,
		Params:  params,
	}
}

type Client struct {
	URL             string
	Timeout         time.Duration
	ErrorCallback   func() error
	ErrorFixTimeout time.Duration
	ServerOK        func() bool
}

func NewClient(url string) *Client {
	return &Client{
		URL:     url,
		Timeout: 10 * time.Second,
	}
}

func (r *Client) Call(method string, params ...interface{}) (resp *Response, err error) {
	gres, err := grequests.Post(r.URL, &grequests.RequestOptions{
		RequestTimeout: r.Timeout,
		JSON:           NewRequest(method, params...),
	})
	if err != nil {
		return
	}
	if gres.Error != nil {
		err = gres.Error
		return
	}
	resp = new(Response)
	if err = gres.JSON(resp); err != nil {
		return
	}
	if resp.Error != nil {
		rpcErr := &RPCError{}
		if er := json.Unmarshal(*resp.Error, rpcErr); er != nil {
			err = &RPCError{
				Code:    E_SERVER,
				Message: string(*resp.Error),
			}
			return
		}
		err = rpcErr
	}
	return
}

func (r *Client) RobustCall(method string, params ...interface{}) (resp *Response, err error) {
	resp, err = r.Call(method, params...)
	if err == nil {
		return
	}
	if r.ErrorCallback == nil || r.ErrorCallback() != nil {
		return
	}

	start := time.Now()
	for {
		if time.Now().Sub(start) > r.ErrorFixTimeout {
			return
		}
		if r.ServerOK != nil && !r.ServerOK() {
			err = errors.New("jsonrpc server is down, auto-recover failed")
			return
		}
		time.Sleep(1 * time.Second)
		resp, err = r.Call(method, params...)
		if err == nil {
			return
		}
	}
}
