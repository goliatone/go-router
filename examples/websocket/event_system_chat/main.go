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

func newEventChatHost() *eventChatHost {
	h := &eventChatHost{
		app:     router.NewHTTPServer().(*router.HTTPServer),
		http:    &http.Server{ReadHeaderTimeout: 10 * time.Second},
		active:  make(map[*ownedSocket]struct{}),
		clients: make(map[string]router.WebSocketContext),
		stopped: make(chan struct{}),
	}
	app := h.app

	// Configure WebSocket settings
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"} // Allow all origins for demo
	// Increase timeouts for interactive chat
	config.ReadTimeout = 300 * time.Second // 5 minutes
	config.PingPeriod = 60 * time.Second   // Send ping every minute
	config.PongWait = 120 * time.Second    // Wait up to 2 minutes for pong

	// No OnConnect/OnDisconnect callbacks - handle everything in main handler

	// Chat handler with event-driven patterns
	chatHandler := func(ws router.WebSocketContext) error {
		log.Printf("[%s] WebSocket handler started", ws.ConnectionID()[:8])

		// Add client to connected clients
		h.clientsMu.Lock()
		h.clients[ws.ConnectionID()] = ws
		h.clientsMu.Unlock()
		log.Printf("Client connected: %s", ws.ConnectionID())
		defer func() {
			// Cleanup on exit
			h.clientsMu.Lock()
			delete(h.clients, ws.ConnectionID())
			h.clientsMu.Unlock()
			log.Printf("Client disconnected: %s", ws.ConnectionID())

		}()

		// Send welcome message
		if err := ws.WriteJSON(map[string]string{
			"type":    "welcome",
			"message": "Welcome to the event-driven chat!",
			"id":      ws.ConnectionID(),
		}); err != nil {
			log.Printf("[%s] Welcome message error: %v", ws.ConnectionID()[:8], err)
		}

		// Send initial message to confirm handler is working
		if err := ws.WriteJSON(map[string]any{
			"type":          "handler_ready",
			"message":       "Handler is ready to receive messages",
			"connection_id": ws.ConnectionID()[:8],
		}); err != nil {
			log.Printf("[%s] Failed to send handler_ready message: %v", ws.ConnectionID()[:8], err)
		}

		for {
			log.Printf("[%s] Waiting for message...", ws.ConnectionID()[:8])
			// Read message
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[%s] Read error: %v", ws.ConnectionID()[:8], err)
				break
			}
			log.Printf("[%s] SUCCESS: Received message: %+v", ws.ConnectionID()[:8], msg)

			eventType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			fmt.Printf("Received event: %s from %s\n", eventType, ws.ConnectionID())

			// Handle different event types
			switch eventType {
			case "message":
				// Broadcast chat message
				response := map[string]any{
					"type":      "message",
					"user":      ws.ConnectionID()[:8], // Short ID
					"message":   msg["message"],
					"timestamp": time.Now().Unix(),
				}

				// Snapshot recipients so network writes cannot block membership cleanup.
				recipients := h.clientSnapshot()
				broadcastCount := 0
				for _, client := range recipients {
					if client.ConnectionID() != ws.ConnectionID() {
						if err := client.WriteJSON(response); err != nil {
							log.Printf("[%s] Broadcast error to %s: %v", ws.ConnectionID()[:8], client.ConnectionID()[:8], err)
						} else {
							broadcastCount++
						}
					}
				}
				log.Printf("[%s] Broadcasted message '%s' to %d clients", ws.ConnectionID()[:8], msg["message"], broadcastCount)

				// Echo back to sender with confirmation
				confirmResponse := map[string]any{
					"type":      "message_sent",
					"user":      ws.ConnectionID()[:8],
					"message":   msg["message"],
					"timestamp": time.Now().Unix(),
				}
				if err := ws.WriteJSON(confirmResponse); err != nil {
					log.Printf("[%s] Confirmation error: %v", ws.ConnectionID()[:8], err)
				}

			case "ping":
				log.Printf("[%s] Handling ping event", ws.ConnectionID()[:8])
				if err := ws.WriteJSON(map[string]any{
					"type":      "pong",
					"timestamp": time.Now().Unix(),
				}); err != nil {
					log.Printf("[%s] Pong error: %v", ws.ConnectionID()[:8], err)
				}

			case "get_users":
				h.clientsMu.RLock()
				userList := make([]string, 0, len(h.clients))
				for id := range h.clients {
					userList = append(userList, id[:8]) // Short IDs
				}
				h.clientsMu.RUnlock()
				log.Printf("[%s] Sending user list: %d users", ws.ConnectionID()[:8], len(userList))

				if err := ws.WriteJSON(map[string]any{
					"type":  "users_list",
					"users": userList,
					"count": len(userList),
				}); err != nil {
					log.Printf("[%s] Users list error: %v", ws.ConnectionID()[:8], err)
				}

			case "close":
				ws.CloseWithStatus(router.CloseNormalClosure, "Goodbye!")
				return nil
			}
		}

		return nil
	}

	// Register routes using unified WebSocket interface
	app.Router().Get("/", homeHandler)
	app.Router().WebSocket("/ws/chat", config, h.track(chatHandler))

	// Health check endpoint
	app.Router().Get("/health", func(ctx router.Context) error {
		return ctx.JSON(200, map[string]string{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	return h
}

// Home page with event-driven WebSocket client
func homeHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Event-Driven Chat</title>
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
        <h1>🔥 Event-Driven Chat Server</h1>
        <p>This demo shows an event-driven WebSocket chat with real-time messaging and user management.</p>

        <div class="controls">
            <button class="success" onclick="connect()">Connect</button>
            <button class="danger" onclick="disconnect()">Disconnect</button>
            <button class="secondary" onclick="sendPing()">Ping</button>
            <button class="secondary" onclick="getUserList()">Get Users</button>
            <button class="secondary" onclick="clearMessages()">Clear</button>
            <span id="status" class="status-disconnected">Disconnected</span>
        </div>

        <div id="messages"></div>

        <div style="margin-top: 15px;">
            <input type="text" id="messageInput" placeholder="Type your message here..."
                   onkeypress="if(event.key === 'Enter') sendMessage()">
            <button class="primary" onclick="sendMessage()">Send Message</button>
        </div>

        <div style="margin-top: 20px;">
            <h3>Event Commands:</h3>
            <ul>
                <li><code>ping</code> - Send ping event</li>
                <li><code>get_users</code> - Get list of connected users</li>
                <li><code>close</code> - Close connection gracefully</li>
                <li>Type any message - Broadcast to all users</li>
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
            ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws/chat');

            ws.onopen = function() {
                updateStatus('Connected', true);
                addMessage('✅ Connected to event-driven chat', 'system');
            };

            ws.onmessage = function(event) {
                try {
                    const data = JSON.parse(event.data);
                    let displayMsg;

                    switch (data.type) {
                        case 'welcome':
                            displayMsg = '🎉 ' + data.message + ' (ID: ' + data.id + ')';
                            addMessage(displayMsg, 'system');
                            break;
                        case 'handler_ready':
                            displayMsg = '✅ ' + data.message + ' (Connection: ' + data.connection_id + ')';
                            addMessage(displayMsg, 'system');
                            break;
                        case 'message':
                            displayMsg = '💬 ' + data.user + ': ' + data.message;
                            addMessage(displayMsg, 'received');
                            break;
                        case 'message_sent':
                            displayMsg = '✓ Message sent: ' + data.message;
                            addMessage(displayMsg, 'sent');
                            break;
                        case 'pong':
                            displayMsg = '🏓 Pong received at ' + new Date(data.timestamp * 1000).toLocaleTimeString();
                            addMessage(displayMsg, 'system');
                            break;
                        case 'users_list':
                            displayMsg = '👥 Connected users (' + data.count + '): ' + data.users.join(', ');
                            addMessage(displayMsg, 'system');
                            break;
                        default:
                            displayMsg = '📥 ' + JSON.stringify(data, null, 2);
                            addMessage(displayMsg, 'received');
                    }
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
                ws.send(JSON.stringify({type: 'close'}));
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
                ws.send(JSON.stringify({type: 'message', message: msg}));
                input.value = '';
                input.focus();
            }
        }

        function sendPing() {
            if (!ws || ws.readyState !== WebSocket.OPEN) {
                addMessage('❌ Not connected to server', 'error');
                return;
            }

            ws.send(JSON.stringify({type: 'ping'}));
            addMessage('🏓 Sent ping event', 'sent');
        }

        function getUserList() {
            if (!ws || ws.readyState !== WebSocket.OPEN) {
                addMessage('❌ Not connected to server', 'error');
                return;
            }

            ws.send(JSON.stringify({type: 'get_users'}));
            addMessage('👥 Requested user list', 'sent');
        }

        function clearMessages() {
            messages.innerHTML = '';
            addMessage('Messages cleared', 'system');
        }

        // Auto-focus input
        input.focus();

        // Auto-connect on load
        window.onload = function() {
            addMessage('🚀 Event-driven chat ready. Click Connect to start!', 'system');
        };
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.Send([]byte(html))
}

// clientSnapshot copies membership under its lock. Handlers own the subsequent
// writes; disconnect and shutdown do not need to wait for that lock during I/O.
func (h *eventChatHost) clientSnapshot() []router.WebSocketContext {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	clients := make([]router.WebSocketContext, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	return clients
}

// eventChatHost owns HTTP and upgraded connection lifetimes. Allocate the native
// server before starting so shutdown prevents a late listener from escaping.
type eventChatHost struct {
	clientsMu sync.RWMutex
	clients   map[string]router.WebSocketContext
	app       *router.HTTPServer
	http      *http.Server
	mu        sync.Mutex
	stopping  bool
	active    map[*ownedSocket]struct{}
	workers   sync.WaitGroup
	stopOnce  sync.Once
	stopped   chan struct{}
	stopErr   error
}

type ownedSocket struct {
	router.WebSocketContext
	mu      sync.Mutex
	closing bool
}

func (s *ownedSocket) InterruptRead() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return nil
	}
	return s.WebSocketContext.(router.WebSocketReadInterrupter).InterruptRead()
}

func (s *ownedSocket) Close() error {
	return s.CloseWithStatus(router.CloseNormalClosure, "")
}

func (s *ownedSocket) CloseWithStatus(code int, reason string) error {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	// Native callbacks execute outside the lock; the enclosing handler stays owned.
	return s.WebSocketContext.CloseWithStatus(code, reason)
}

func (h *eventChatHost) track(next func(router.WebSocketContext) error) func(router.WebSocketContext) error {
	return func(ws router.WebSocketContext) error {
		socket := &ownedSocket{WebSocketContext: ws}
		h.mu.Lock()
		if h.stopping {
			h.mu.Unlock()
			return ws.Close()
		}
		h.active[socket] = struct{}{}
		h.workers.Add(1)
		h.mu.Unlock()
		defer func() {
			_ = socket.Close()
			h.mu.Lock()
			delete(h.active, socket)
			h.mu.Unlock()
			h.workers.Done()
		}()
		return next(socket)
	}
}

func (h *eventChatHost) Serve(address string) error {
	// WrappedRouter finalizes registration; call it only after all routes exist.
	native := h.app.WrappedRouter()
	h.http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Own entry before an upgrade can hijack its socket. Native HTTP shutdown
		// stops tracking hijacked connections before their handlers return.
		h.mu.Lock()
		if h.stopping {
			h.mu.Unlock()
			w.Header().Set("Connection", "close")
			http.Error(w, "server stopping", http.StatusServiceUnavailable)
			return
		}
		h.workers.Add(1)
		h.mu.Unlock()
		defer h.workers.Done()
		native.ServeHTTP(w, r)
	})
	h.http.Addr = address
	return h.http.ListenAndServe()
}

// Shutdown bounds this caller's wait. Cleanup continues after a deadline and
// subsequent calls join it; no application lock is held through socket callbacks.
func (h *eventChatHost) Shutdown(ctx context.Context) error {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.stopping = true
		sockets := make([]*ownedSocket, 0, len(h.active))
		for socket := range h.active {
			sockets = append(sockets, socket)
		}
		h.mu.Unlock()
		go func() {
			for _, socket := range sockets {
				h.stopErr = errors.Join(h.stopErr, socket.InterruptRead())
			}
			h.workers.Wait()
			close(h.stopped)
		}()
	})
	serverErr := h.http.Shutdown(ctx)
	select {
	case <-h.stopped:
		return errors.Join(serverErr, h.stopErr)
	case <-ctx.Done():
		return errors.Join(serverErr, ctx.Err())
	}
}

func (h *eventChatHost) Run(parent context.Context, address string, drain time.Duration) error {
	if err := parent.Err(); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), drain)
		defer cancel()
		return errors.Join(err, h.Shutdown(cleanup))
	}
	served := make(chan error, 1)
	go func() { served <- h.Serve(address) }()
	var serveErr error
	returned := false
	select {
	case serveErr = <-served:
		returned = true
	case <-parent.Done():
	}
	cleanup, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	shutdownErr := h.Shutdown(cleanup)
	if !returned {
		select {
		case serveErr = <-served:
		case <-cleanup.Done():
			serveErr = cleanup.Err()
		}
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(serveErr, shutdownErr)
}

func main() {
	address := os.Getenv("EVENT_CHAT_ADDR")
	if address == "" {
		address = ":8086"
	}
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Event-driven chat server starting on %s", address)
	if err := newEventChatHost().Run(parent, address, 5*time.Second); err != nil {
		log.Fatal("Server error: ", err)
	}
}
