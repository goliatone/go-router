//go:build ignore
// +build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/goliatone/go-router"
)

// setupWebSocketRoutes demonstrates the unified WebSocket interface
// This SAME function works with ANY adapter - Fiber, HTTPRouter, or future ones!
func setupWebSocketRoutes[T any](app router.Server[T]) {
	// Shared client management for chat broadcasting
	chatClients := make(map[string]router.WebSocketContext)
	var chatMutex sync.RWMutex

	// WebSocket configuration - same for all adapters
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"} // Allow all origins for demo
	config.OnConnect = func(ws router.WebSocketContext) error {
		fmt.Printf("[WebSocket] Connected: %s\n", ws.ConnectionID())
		return ws.WriteJSON(map[string]any{
			"type":       "welcome",
			"message":    "Welcome to the unified WebSocket server!",
			"adapter":    getAdapterName(app),
			"connection": ws.ConnectionID(),
		})
	}
	config.OnDisconnect = func(ws router.WebSocketContext, err error) {
		// Remove from chat clients if connected to chat
		chatMutex.Lock()
		delete(chatClients, ws.ConnectionID())
		chatMutex.Unlock()

		fmt.Printf("[WebSocket] Disconnected: %s (error: %v)\n", ws.ConnectionID(), err)
	}

	// Echo WebSocket handler - same logic for all adapters
	echoHandler := func(ws router.WebSocketContext) error {
		fmt.Printf("[WebSocket] Handler started for %s\n", ws.ConnectionID())

		for {
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				fmt.Printf("[WebSocket] Read error: %v\n", err)
				break
			}

			fmt.Printf("[WebSocket] Received: %s\n", string(data))

			// Echo response
			response := map[string]any{
				"type":        "echo",
				"original":    string(data),
				"messageType": messageType,
				"timestamp":   time.Now().Unix(),
				"adapter":     getAdapterName(app),
			}

			if err := ws.WriteJSON(response); err != nil {
				fmt.Printf("[WebSocket] Write error: %v\n", err)
				break
			}
		}

		return nil
	}

	// Chat room WebSocket handler
	chatHandler := func(ws router.WebSocketContext) error {
		fmt.Printf("[Chat] User joined: %s\n", ws.ConnectionID())

		// Add client to chat room
		chatMutex.Lock()
		chatClients[ws.ConnectionID()] = ws
		chatMutex.Unlock()

		// Send join notification to this client
		ws.WriteJSON(map[string]any{
			"type":    "system",
			"message": "You joined the chat room",
			"adapter": getAdapterName(app),
		})

		// Notify other clients about new user
		chatMutex.RLock()
		userJoinMsg := map[string]any{
			"type":      "system",
			"message":   fmt.Sprintf("User %s joined the chat", ws.ConnectionID()[:8]),
			"adapter":   getAdapterName(app),
			"timestamp": time.Now().Format("15:04:05"),
		}
		for clientID, client := range chatClients {
			if clientID != ws.ConnectionID() { // Don't send to self
				client.WriteJSON(userJoinMsg)
			}
		}
		chatMutex.RUnlock()

		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				fmt.Printf("[Chat] Read error: %v\n", err)
				break
			}

			// Broadcast message to ALL connected chat clients
			response := map[string]any{
				"type":      "message",
				"user":      ws.ConnectionID()[:8], // Short ID
				"message":   msg["message"],
				"timestamp": time.Now().Format("15:04:05"),
				"adapter":   getAdapterName(app),
			}

			fmt.Printf("[Chat] Broadcasting message from %s to %d clients\n", ws.ConnectionID()[:8], len(chatClients))

			chatMutex.RLock()
			for clientID, client := range chatClients {
				if err := client.WriteJSON(response); err != nil {
					fmt.Printf("[Chat] Broadcast error to %s: %v\n", clientID[:8], err)
				}
			}
			chatMutex.RUnlock()
		}

		// Remove client on disconnect
		chatMutex.Lock()
		delete(chatClients, ws.ConnectionID())
		chatMutex.Unlock()

		// Notify other clients about user leaving
		chatMutex.RLock()
		userLeaveMsg := map[string]any{
			"type":      "system",
			"message":   fmt.Sprintf("User %s left the chat", ws.ConnectionID()[:8]),
			"adapter":   getAdapterName(app),
			"timestamp": time.Now().Format("15:04:05"),
		}
		for _, client := range chatClients {
			client.WriteJSON(userLeaveMsg)
		}
		chatMutex.RUnlock()

		return nil
	}

	// Register WebSocket routes - SAME API for all adapters!
	app.Router().WebSocket("/ws/echo", config, trackWebSocket(echoHandler)).SetName("websocket.echo")
	app.Router().WebSocket("/ws/chat", config, trackWebSocket(chatHandler)).SetName("websocket.chat")
}

// setupHTTPRoutes demonstrates regular HTTP routing alongside WebSocket
func setupHTTPRoutes[T any](app router.Server[T]) {
	// Handle service worker requests to avoid 404 errors
	app.Router().Get("/sw.js", func(c router.Context) error {
		return c.Status(404).Send([]byte(""))
	})

	app.Router().Get("/", func(c router.Context) error {
		html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>Unified WebSocket Demo - %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .container { max-width: 800px; margin: 0 auto; }
        .status { padding: 10px; margin: 10px 0; border-radius: 5px; }
        .connected { background-color: #d4edda; color: #155724; }
        .disconnected { background-color: #f8d7da; color: #721c24; }
        .messages { height: 300px; border: 1px solid #ccc; padding: 10px; overflow-y: scroll; margin: 10px 0; }
        input[type=text] { width: 70%%; padding: 10px; }
        button { padding: 10px 20px; margin: 0 5px; }
        .endpoint { margin: 20px 0; padding: 20px; border: 1px solid #ddd; border-radius: 5px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Unified WebSocket Demo</h1>
        <h2>Running on: %s adapter</h2>
        <p>This demonstrates that the SAME code works with any router adapter!</p>

        <!-- Echo WebSocket -->
        <div class="endpoint">
            <h3>Echo WebSocket (/ws/echo)</h3>
            <div id="echo-status" class="status disconnected">Disconnected</div>
            <div>
                <input type="text" id="echo-input" placeholder="Type a message to echo..." />
                <button onclick="sendEcho()">Send Echo</button>
                <button onclick="connectEcho()">Connect</button>
                <button onclick="disconnectEcho()">Disconnect</button>
            </div>
            <div id="echo-messages" class="messages"></div>
        </div>

        <!-- Chat WebSocket -->
        <div class="endpoint">
            <h3>Chat Room (/ws/chat)</h3>
            <div id="chat-status" class="status disconnected">Disconnected</div>
            <div>
                <input type="text" id="chat-input" placeholder="Type a chat message..." />
                <button onclick="sendChat()">Send Message</button>
                <button onclick="connectChat()">Connect</button>
                <button onclick="disconnectChat()">Disconnect</button>
            </div>
            <div id="chat-messages" class="messages"></div>
        </div>
    </div>

    <script>
        let echoWs = null;
        let chatWs = null;

        // Echo WebSocket functions
        function connectEcho() {
            if (echoWs) return;
            echoWs = new WebSocket('ws://localhost:3000/ws/echo');
            echoWs.onopen = () => updateStatus('echo', 'Connected');
            echoWs.onclose = () => { updateStatus('echo', 'Disconnected'); echoWs = null; };
            echoWs.onmessage = (e) => addMessage('echo', JSON.parse(e.data));
        }

        function disconnectEcho() {
            if (echoWs) { echoWs.close(); echoWs = null; }
        }

        function sendEcho() {
            const input = document.getElementById('echo-input');
            if (echoWs && input.value) {
                echoWs.send(input.value);
                input.value = '';
            }
        }

        // Chat WebSocket functions
        function connectChat() {
            if (chatWs) return;
            chatWs = new WebSocket('ws://localhost:3000/ws/chat');
            chatWs.onopen = () => updateStatus('chat', 'Connected');
            chatWs.onclose = () => { updateStatus('chat', 'Disconnected'); chatWs = null; };
            chatWs.onmessage = (e) => addMessage('chat', JSON.parse(e.data));
        }

        function disconnectChat() {
            if (chatWs) { chatWs.close(); chatWs = null; }
        }

        function sendChat() {
            const input = document.getElementById('chat-input');
            if (chatWs && input.value) {
                chatWs.send(JSON.stringify({message: input.value}));
                input.value = '';
            }
        }

        // Utility functions
        function updateStatus(type, status) {
            const elem = document.getElementById(type + '-status');
            elem.textContent = status;
            elem.className = 'status ' + (status === 'Connected' ? 'connected' : 'disconnected');
        }

        function addMessage(type, msg) {
            const messages = document.getElementById(type + '-messages');
            const div = document.createElement('div');
            div.innerHTML = '<strong>[' + (msg.type || 'message') + ']</strong> ' +
                           JSON.stringify(msg, null, 2);
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        // Enter key support
        document.getElementById('echo-input').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') sendEcho();
        });
        document.getElementById('chat-input').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') sendChat();
        });
    </script>
</body>
</html>`, getAdapterName(app), getAdapterName(app))

		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		return c.Status(200).Send([]byte(html))
	})
}

// Helper function to get adapter name for display
func getAdapterName[T any](app router.Server[T]) string {
	// Use any to avoid generic type constraints
	switch app := any(app).(type) {
	case *router.FiberV3Adapter:
		return "Fiber v3"
	case *router.HTTPServer:
		return "HTTPRouter"
	default:
		_ = app // avoid unused variable warning
		return "Unknown"
	}
}

func main() {
	// Choose adapter based on command line argument or environment
	adapterType := "fiber" // default
	if len(os.Args) > 1 {
		adapterType = os.Args[1]
	}
	if env := os.Getenv("ROUTER_ADAPTER"); env != "" {
		adapterType = env
	}

	fmt.Printf("Starting server with %s adapter...\n", adapterType)

	switch adapterType {
	case "fiber":
		app := router.NewFiberV3Adapter()

		// Setup routes - SAME function works with Fiber!
		setupWebSocketRoutes(app)
		setupHTTPRoutes(app)

		fmt.Println("Fiber server starting on :3000")
		fmt.Println("Try: go run examples/websocket_adapter_demo.go httprouter")
		if err := runDemo(app, ":3000"); err != nil {
			log.Fatal(err)
		}

	case "httprouter", "http":
		app := router.NewHTTPServer()

		// Setup routes - SAME function works with HTTPRouter!
		setupWebSocketRoutes(app)
		setupHTTPRoutes(app)

		fmt.Println("HTTPRouter server starting on :3000")
		fmt.Println("Try: go run examples/websocket_adapter_demo.go fiber")
		if err := runDemo(app, ":3000"); err != nil {
			log.Fatal(err)
		}

	default:
		fmt.Printf("Unknown adapter: %s\n", adapterType)
		fmt.Println("Usage: go run websocket_adapter_demo.go [fiber|httprouter]")
		os.Exit(1)
	}
}

var demoSockets = struct {
	sync.Mutex
	readers  map[router.WebSocketReadInterrupter]struct{}
	workers  sync.WaitGroup
	stopping bool
}{readers: make(map[router.WebSocketReadInterrupter]struct{})}

func trackWebSocket(handler func(router.WebSocketContext) error) func(router.WebSocketContext) error {
	return func(ws router.WebSocketContext) error {
		reader, ok := ws.(router.WebSocketReadInterrupter)
		if !ok {
			return fmt.Errorf("websocket read interruption unavailable")
		}
		demoSockets.Lock()
		if demoSockets.stopping {
			demoSockets.Unlock()
			return ws.Close()
		}
		demoSockets.readers[reader] = struct{}{}
		demoSockets.workers.Add(1)
		demoSockets.Unlock()
		defer func() {
			if err := ws.Close(); err != nil {
				log.Printf("websocket close: %v", err)
			}
			demoSockets.Lock()
			delete(demoSockets.readers, reader)
			demoSockets.Unlock()
			demoSockets.workers.Done()
		}()
		return handler(ws)
	}
}

func runDemo(server interface {
	Serve(string) error
	Shutdown(context.Context) error
}, address string) error {
	host, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	served := make(chan error, 1)
	go func() { served <- server.Serve(address) }()
	var serveErr error
	returned := false
	select {
	case serveErr = <-served:
		returned = true
	case <-host.Done():
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	demoSockets.Lock()
	demoSockets.stopping = true
	readers := make([]router.WebSocketReadInterrupter, 0, len(demoSockets.readers))
	for reader := range demoSockets.readers {
		readers = append(readers, reader)
	}
	demoSockets.Unlock()
	for _, reader := range readers {
		if err := reader.InterruptRead(); err != nil {
			log.Printf("interrupt websocket: %v", err)
		}
	}
	joined := make(chan struct{})
	go func() { demoSockets.workers.Wait(); close(joined) }()
	var workerErr error
	select {
	case <-joined:
	case <-ctx.Done():
		workerErr = ctx.Err()
	}
	shutdownErr := server.Shutdown(ctx)
	if !returned {
		select {
		case serveErr = <-served:
		case <-ctx.Done():
			serveErr = ctx.Err()
		}
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, workerErr, shutdownErr)
}
