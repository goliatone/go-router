//go:build ignore
// +build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/goliatone/go-router"
)

func main() {
	// Create Fiber adapter with WebSocket support
	app := router.NewFiberV3Adapter()

	// WebSocket configuration
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"} // Allow all origins for demo
	config.OnConnect = func(ws router.WebSocketContext) error {
		fmt.Printf("WebSocket connected: %s\n", ws.ConnectionID())
		return nil
	}
	config.OnDisconnect = func(ws router.WebSocketContext, err error) {
		fmt.Printf("WebSocket disconnected: %s\n", ws.ConnectionID())
	}

	// WebSocket echo handler
	wsHandler := func(ws router.WebSocketContext) error {
		fmt.Printf("WebSocket handler started for connection: %s\n", ws.ConnectionID())

		// Send welcome message
		if err := ws.WriteJSON(map[string]string{
			"type":         "welcome",
			"message":      "Connected to WebSocket server!",
			"connectionId": ws.ConnectionID(),
		}); err != nil {
			return err
		}

		// Message handling loop
		for {
			// Read message
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				fmt.Printf("Read error: %v\n", err)
				break
			}

			fmt.Printf("Received message type %d: %s\n", messageType, string(data))

			// Echo back the message
			response := map[string]any{
				"type":            "echo",
				"originalMessage": string(data),
				"messageType":     messageType,
				"timestamp":       time.Now().Unix(),
			}

			if err := ws.WriteJSON(response); err != nil {
				fmt.Printf("Write error: %v\n", err)
				break
			}
		}

		return nil
	}

	// Add WebSocket route using the unified interface - works with ANY adapter!
	app.Router().WebSocket("/ws", config, trackWebSocket(wsHandler))

	// Add a simple HTTP route for testing
	app.Router().Get("/", func(c router.Context) error {
		html := `
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Demo</title>
</head>
<body>
    <h1>Fiber WebSocket Demo</h1>
    <div id="status">Disconnected</div>
    <div>
        <input type="text" id="messageInput" placeholder="Type a message..." />
        <button onclick="sendMessage()">Send</button>
    </div>
    <div id="messages"></div>

    <script>
        const ws = new WebSocket('ws://localhost:3000/ws');
        const status = document.getElementById('status');
        const messages = document.getElementById('messages');

        ws.onopen = function() {
            status.textContent = 'Connected';
            status.style.color = 'green';
        };

        ws.onclose = function() {
            status.textContent = 'Disconnected';
            status.style.color = 'red';
        };

        ws.onmessage = function(event) {
            const message = JSON.parse(event.data);
            const div = document.createElement('div');
            div.textContent = 'Received: ' + JSON.stringify(message);
            messages.appendChild(div);
        };

        function sendMessage() {
            const input = document.getElementById('messageInput');
            if (input.value) {
                ws.send(input.value);
                input.value = '';
            }
        }

        document.getElementById('messageInput').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') {
                sendMessage();
            }
        });
    </script>
</body>
</html>`
		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		return c.Status(200).Send([]byte(html))
	})

	// Handle service worker requests to avoid 404 errors
	app.Router().Get("/sw.js", func(c router.Context) error {
		return c.Status(404).Send([]byte(""))
	})

	fmt.Println("Starting server on :3000")
	fmt.Println("Open http://localhost:3000 to test WebSocket functionality")

	if err := runDemo(app, ":3000"); err != nil {
		log.Fatal(err)
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
	return errors.Join(serveErr, workerErr, shutdownErr)
}
