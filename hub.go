package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients    map[*Client]bool // Registered clients.
	broadcast  chan []byte      // Inbound messages from the clients.
	register   chan *Client     // Register requests from the clients.
	unregister chan *Client     // Unregister requests from clients.
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte, 10),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) _startTranslate(ctx context.Context) {
	h.broadcast <- []byte("welcome")
	if minicapSocketPath == "@minicap" {
		service.Start("minicap")
	}

	log.Printf("Receive images from %s", minicapSocketPath)
	retries := 0
	for {
		if retries > 10 {
			log.Printf("unix %s connect failed", minicapSocketPath)
			h.broadcast <- []byte("@minicapagent listen timeout")
			break
		}

		conn, err := net.Dial("unix", minicapSocketPath)
		if err != nil {
			retries++
			log.Printf("dial %s err: %v, wait 0.5s", minicapSocketPath, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		retries = 0 // connected, reset retries
		if er := translateMinicap(conn, h.broadcast, ctx); er == nil {
			conn.Close()
			log.Println("transfer closed")
			break
		} else {
			conn.Close()
			log.Println("translateMinicap error:", er) //scrcpy read error, try to read again")
		}
	}
}

func (h *Hub) run() {
	var cancel context.CancelFunc
	var ctx context.Context

	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Println("new broadcast client")
			h.broadcast <- []byte("rotation " + strconv.Itoa(deviceRotation))
			if len(h.clients) == 1 {
				ctx, cancel = context.WithCancel(context.Background())
				go h._startTranslate(ctx)
			}
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			if len(h.clients) == 0 {
				log.Println("All client quited, context stop minicap service")
				cancel()
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub  *Hub
	conn *websocket.Conn // The websocket connection.
	send chan []byte     // Buffered channel of outbound messages.
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(time.Second * 10)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		var err error
		select {
		case data, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(time.Second * 10))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if string(data[:2]) == "\xff\xd8" || string(data[:4]) == "\x89PNG" { // jpg or png data
				err = c.conn.WriteMessage(websocket.BinaryMessage, data)
			} else {
				err = c.conn.WriteMessage(websocket.TextMessage, data)
			}
		case <-ticker.C:
			// err = c.conn.WriteMessage(websocket.PingMessage, nil)
		}
		if err != nil {
			log.Println(err)
			break
		}
	}
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	// c.conn.SetReadLimit(maxMessageSize)
	// c.conn.SetReadDeadline(time.Now().Add(pongWait))
	// c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		log.Println("websocket recv message", string(message))
		// message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		// c.hub.broadcast <- message
	}
}

func broadcastWebsocket() func(http.ResponseWriter, *http.Request) {
	hub := newHub()
	go hub.run() // start read images from unix:@minicap

	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println(err)
			return
		}
		client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
		hub.register <- client

		done := make(chan bool)
		go client.writePump()
		go func() {
			client.readPump()
			done <- true
		}()
		go func() {
			ch := make(chan interface{})
			rotationPublisher.Register(ch)
			defer rotationPublisher.Unregister(ch)
			for {
				select {
				case <-done:
					return
				case r := <-ch:
					hub.broadcast <- []byte(fmt.Sprintf("rotation %v", r))
				}
			}
		}()
	}
}
