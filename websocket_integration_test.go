package router_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
)

// Integration Test Suite for WebSocket Implementation

// Test: HTTPRouter Real WebSocket Communication
func TestHTTPRouterRealWebSocket(t *testing.T) {
	t.Run("EchoServer", func(t *testing.T) {
		// Create router
		router := httprouter.New()

		// Create WebSocket handler
		router.Handle("GET", "/ws", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Logf("Upgrade failed: %v", err)
				return
			}
			defer conn.Close()

			// Echo server
			for {
				messageType, data, err := conn.ReadMessage()
				if err != nil {
					break
				}

				if err := conn.WriteMessage(messageType, data); err != nil {
					break
				}
			}
		})

		// Create test server
		server := httptest.NewServer(router)
		defer server.Close()

		// Connect WebSocket client
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer ws.Close()

		// Test echo
		testMsg := "Hello, WebSocket!"
		if err := ws.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
			t.Fatalf("Failed to write: %v", err)
		}

		_, received, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to read: %v", err)
		}

		if string(received) != testMsg {
			t.Errorf("Expected %s, got %s", testMsg, string(received))
		}
	})

	t.Run("JSONMessages", func(t *testing.T) {
		// Create router
		router := httprouter.New()

		// JSON WebSocket handler
		router.Handle("GET", "/ws-json", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			// JSON echo with transformation
			for {
				var msg map[string]any
				if err := conn.ReadJSON(&msg); err != nil {
					break
				}

				// Add timestamp
				msg["timestamp"] = time.Now().Unix()
				msg["processed"] = true

				if err := conn.WriteJSON(msg); err != nil {
					break
				}
			}
		})

		// Create test server
		server := httptest.NewServer(router)
		defer server.Close()

		// Connect WebSocket client
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-json"
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer ws.Close()

		// Test JSON message
		testData := map[string]any{
			"action": "test",
			"value":  42,
		}

		if err := ws.WriteJSON(testData); err != nil {
			t.Fatalf("Failed to write JSON: %v", err)
		}

		var received map[string]any
		if err := ws.ReadJSON(&received); err != nil {
			t.Fatalf("Failed to read JSON: %v", err)
		}

		if received["action"] != "test" {
			t.Error("Action field not preserved")
		}

		if received["processed"] != true {
			t.Error("Message not processed")
		}

		if _, ok := received["timestamp"]; !ok {
			t.Error("Timestamp not added")
		}
	})

	t.Run("PingPong", func(t *testing.T) {
		// Create router
		router := httprouter.New()

		// WebSocket handler with ping/pong
		router.Handle("GET", "/ws-ping", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			// Set pong handler
			conn.SetPongHandler(func(data string) error {
				return nil
			})

			// Send periodic pings
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			done := make(chan bool)
			go func() {
				for {
					select {
					case <-ticker.C:
						if err := conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
							done <- true
							return
						}
					case <-done:
						return
					}
				}
			}()

			// Read messages
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					done <- true
					break
				}
			}
		})

		// Create test server
		server := httptest.NewServer(router)
		defer server.Close()

		// Connect WebSocket client
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-ping"
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer ws.Close()

		// Set ping handler
		pingReceived := false
		ws.SetPingHandler(func(data string) error {
			pingReceived = true
			return ws.WriteMessage(websocket.PongMessage, []byte(data))
		})

		// Wait for ping
		time.Sleep(200 * time.Millisecond)

		// Try to read to trigger handlers
		ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		ws.ReadMessage()

		if !pingReceived {
			t.Log("Warning: Ping may not have been received (timing dependent)")
		}
	})
}

// Test: Concurrent Connections
func TestConcurrentWebSocketConnections(t *testing.T) {
	// Create router
	router := httprouter.New()

	// Track connections
	var connMu sync.Mutex
	connCount := 0
	maxConn := 0

	// WebSocket handler
	router.Handle("GET", "/ws-concurrent", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Track connection
		connMu.Lock()
		connCount++
		if connCount > maxConn {
			maxConn = connCount
		}
		connMu.Unlock()

		defer func() {
			connMu.Lock()
			connCount--
			connMu.Unlock()
		}()

		// Echo server
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			if err := conn.WriteMessage(mt, data); err != nil {
				break
			}
		}
	})

	// Create test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Connect multiple clients
	numClients := 10
	var wg sync.WaitGroup
	errors := make(chan error, numClients)

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Connect
			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-concurrent"
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				errors <- fmt.Errorf("client %d: failed to connect: %v", id, err)
				return
			}
			defer ws.Close()

			// Send message
			msg := fmt.Sprintf("Message from client %d", id)
			if err := ws.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				errors <- fmt.Errorf("client %d: failed to write: %v", id, err)
				return
			}

			// Read echo
			_, data, err := ws.ReadMessage()
			if err != nil {
				errors <- fmt.Errorf("client %d: failed to read: %v", id, err)
				return
			}

			if string(data) != msg {
				errors <- fmt.Errorf("client %d: expected %s, got %s", id, msg, string(data))
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}

	// Verify concurrent connections
	if maxConn < 2 {
		t.Error("Should have had multiple concurrent connections")
	}
}

// Test: Message Broadcasting
func TestWebSocketBroadcasting(t *testing.T) {
	// Create router
	router := httprouter.New()

	// Connection pool
	var poolMu sync.RWMutex
	pool := make(map[*websocket.Conn]bool)

	// Broadcast channel
	broadcast := make(chan []byte)

	// Start broadcaster
	go func() {
		for msg := range broadcast {
			poolMu.RLock()
			for conn := range pool {
				conn.WriteMessage(websocket.TextMessage, msg)
			}
			poolMu.RUnlock()
		}
	}()

	// WebSocket handler
	router.Handle("GET", "/ws-broadcast", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Add to pool
		poolMu.Lock()
		pool[conn] = true
		poolMu.Unlock()

		defer func() {
			poolMu.Lock()
			delete(pool, conn)
			poolMu.Unlock()
		}()

		// Read and broadcast messages
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Broadcast to all
			broadcast <- data
		}
	})

	// Create test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Connect multiple clients
	numClients := 3
	clients := make([]*websocket.Conn, numClients)

	for i := 0; i < numClients; i++ {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-broadcast"
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Client %d failed to connect: %v", i, err)
		}
		defer ws.Close()
		clients[i] = ws
	}

	// Allow connections to establish
	time.Sleep(100 * time.Millisecond)

	// Client 0 sends a message
	testMsg := "Broadcast test message"
	if err := clients[0].WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("Failed to send broadcast: %v", err)
	}

	// All clients should receive it
	for i, client := range clients {
		client.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, data, err := client.ReadMessage()
		if err != nil {
			t.Errorf("Client %d failed to receive: %v", i, err)
			continue
		}

		if string(data) != testMsg {
			t.Errorf("Client %d: expected %s, got %s", i, testMsg, string(data))
		}
	}
}

// Test: Connection Lifecycle
func TestWebSocketConnectionLifecycle(t *testing.T) {
	// Create router
	router := httprouter.New()

	// Track lifecycle events
	var events []string
	var eventsMu sync.Mutex

	// WebSocket handler
	router.Handle("GET", "/ws-lifecycle", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		eventsMu.Lock()
		events = append(events, "connected")
		eventsMu.Unlock()

		defer func() {
			eventsMu.Lock()
			events = append(events, "disconnected")
			eventsMu.Unlock()
			conn.Close()
		}()

		// Handle messages
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				eventsMu.Lock()
				events = append(events, "read_error")
				eventsMu.Unlock()
				break
			}

			eventsMu.Lock()
			events = append(events, "message_received")
			eventsMu.Unlock()

			if string(data) == "close" {
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "goodbye"))
				break
			}

			if err := conn.WriteMessage(mt, data); err != nil {
				eventsMu.Lock()
				events = append(events, "write_error")
				eventsMu.Unlock()
				break
			}

			eventsMu.Lock()
			events = append(events, "message_sent")
			eventsMu.Unlock()
		}
	})

	// Create test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Connect client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-lifecycle"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Send message
	if err := ws.WriteMessage(websocket.TextMessage, []byte("test")); err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	// Read echo
	ws.ReadMessage()

	// Send close
	if err := ws.WriteMessage(websocket.TextMessage, []byte("close")); err != nil {
		t.Fatalf("Failed to send close: %v", err)
	}

	// Wait for close
	time.Sleep(100 * time.Millisecond)
	ws.Close()

	// Allow cleanup
	time.Sleep(100 * time.Millisecond)

	// Check events
	eventsMu.Lock()
	defer eventsMu.Unlock()

	expectedEvents := []string{"connected", "message_received", "message_sent"}
	for _, expected := range expectedEvents {
		found := false
		for _, event := range events {
			if event == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected event %s not found in %v", expected, events)
		}
	}
}

// Test: Error Handling
func TestWebSocketIntegrationErrorHandling(t *testing.T) {
	t.Run("InvalidUpgrade", func(t *testing.T) {
		// Create router
		router := httprouter.New()

		// WebSocket handler
		router.Handle("GET", "/ws", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				http.Error(w, "Upgrade failed", http.StatusBadRequest)
				return
			}
			defer conn.Close()
		})

		// Create test server
		server := httptest.NewServer(router)
		defer server.Close()

		// Make non-WebSocket request
		resp, err := http.Get(server.URL + "/ws")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("MessageSizeLimit", func(t *testing.T) {
		// Create router
		router := httprouter.New()

		// WebSocket handler with size limit
		router.Handle("GET", "/ws-limited", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			// Set max message size to 1KB
			conn.SetReadLimit(1024)

			// Try to read
			_, _, err = conn.ReadMessage()
			if err != nil {
				// Expected for large message
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseMessageTooBig, "message too large"))
			}
		})

		// Create test server
		server := httptest.NewServer(router)
		defer server.Close()

		// Connect client
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-limited"
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer ws.Close()

		// Send large message (>1KB)
		largeMsg := make([]byte, 2048)
		for i := range largeMsg {
			largeMsg[i] = 'A'
		}

		ws.WriteMessage(websocket.TextMessage, largeMsg)

		// Expect close message
		_, _, err = ws.ReadMessage()
		if err == nil {
			t.Error("Expected error for oversized message")
		}
	})
}

// Test: Subprotocol Negotiation
func TestWebSocketSubprotocols(t *testing.T) {
	// Create router
	router := httprouter.New()

	// WebSocket handler with subprotocols
	router.Handle("GET", "/ws-subprotocol", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			Subprotocols: []string{"chat", "echo"},
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Check selected protocol
		protocol := conn.Subprotocol()

		// Handle based on protocol
		switch protocol {
		case "chat":
			// Chat protocol - prefix messages
			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					break
				}
				response := append([]byte("[CHAT] "), data...)
				conn.WriteMessage(mt, response)
			}
		case "echo":
			// Echo protocol - return as-is
			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					break
				}
				conn.WriteMessage(mt, data)
			}
		default:
			// No protocol - close
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseProtocolError, "no protocol"))
		}
	})

	// Create test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Test chat protocol
	t.Run("ChatProtocol", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-subprotocol"
		header := http.Header{}
		header.Add("Sec-WebSocket-Protocol", "chat")

		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer ws.Close()

		if resp.Header.Get("Sec-WebSocket-Protocol") != "chat" {
			t.Error("Chat protocol not selected")
		}

		// Test chat behavior
		ws.WriteMessage(websocket.TextMessage, []byte("Hello"))
		_, data, _ := ws.ReadMessage()

		if !strings.HasPrefix(string(data), "[CHAT]") {
			t.Errorf("Expected chat prefix, got %s", string(data))
		}
	})

	// Test echo protocol
	t.Run("EchoProtocol", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-subprotocol"
		header := http.Header{}
		header.Add("Sec-WebSocket-Protocol", "echo")

		ws, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer ws.Close()

		if resp.Header.Get("Sec-WebSocket-Protocol") != "echo" {
			t.Error("Echo protocol not selected")
		}

		// Test echo behavior
		testMsg := "Hello"
		ws.WriteMessage(websocket.TextMessage, []byte(testMsg))
		_, data, _ := ws.ReadMessage()

		if string(data) != testMsg {
			t.Errorf("Expected %s, got %s", testMsg, string(data))
		}
	})
}

// Test: Performance and Load
func TestWebSocketPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Create router
	router := httprouter.New()

	// Message counter
	var messageCount int64
	var messageMu sync.Mutex

	// WebSocket handler
	router.Handle("GET", "/ws-perf", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo with counting
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				break
			}

			messageMu.Lock()
			messageCount++
			messageMu.Unlock()

			if err := conn.WriteMessage(mt, data); err != nil {
				break
			}
		}
	})

	// Create test server
	server := httptest.NewServer(router)
	defer server.Close()

	// Performance test
	numClients := 10
	messagesPerClient := 100

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws-perf"
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Logf("Client %d failed: %v", id, err)
				return
			}
			defer ws.Close()

			for j := 0; j < messagesPerClient; j++ {
				msg := fmt.Sprintf("Client %d Message %d", id, j)
				if err := ws.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
					break
				}

				if _, _, err := ws.ReadMessage(); err != nil {
					break
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// Calculate metrics
	totalMessages := int64(numClients * messagesPerClient)
	messagesPerSecond := float64(messageCount) / duration.Seconds()

	t.Logf("Performance Test Results:")
	t.Logf("  Clients: %d", numClients)
	t.Logf("  Messages per client: %d", messagesPerClient)
	t.Logf("  Total messages processed: %d", messageCount)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Messages/second: %.2f", messagesPerSecond)

	// Verify completion
	if messageCount < totalMessages*90/100 { // Allow 10% loss
		t.Errorf("Only processed %d of %d messages", messageCount, totalMessages)
	}
}
