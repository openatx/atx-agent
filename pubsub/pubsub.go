package pubsub

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type PubSub struct {
	messageC chan Message
	subs     map[chan interface{}]Message
	mu       sync.Mutex
}

type Message struct {
	Topic    string
	Receiver string
	Data     interface{}
}

func New() *PubSub {
	return &PubSub{
		messageC: make(chan Message, 10),
	}
}

func (ps *PubSub) drain() {
	for message := range ps.messageC {
		ps.mu.Lock()
		for ch, m := range ps.subs {
			if m.Topic == message.Topic && m.Receiver == message.Receiver {
				select {
				case ch <- message.Data:
				case <-time.After(1 * time.Second):
					log.Println("Sub-chan receive timeout 1s, deleted")
					delete(ps.subs, ch)
				}
			}
		}
		ps.mu.Unlock()
	}
}

func (ps *PubSub) Publish(data interface{}, topic string, receiver string) {
	ps.messageC <- Message{
		Topic:    topic,
		Receiver: receiver,
		Data:     data,
	}
}

func (ps *PubSub) Subscribe(topic string, receiver string) chan interface{} {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	C := make(chan interface{})
	ps.subs[C] = Message{
		Topic:    topic,
		Receiver: receiver,
	}
	return C
}

func (ps *PubSub) Unsubscribe(ch chan interface{}) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.subs, ch)
}

type HTTPPubSub struct {
	ps *PubSub
	r  *mux.Router
}

func NewHTTPPubSub(ps *PubSub) *HTTPPubSub {
	r := mux.NewRouter()
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// publish
	r.HandleFunc("/{topic}/{receiver}", func(w http.ResponseWriter, r *http.Request) {
		topic := mux.Vars(r)["topic"]
		receiver := mux.Vars(r)["receiver"]
		var data interface{}
		json.NewDecoder(r.Body).Decode(&data)
		ps.Publish(data, topic, receiver)
	}).Methods("POST")

	// subscribe WebSocket
	r.HandleFunc("/{topic}/{receiver}", func(w http.ResponseWriter, r *http.Request) {
		topic := mux.Vars(r)["topic"]
		receiver := mux.Vars(r)["receiver"]
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
		dataC := ps.Subscribe(topic, receiver)
		defer ps.Unsubscribe(dataC)
		quitC := make(chan bool, 1)
		go func() {
			for {
				select {
				case <-quitC:
					return
				case data := <-dataC:
					ws.WriteJSON(data)
				}
			}
		}()
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				quitC <- true
				break
			}
		}
	}).Methods("GET")

	// subscribe hijack
	r.HandleFunc("/{topic}/{receiver}", func(w http.ResponseWriter, r *http.Request) {
		topic := mux.Vars(r)["topic"]
		receiver := mux.Vars(r)["receiver"]
		conn, err := hijackHTTPRequest(w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		dataC := ps.Subscribe(topic, receiver)
		defer ps.Unsubscribe(dataC)
		for data := range dataC {
			jsdata, _ := json.Marshal(data)
			if _, err := io.WriteString(conn, string(jsdata)+"\n"); err != nil {
				break
			}
		}
	}).Methods("CONNECT")

	return &HTTPPubSub{
		ps: ps,
		r:  r,
	}
}

func (h *HTTPPubSub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.r.ServeHTTP(w, r)
}

func hijackHTTPRequest(w http.ResponseWriter) (conn net.Conn, err error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		err = errors.New("webserver don't support hijacking")
		return
	}

	hjconn, bufrw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	conn = newHijackReadWriteCloser(hjconn.(*net.TCPConn), bufrw)
	return
}

type hijackRW struct {
	*net.TCPConn
	bufrw *bufio.ReadWriter
}

func (this *hijackRW) Write(data []byte) (int, error) {
	nn, err := this.bufrw.Write(data)
	this.bufrw.Flush()
	return nn, err
}

func (this *hijackRW) Read(p []byte) (int, error) {
	return this.bufrw.Read(p)
}

func newHijackReadWriteCloser(conn *net.TCPConn, bufrw *bufio.ReadWriter) net.Conn {
	return &hijackRW{
		bufrw:   bufrw,
		TCPConn: conn,
	}
}
