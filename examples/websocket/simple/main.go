package main

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	// Test WebSocket connection
	u := url.URL{Scheme: "ws", Host: "localhost:8089", Path: "/ws/chat", RawQuery: "username=testuser"}
	log.Printf("Connecting to %s", u.String())

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			log.Printf("HTTP Response Status: %d", resp.StatusCode)
		}
		log.Fatal("dial:", err)
	}
	defer conn.Close()

	log.Println("Connected successfully!")

	// Send a test message
	msg := map[string]string{
		"type":    "message",
		"message": "Hello from test client!",
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Fatal("write:", err)
	}

	// Read response
	var response any
	if err := conn.ReadJSON(&response); err != nil {
		log.Fatal("read:", err)
	}

	fmt.Printf("Received: %+v\n", response)
}
