//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/goliatone/go-router"
)

func main() {
	// Create Fiber adapter with WebSocket support
	app := router.NewFiberAdapter()

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
	app.Router().WebSocket("/ws", config, wsHandler)

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

	log.Fatal(app.Serve(":3000"))
}
