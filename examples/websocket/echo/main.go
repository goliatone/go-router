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
	// Create HTTP server adapter (can also use NewFiberAdapter())
	app := router.NewHTTPServer()

	// Configure WebSocket settings
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"} // Allow all origins for demo
	config.OnConnect = func(ws router.WebSocketContext) error {
		log.Printf("Client connected: %s", ws.ConnectionID())
		return ws.WriteJSON(map[string]string{
			"type":    "welcome",
			"message": "Welcome to Echo Server!",
			"id":      ws.ConnectionID(),
		})
	}
	config.OnDisconnect = func(ws router.WebSocketContext, err error) {
		log.Printf("Client disconnected: %s (error: %v)", ws.ConnectionID(), err)
	}

	// Echo WebSocket handler
	echoHandler := func(ws router.WebSocketContext) error {
		fmt.Printf("Echo handler started for connection: %s\n", ws.ConnectionID())

		for {
			// Read message
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				fmt.Printf("Read error: %v\n", err)
				break
			}

			message := string(data)
			fmt.Printf("Received: %s\n", message)

			// Handle special commands
			switch message {
			case "ping":
				if err := ws.WritePing([]byte("pong")); err != nil {
					fmt.Printf("Ping error: %v\n", err)
				}
				continue
			case "close":
				ws.CloseWithStatus(router.CloseNormalClosure, "Goodbye!")
				return nil
			}

			// Echo back with prefix
			response := map[string]any{
				"type":        "echo",
				"original":    message,
				"messageType": messageType,
				"timestamp":   time.Now().Unix(),
				"server":      "echo-server",
			}

			if err := ws.WriteJSON(response); err != nil {
				fmt.Printf("Write error: %v\n", err)
				break
			}
		}

		return nil
	}

	// Register routes using unified WebSocket interface
	app.Router().Get("/", homeHandler)
	app.Router().WebSocket("/ws/echo", config, echoHandler)

	// Health check endpoint
	app.Router().Get("/health", func(ctx router.Context) error {
		return ctx.JSON(200, map[string]string{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Start server
	port := ":8080"
	log.Printf("Echo Server starting on %s", port)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws/echo", port)
	log.Printf("Test page: http://localhost%s/", port)

	if err := app.Serve(port); err != nil {
		log.Fatal("Server error:", err)
	}
}

// Home page with WebSocket client
func homeHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Echo Server</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; max-width: 800px; }
        .container { margin: 0 auto; }
        #messages {
            border: 1px solid #ddd;
            height: 400px;
            overflow-y: auto;
            padding: 15px;
            margin: 15px 0;
            background: #f9f9f9;
            border-radius: 5px;
        }
        .message {
            margin: 8px 0;
            padding: 5px 10px;
            border-radius: 3px;
            word-wrap: break-word;
        }
        .sent { background: #e3f2fd; border-left: 3px solid #2196f3; }
        .received { background: #e8f5e8; border-left: 3px solid #4caf50; }
        .system { background: #fff3e0; border-left: 3px solid #ff9800; font-style: italic; }
        .error { background: #ffebee; border-left: 3px solid #f44336; }
        input[type="text"] {
            width: 300px;
            padding: 10px;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
        button {
            padding: 10px 15px;
            margin: 0 5px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        .primary { background: #2196f3; color: white; }
        .secondary { background: #666; color: white; }
        .success { background: #4caf50; color: white; }
        .danger { background: #f44336; color: white; }
        .controls { margin: 15px 0; }
        #status {
            padding: 5px 10px;
            border-radius: 3px;
            font-weight: bold;
            margin-left: 10px;
        }
        .status-connected { background: #4caf50; color: white; }
        .status-disconnected { background: #f44336; color: white; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔄 WebSocket Echo Server</h1>
        <p>This demo shows real-time bidirectional communication using WebSockets.</p>

        <div class="controls">
            <button class="success" onclick="connect()">Connect</button>
            <button class="danger" onclick="disconnect()">Disconnect</button>
            <button class="secondary" onclick="sendPing()">Send Ping</button>
            <button class="secondary" onclick="clearMessages()">Clear Messages</button>
            <span id="status" class="status-disconnected">Disconnected</span>
        </div>

        <div id="messages"></div>

        <div style="margin-top: 15px;">
            <input type="text" id="messageInput" placeholder="Type your message here..."
                   onkeypress="if(event.key === 'Enter') sendMessage()">
            <button class="primary" onclick="sendMessage()">Send Message</button>
        </div>

        <div style="margin-top: 20px;">
            <h3>Try these commands:</h3>
            <ul>
                <li><code>ping</code> - Send a ping frame</li>
                <li><code>close</code> - Close the connection gracefully</li>
                <li>Any other text - Will be echoed back</li>
            </ul>
        </div>
    </div>

    <script>
        let ws = null;
        const messages = document.getElementById('messages');
        const status = document.getElementById('status');
        const input = document.getElementById('messageInput');

        function addMessage(msg, className, timestamp = true) {
            const div = document.createElement('div');
            div.className = 'message ' + className;
            const time = timestamp ? new Date().toLocaleTimeString() + ' - ' : '';
            div.innerHTML = time + msg;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        function updateStatus(text, connected) {
            status.textContent = text;
            status.className = connected ? 'status-connected' : 'status-disconnected';
        }

        function connect() {
            if (ws && ws.readyState === WebSocket.OPEN) {
                addMessage('Already connected', 'system');
                return;
            }

            addMessage('Connecting...', 'system');
            ws = new WebSocket('ws://localhost:8080/ws/echo');

            ws.onopen = function() {
                updateStatus('Connected', true);
                addMessage('✅ Connected to echo server', 'system');
            };

            ws.onmessage = function(event) {
                try {
                    const data = JSON.parse(event.data);
                    let displayMsg;

                    if (data.type === 'welcome') {
                        displayMsg = '🎉 ' + data.message + ' (ID: ' + data.id + ')';
                    } else if (data.type === 'echo') {
                        displayMsg = '📢 Echo: ' + data.original;
                    } else {
                        displayMsg = '📥 ' + JSON.stringify(data, null, 2);
                    }

                    addMessage(displayMsg, 'received');
                } catch (e) {
                    // Handle plain text messages
                    addMessage('📥 ' + event.data, 'received');
                }
            };

            ws.onerror = function(error) {
                addMessage('❌ WebSocket error occurred', 'error');
                console.error('WebSocket error:', error);
            };

            ws.onclose = function(event) {
                updateStatus('Disconnected', false);
                const reason = event.reason || 'Connection closed';
                addMessage('🔌 Disconnected: ' + reason, 'system');
                ws = null;
            };
        }

        function disconnect() {
            if (ws) {
                ws.send('close');
                // Connection will close gracefully via server
            } else {
                addMessage('Not connected', 'system');
            }
        }

        function sendMessage() {
            if (!ws || ws.readyState !== WebSocket.OPEN) {
                addMessage('❌ Not connected to server', 'error');
                return;
            }

            const msg = input.value.trim();
            if (msg) {
                ws.send(msg);
                addMessage('📤 Sent: ' + msg, 'sent');
                input.value = '';
                input.focus();
            }
        }

        function sendPing() {
            if (!ws || ws.readyState !== WebSocket.OPEN) {
                addMessage('❌ Not connected to server', 'error');
                return;
            }

            ws.send('ping');
            addMessage('🏓 Sent ping command', 'sent');
        }

        function clearMessages() {
            messages.innerHTML = '';
            addMessage('Messages cleared', 'system');
        }

        // Auto-focus input
        input.focus();

        // Auto-connect on load
        window.onload = function() {
            addMessage('🚀 Echo server ready. Click Connect to start!', 'system');
        };
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.Send([]byte(html))
}
