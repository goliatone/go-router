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
	fmt.Println("🚀 OnPreUpgrade Hook Demo")

	// Create a Fiber application
	app := router.NewFiberV3Adapter()

	// WebSocket configuration with OnPreUpgrade hook
	config := router.WebSocketConfig{
		Origins: []string{"*"},

		// OnPreUpgrade: Extract and validate data before WebSocket upgrade
		OnPreUpgrade: func(c router.Context) (router.UpgradeData, error) {
			fmt.Println("🔍 OnPreUpgrade: Processing with guaranteed HTTP context access")

			// Extract query parameters (guaranteed to work!)
			token := c.Query("token")
			username := c.Query("username")
			role := c.Query("role")

			if token == "" {
				return nil, fmt.Errorf("token parameter required")
			}
			if username == "" {
				return nil, fmt.Errorf("username parameter required")
			}

			// Simulate token validation
			if token != "valid-token" {
				return nil, fmt.Errorf("invalid token")
			}

			fmt.Printf("✅ Pre-validated: user=%s, role=%s\n", username, role)

			// Return structured data for WebSocket context
			return router.UpgradeData{
				"token":      token,
				"username":   username,
				"role":       role,
				"login_time": time.Now(),
			}, nil
		},

		// OnConnect: Clean access to pre-validated data
		OnConnect: func(ws router.WebSocketContext) error {
			fmt.Println("🔌 OnConnect: Accessing pre-upgrade data")

			// Get pre-validated data using the UpgradeData method
			username := router.GetUpgradeDataWithDefault(ws, "username", "unknown").(string)
			role := router.GetUpgradeDataWithDefault(ws, "role", "user").(string)

			if loginTime, exists := ws.UpgradeData("login_time"); exists {
				fmt.Printf("👋 User %s (%s) connected at %v\n", username, role, loginTime)
			}

			// Send welcome message
			return ws.WriteJSON(map[string]any{
				"type":     "welcome",
				"message":  fmt.Sprintf("Welcome %s! Your role is %s", username, role),
				"username": username,
				"role":     role,
			})
		},

		OnMessage: func(ws router.WebSocketContext, messageType int, data []byte) error {
			fmt.Printf("📨 Message received: %s\n", string(data))
			return nil
		},

		OnDisconnect: func(ws router.WebSocketContext, err error) {
			username := router.GetUpgradeDataWithDefault(ws, "username", "unknown").(string)
			fmt.Printf("👋 User %s disconnected\n", username)
		},
	}

	// Register WebSocket handler
	wsHandler := func(ws router.WebSocketContext) error {
		// WebSocket message loop
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				break
			}

			// Echo the message back
			response := map[string]any{
				"type":    "echo",
				"message": string(data),
				"time":    time.Now().Format("15:04:05"),
			}

			if err := ws.WriteJSON(response); err != nil {
				break
			}
		}
		return nil
	}

	app.Router().WebSocket("/ws", config, trackWebSocket(wsHandler))

	// Home page with demo interface
	app.Router().Get("/", func(c router.Context) error {
		html := `
<!DOCTYPE html>
<html>
<head>
    <title>OnPreUpgrade Hook Demo</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .container { max-width: 800px; margin: 0 auto; }
        .section { margin: 20px 0; padding: 20px; border: 1px solid #ddd; border-radius: 5px; }
        .messages { height: 300px; border: 1px solid #ccc; padding: 10px; overflow-y: scroll; }
        input, button { padding: 10px; margin: 5px; }
        .status { padding: 10px; margin: 10px 0; font-weight: bold; }
        .connected { background-color: #d4edda; color: #155724; }
        .disconnected { background-color: #f8d7da; color: #721c24; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔗 OnPreUpgrade Hook Demo</h1>
        <p>This demo shows the OnPreUpgrade hook extracting query parameters before WebSocket upgrade.</p>

        <div class="section">
            <h3>Connection</h3>
            <div id="status" class="status disconnected">Disconnected</div>
            <input type="text" id="username" placeholder="Username" value="john_doe" />
            <input type="text" id="role" placeholder="Role" value="admin" />
            <input type="text" id="token" placeholder="Token" value="valid-token" />
            <br />
            <button onclick="connect()">Connect</button>
            <button onclick="disconnect()">Disconnect</button>
        </div>

        <div class="section">
            <h3>Messages</h3>
            <div id="messages" class="messages"></div>
            <input type="text" id="messageInput" placeholder="Type a message..." />
            <button onclick="sendMessage()">Send</button>
        </div>
    </div>

    <script>
        let ws = null;

        function addMessage(text) {
            const messages = document.getElementById('messages');
            const div = document.createElement('div');
            div.innerHTML = '<strong>' + new Date().toLocaleTimeString() + '</strong> ' + text;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        function updateStatus(status, connected = false) {
            const statusEl = document.getElementById('status');
            statusEl.textContent = status;
            statusEl.className = 'status ' + (connected ? 'connected' : 'disconnected');
        }

        function connect() {
            if (ws) {
                addMessage('Already connected');
                return;
            }

            const username = document.getElementById('username').value;
            const role = document.getElementById('role').value;
            const token = document.getElementById('token').value;

            const wsUrl = 'ws://' + window.location.host + '/ws?username=' + username + '&role=' + role + '&token=' + token;
            addMessage('🔍 Connecting with OnPreUpgrade validation...');

            ws = new WebSocket(wsUrl);

            ws.onopen = function() {
                updateStatus('Connected', true);
                addMessage('✅ Connected! OnPreUpgrade hook validated successfully');
            };

            ws.onclose = function(event) {
                updateStatus('Disconnected', false);
                addMessage('❌ Disconnected (code: ' + event.code + ')');
                ws = null;
            };

            ws.onerror = function(error) {
                addMessage('❌ Connection failed - check OnPreUpgrade validation');
            };

            ws.onmessage = function(event) {
                try {
                    const data = JSON.parse(event.data);
                    if (data.type === 'welcome') {
                        addMessage('🎉 ' + data.message);
                    } else if (data.type === 'echo') {
                        addMessage('📨 Echo: ' + data.message + ' (at ' + data.time + ')');
                    } else {
                        addMessage('📦 ' + JSON.stringify(data));
                    }
                } catch (e) {
                    addMessage('📝 ' + event.data);
                }
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
            }
        }

        function sendMessage() {
            if (!ws) {
                addMessage('❌ Not connected');
                return;
            }
            const input = document.getElementById('messageInput');
            if (input.value.trim()) {
                ws.send(input.value);
                input.value = '';
            }
        }

        // Enter key support
        document.getElementById('messageInput').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') sendMessage();
        });

        addMessage('🚀 OnPreUpgrade Hook Demo loaded');
        addMessage('💡 Enter credentials and click Connect to test the hook');
    </script>
</body>
</html>`
		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		return c.Status(200).Send([]byte(html))
	})

	fmt.Println("\n🌐 Server starting on http://localhost:3001")
	fmt.Println("📝 Try these test URLs:")
	fmt.Println("   Valid:   ws://localhost:3001/ws?username=john&role=admin&token=valid-token")
	fmt.Println("   Invalid: ws://localhost:3001/ws?username=john&role=admin&token=bad-token")
	fmt.Println("   Missing: ws://localhost:3001/ws?username=john&role=admin")

	if err := runDemo(app, ":3001"); err != nil {
		log.Fatal(err)
	}
}

var demoSockets = newSocketTracker()

// socketTracker owns one server lifetime. The last worker signals completion
// directly, so a timed-out shutdown does not leave a WaitGroup waiter behind.
type socketTracker struct {
	sync.Mutex
	readers  map[router.WebSocketReadInterrupter]int
	workers  int
	stopping bool
	drained  chan struct{}
}

func newSocketTracker() *socketTracker {
	return &socketTracker{
		readers: make(map[router.WebSocketReadInterrupter]int),
		drained: make(chan struct{}),
	}
}

func trackWebSocket(handler func(router.WebSocketContext) error) func(router.WebSocketContext) error {
	return demoSockets.track(handler)
}

func (s *socketTracker) track(handler func(router.WebSocketContext) error) func(router.WebSocketContext) error {
	return func(ws router.WebSocketContext) error {
		reader, ok := ws.(router.WebSocketReadInterrupter)
		if !ok {
			return fmt.Errorf("websocket read interruption unavailable")
		}
		s.Lock()
		if s.stopping {
			s.Unlock()
			return ws.Close()
		}
		s.readers[reader]++
		s.workers++
		s.Unlock()
		defer func() {
			if err := ws.Close(); err != nil {
				log.Printf("websocket close: %v", err)
			}
			s.Lock()
			defer s.Unlock()
			s.readers[reader]--
			if s.readers[reader] == 0 {
				delete(s.readers, reader)
			}
			s.workers--
			if s.stopping && s.workers == 0 {
				close(s.drained)
			}
		}()
		return handler(ws)
	}
}

// stop seals admission and returns a snapshot without holding the lock during
// interruption or close callbacks. Repeated calls share the same completion.
func (s *socketTracker) stop() ([]router.WebSocketReadInterrupter, <-chan struct{}) {
	s.Lock()
	defer s.Unlock()
	if !s.stopping {
		s.stopping = true
		if s.workers == 0 {
			close(s.drained)
		}
	}
	readers := make([]router.WebSocketReadInterrupter, 0, len(s.readers))
	for reader := range s.readers {
		readers = append(readers, reader)
	}
	return readers, s.drained
}

type demoServer interface {
	Serve(string) error
	Shutdown(context.Context) error
}

func runDemo(server demoServer, address string) error {
	host, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return demoSockets.run(host, server, address, 5*time.Second)
}

func (s *socketTracker) run(host context.Context, server demoServer, address string, timeout time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- server.Serve(address) }()
	var serveErr error
	returned := false
	select {
	case serveErr = <-served:
		returned = true
	case <-host.Done():
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	readers, joined := s.stop()
	for _, reader := range readers {
		if err := reader.InterruptRead(); err != nil {
			log.Printf("interrupt websocket: %v", err)
		}
	}
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
