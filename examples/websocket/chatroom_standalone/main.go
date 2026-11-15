//go:build ignore
// +build ignore

package main

import (
	"context"
	"encoding/json"
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

// ChatRoomMessage represents a chat message
type ChatRoomMessage struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Color     string    `json:"color,omitempty"`
}

// ChatRoom manages chat connections
type ChatRoom struct {
	mu         sync.RWMutex
	clients    map[string]*ChatClient
	broadcast  chan ChatRoomMessage
	register   chan *ChatClient
	unregister chan *ChatClient
	history    []ChatRoomMessage
	maxHistory int
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

// ChatClient represents a connected user
type ChatClient struct {
	ID       string
	Username string
	Color    string
	Conn     router.WebSocketContext
	Send     chan ChatRoomMessage
	Room     *ChatRoom
}

// NewChatRoom creates a new chat room
func NewChatRoom() *ChatRoom {
	room := &ChatRoom{
		clients:    make(map[string]*ChatClient),
		broadcast:  make(chan ChatRoomMessage),
		register:   make(chan *ChatClient),
		unregister: make(chan *ChatClient),
		history:    make([]ChatRoomMessage, 0),
		maxHistory: 100,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}

	go room.run()
	return room
}

// run handles chat room events
func (r *ChatRoom) run() {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			return
		case client := <-r.register:
			r.mu.Lock()
			r.clients[client.ID] = client
			for _, msg := range r.history {
				select {
				case client.Send <- msg:
				default:
				}
			}
			r.mu.Unlock()
			r.publish(ChatRoomMessage{Type: "join", Username: client.Username, Message: fmt.Sprintf("%s joined the chat", client.Username), Timestamp: time.Now(), Color: client.Color})
		case client := <-r.unregister:
			r.mu.Lock()
			_, exists := r.clients[client.ID]
			if exists {
				delete(r.clients, client.ID)
				close(client.Send)
			}
			r.mu.Unlock()
			if exists {
				r.publish(ChatRoomMessage{Type: "leave", Username: client.Username, Message: fmt.Sprintf("%s left the chat", client.Username), Timestamp: time.Now()})
			}
		case message := <-r.broadcast:
			r.publish(message)
		}
	}
}

// publish runs on the room worker, without spawning unowned broadcast goroutines.
func (r *ChatRoom) publish(message ChatRoomMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history = append(r.history, message)
	if len(r.history) > r.maxHistory {
		r.history = r.history[1:]
	}
	for id, client := range r.clients {
		select {
		case client.Send <- message:
		default:
			// Closing the queue makes its writer close the socket and release the reader.
			delete(r.clients, id)
			close(client.Send)
		}
	}
}

// Close is called after connection workers have left the room.
func (r *ChatRoom) Close() {
	r.closeOnce.Do(func() { close(r.stop) })
	<-r.done
}

// GetUserCount returns the number of connected users
func (r *ChatRoom) GetUserCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// GetUserList returns list of connected users
func (r *ChatRoom) GetUserList() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	users := make([]string, 0, len(r.clients))
	for _, client := range r.clients {
		users = append(users, client.Username)
	}
	return users
}

var (
	colors   = []string{"#FF6B6B", "#4ECDC4", "#45B7D1", "#96CEB4", "#FECA57", "#48C9B0", "#9B59B6", "#F39C12"}
	colorIdx = 0
	colorMu  sync.Mutex
)

func getNextColor() string {
	colorMu.Lock()
	defer colorMu.Unlock()
	color := colors[colorIdx%len(colors)]
	colorIdx++
	return color
}

// newLowLevelChatRoom configures the low-level implementation.
func newLowLevelChatRoom() *chatHost {
	// Register the HTTPRouter WebSocket factory
	router.RegisterHTTPRouterWebSocketFactory(nil)

	// Create router
	host := newChatHost()
	app, chatRoom := host.app, host.room

	// WebSocket configuration
	wsConfig := router.WebSocketConfig{
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		HandshakeTimeout: 10 * time.Second,
		Origins:          []string{"*"},
		PingPeriod:       30 * time.Second,
		PongWait:         60 * time.Second,
	}

	// Chat WebSocket handler
	chatHandler := func(ctx router.Context) error {
		wsCtx, ok := ctx.(router.WebSocketContext)
		if !ok {
			log.Printf("WebSocket upgrade failed: got context type %T", ctx)
			return ctx.Status(400).SendString("WebSocket upgrade required")
		}

		log.Printf("WebSocket connection established: %s", wsCtx.ConnectionID())

		// Get username from query
		username := ctx.Query("username")
		if username == "" {
			username = "Guest" + wsCtx.ConnectionID()[:8]
		}

		log.Printf("User connecting: %s (ID: %s)", username, wsCtx.ConnectionID())

		// Create client
		client := &ChatClient{
			ID:       wsCtx.ConnectionID(),
			Username: username,
			Color:    getNextColor(),
			Conn:     wsCtx,
			Send:     make(chan ChatRoomMessage, 256),
			Room:     chatRoom,
		}

		// Register client
		log.Printf("Registering client: %s", username)
		select {
		case chatRoom.register <- client:
		case <-chatRoom.done:
			return wsCtx.Close()
		}

		// Start client handlers
		written := make(chan struct{})
		go func() { defer close(written); client.writePump() }()
		client.readPump() // This blocks until connection closes
		<-written

		// readPump will handle unregistration in its defer
		log.Printf("Client %s disconnected", username)
		return nil
	}

	// Register routes
	app.Router().Get("/", chatPageHandler)
	app.Router().Get("/ws/chat", host.track(chatHandler), router.WebSocketUpgrade(wsConfig))
	app.Router().Get("/api/stats", chatRoom.statsHandler)

	return host
}

// newHighLevelChatRoom configures the implementation using WSHub and WSClient.
func newHighLevelChatRoom() *chatHost {
	// Register the HTTPRouter WebSocket factory
	router.RegisterHTTPRouterWebSocketFactory(nil)

	// Create router
	host := newChatHost()
	app, chatRoom := host.app, host.room

	// Create WSHub for managing WebSocket connections
	hub := router.NewWSHub()
	host.hub = hub

	// Define message handler that will be attached to each client
	messageHandler := func(ctx context.Context, client router.WSClient) error {
		client.OnMessage(func(msgCtx context.Context, data []byte) error {
			// Parse the incoming message
			var msg ChatRoomMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				return fmt.Errorf("invalid message format: %w", err)
			}

			// Get user info from client state
			username := client.Get("username")
			color := client.Get("color")

			// Create the chat message
			chatMsg := ChatRoomMessage{
				ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
				Type:      msg.Type,
				Username:  username.(string),
				Message:   msg.Message,
				Timestamp: time.Now(),
				Color:     color.(string),
			}

			// Handle different message types
			switch msg.Type {
			case "message":
				// Add to history and broadcast
				chatRoom.mu.Lock()
				chatRoom.history = append(chatRoom.history, chatMsg)
				if len(chatRoom.history) > chatRoom.maxHistory {
					chatRoom.history = chatRoom.history[1:]
				}
				chatRoom.mu.Unlock()

				hub.BroadcastJSON(chatMsg)
			case "typing":
				// Broadcast typing indicator
				chatMsg.Message = fmt.Sprintf("%s is typing...", username)
				hub.BroadcastJSON(chatMsg)
			}

			return nil
		})
		return nil
	}

	// Configure the hub with event handlers
	hub.OnConnect(func(ctx context.Context, client router.WSClient, request any) error {
		// Extract username from the query parameters
		// The client interface should have a Query method
		username := client.Query("username", "")
		if username == "" {
			username = "Guest" + client.ID()[:8] // Default username
		}

		// Store user info in client context
		client.SetWithContext(ctx, "username", username)
		client.SetWithContext(ctx, "color", getNextColor())

		log.Printf("User connected: %s (ID: %s)", username, client.ID())

		// Send chat history to new client
		chatRoom.mu.RLock()
		history := append([]ChatRoomMessage(nil), chatRoom.history...)
		chatRoom.mu.RUnlock()

		for _, msg := range history {
			client.SendJSON(msg)
		}

		// Broadcast join message
		joinMsg := ChatRoomMessage{
			Type:      "join",
			Username:  username,
			Message:   fmt.Sprintf("%s joined the chat", username),
			Timestamp: time.Now(),
			Color:     client.Get("color").(string),
		}

		// Add to history and broadcast
		chatRoom.mu.Lock()
		chatRoom.history = append(chatRoom.history, joinMsg)
		if len(chatRoom.history) > chatRoom.maxHistory {
			chatRoom.history = chatRoom.history[1:]
		}
		chatRoom.mu.Unlock()

		hub.BroadcastJSON(joinMsg)

		// Set up message handler for this client
		return messageHandler(ctx, client)
	})

	// Handle disconnections
	hub.OnDisconnect(func(ctx context.Context, client router.WSClient, request any) error {
		if !host.beginWork() {
			return nil
		}
		defer host.workers.Done()
		username := client.Get("username")
		if username == nil {
			username = "Unknown"
		}
		log.Printf("User disconnected: %s (ID: %s)", username, client.ID())

		// Broadcast leave message
		leaveMsg := ChatRoomMessage{
			Type:      "leave",
			Username:  username.(string),
			Message:   fmt.Sprintf("%s left the chat", username),
			Timestamp: time.Now(),
		}

		// Add to history and broadcast
		chatRoom.mu.Lock()
		chatRoom.history = append(chatRoom.history, leaveMsg)
		if len(chatRoom.history) > chatRoom.maxHistory {
			chatRoom.history = chatRoom.history[1:]
		}
		chatRoom.mu.Unlock()

		hub.BroadcastJSON(leaveMsg)
		return nil
	})

	// WebSocket configuration
	wsConfig := router.WebSocketConfig{
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		HandshakeTimeout: 10 * time.Second,
		Origins:          []string{"*"},
		PingPeriod:       30 * time.Second,
		PongWait:         60 * time.Second,
	}

	// Stats handler for high-level implementation
	statsHandlerHL := func(ctx router.Context) error {
		clients := hub.Clients()
		users := make([]string, 0, len(clients))

		for _, client := range clients {
			if username := client.Get("username"); username != nil {
				if usernameStr, ok := username.(string); ok {
					users = append(users, usernameStr)
				}
			}
		}

		return ctx.JSON(http.StatusOK, map[string]any{
			"users_online": len(clients),
			"user_list":    users,
			"server_time":  time.Now().Format(time.RFC3339),
		})
	}

	// Register routes
	app.Router().Get("/", chatPageHandler)
	app.Router().Get("/ws/chat", host.track(hub.Handler()), router.WebSocketUpgrade(wsConfig))
	app.Router().Get("/api/stats", statsHandlerHL)

	return host
}

// Client read pump
func (c *ChatClient) readPump() {
	defer func() {
		log.Printf("Unregistering client: %s", c.Username)
		select {
		case c.Room.unregister <- c:
		case <-c.Room.done:
		}
		c.Conn.Close()
	}()

	for {
		var msg ChatRoomMessage
		if err := c.Conn.ReadJSON(&msg); err != nil {
			break
		}

		// Process message
		msg.ID = fmt.Sprintf("%d", time.Now().UnixNano())
		msg.Username = c.Username
		msg.Timestamp = time.Now()
		msg.Color = c.Color

		switch msg.Type {
		case "message":
			select {
			case c.Room.broadcast <- msg:
			case <-c.Room.done:
				return
			}
		case "typing":
			// Handle typing indicator
			msg.Message = fmt.Sprintf("%s is typing...", c.Username)
			select {
			case c.Room.broadcast <- msg:
			case <-c.Room.done:
				return
			}
		}
	}
}

// Client write pump
func (c *ChatClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(router.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteJSON(message); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.Conn.WritePing([]byte{}); err != nil {
				return
			}
		}
	}
}

// Stats API endpoint
func (chatRoom *ChatRoom) statsHandler(ctx router.Context) error {
	return ctx.JSON(http.StatusOK, map[string]any{
		"users_online": chatRoom.GetUserCount(),
		"user_list":    chatRoom.GetUserList(),
		"server_time":  time.Now().Format(time.RFC3339),
	})
}

// Chat page HTML
func chatPageHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Chat Room</title>
    <style>
        body {
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            margin: 0;
            padding: 20px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            border-radius: 10px;
            box-shadow: 0 10px 40px rgba(0,0,0,0.2);
            overflow: hidden;
        }
        .header {
            background: #333;
            color: white;
            padding: 20px;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        .status {
            display: inline-block;
            padding: 5px 10px;
            background: #dc3545;
            border-radius: 20px;
            font-size: 14px;
        }
        .status.connected {
            background: #28a745;
        }
        #messages {
            height: 400px;
            overflow-y: auto;
            padding: 20px;
            background: #f8f9fa;
        }
        .message {
            margin: 10px 0;
            padding: 10px;
            background: white;
            border-radius: 5px;
            animation: slideIn 0.3s ease;
        }
        .message.system {
            background: #e9ecef;
            color: #6c757d;
            font-style: italic;
            text-align: center;
        }
        .message .username {
            font-weight: bold;
            margin-right: 10px;
        }
        .message .time {
            color: #6c757d;
            font-size: 12px;
            float: right;
        }
        .input-area {
            padding: 20px;
            background: white;
            border-top: 1px solid #dee2e6;
            display: flex;
            gap: 10px;
        }
        #messageInput {
            flex: 1;
            padding: 10px;
            border: 1px solid #ced4da;
            border-radius: 5px;
            font-size: 16px;
        }
        button {
            padding: 10px 20px;
            background: #007bff;
            color: white;
            border: none;
            border-radius: 5px;
            cursor: pointer;
            font-size: 16px;
        }
        button:hover {
            background: #0056b3;
        }
        .user-setup {
            padding: 20px;
            text-align: center;
        }
        #usernameInput {
            padding: 10px;
            border: 1px solid #ced4da;
            border-radius: 5px;
            font-size: 16px;
            margin-right: 10px;
        }
        @keyframes slideIn {
            from {
                transform: translateX(-20px);
                opacity: 0;
            }
            to {
                transform: translateX(0);
                opacity: 1;
            }
        }
        .users-online {
            color: #28a745;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>💬 Chat Room</h1>
            <div>
                <span class="users-online" id="userCount">0 users online</span>
                <span class="status" id="status">Disconnected</span>
            </div>
        </div>

        <div id="setupArea" class="user-setup">
            <h2>Join Chat</h2>
            <input type="text" id="usernameInput" placeholder="Enter your username" maxlength="20">
            <button onclick="joinChat()">Join</button>
        </div>

        <div id="chatArea" style="display: none;">
            <div id="messages"></div>
            <div class="input-area">
                <input type="text" id="messageInput" placeholder="Type a message..." disabled>
                <button onclick="sendMessage()" id="sendBtn" disabled>Send</button>
            </div>
        </div>
    </div>

    <script>
        let ws = null;
        let username = '';

        function joinChat() {
            username = document.getElementById('usernameInput').value.trim();
            if (!username) {
                alert('Please enter a username');
                return;
            }

            document.getElementById('setupArea').style.display = 'none';
            document.getElementById('chatArea').style.display = 'block';

            connect();
        }

        function connect() {
            ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws/chat?username=' + encodeURIComponent(username));

            ws.onopen = function() {
                document.getElementById('status').textContent = 'Connected';
                document.getElementById('status').className = 'status connected';
                document.getElementById('messageInput').disabled = false;
                document.getElementById('sendBtn').disabled = false;
                updateUserCount();
            };

            ws.onmessage = function(event) {
                const msg = JSON.parse(event.data);
                displayMessage(msg);
            };

            ws.onerror = function(error) {
                console.error('WebSocket error:', error);
            };

            ws.onclose = function() {
                document.getElementById('status').textContent = 'Disconnected';
                document.getElementById('status').className = 'status';
                document.getElementById('messageInput').disabled = true;
                document.getElementById('sendBtn').disabled = true;
            };
        }

        function displayMessage(msg) {
            const messages = document.getElementById('messages');
            const div = document.createElement('div');

            if (msg.type === 'join' || msg.type === 'leave') {
                div.className = 'message system';
                div.innerHTML = msg.message + ' <span class="time">' +
                    new Date(msg.timestamp).toLocaleTimeString() + '</span>';
            } else {
                div.className = 'message';
                div.innerHTML = '<span class="username" style="color: ' +
                    (msg.color || '#000') + '">' + msg.username + ':</span>' +
                    msg.message + '<span class="time">' +
                    new Date(msg.timestamp).toLocaleTimeString() + '</span>';
            }

            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
            updateUserCount();
        }

        function sendMessage() {
            const input = document.getElementById('messageInput');
            const msg = input.value.trim();

            if (msg && ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({
                    type: 'message',
                    message: msg
                }));
                input.value = '';
            }
        }

        function updateUserCount() {
            fetch('/api/stats')
                .then(res => res.json())
                .then(data => {
                    document.getElementById('userCount').textContent =
                        data.users_online + ' user' + (data.users_online !== 1 ? 's' : '') + ' online';
                });
        }

        document.getElementById('messageInput').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') {
                sendMessage();
            }
        });

        document.getElementById('usernameInput').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') {
                joinChat();
            }
        });
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.SendString(html)
}

// chatHost owns the HTTP listener and all application connection/room workers.
// The native HTTP server is allocated before startup, so shutdown also prevents
// a concurrently starting listener. Routing remains the selected HTTPRouter.
type chatHost struct {
	app      *router.HTTPServer
	http     *http.Server
	room     *ChatRoom
	hub      *router.WSHub
	mu       sync.Mutex
	stopping bool
	readers  map[router.WebSocketReadInterrupter]struct{}
	workers  sync.WaitGroup
	stopOnce sync.Once
	stopped  chan struct{}
	stopErr  error
}

func newChatHost() *chatHost {
	app := router.NewHTTPServer().(*router.HTTPServer)
	return &chatHost{
		app: app, room: NewChatRoom(),
		http:    &http.Server{ReadHeaderTimeout: 10 * time.Second},
		readers: make(map[router.WebSocketReadInterrupter]struct{}), stopped: make(chan struct{}),
	}
}

func (h *chatHost) beginWork() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopping {
		return false
	}
	h.workers.Add(1)
	return true
}

func (h *chatHost) track(next router.HandlerFunc) router.HandlerFunc {
	return func(c router.Context) error {
		ws, ok := c.(router.WebSocketContext)
		if !ok {
			return c.Status(http.StatusBadRequest).SendString("WebSocket upgrade required")
		}
		reader, ok := ws.(router.WebSocketReadInterrupter)
		if !ok {
			return errors.New("websocket read interruption unavailable")
		}
		h.mu.Lock()
		if h.stopping {
			h.mu.Unlock()
			return ws.Close()
		}
		h.workers.Add(1)
		h.readers[reader] = struct{}{}
		h.mu.Unlock()
		defer func() {
			_ = ws.Close()
			h.mu.Lock()
			delete(h.readers, reader)
			h.mu.Unlock()
			h.workers.Done()
		}()
		return next(c)
	}
}

func (h *chatHost) Serve(address string) error {
	h.app.Init()
	h.http.Handler = h.app.WrappedRouter()
	h.http.Addr = address
	return h.http.ListenAndServe()
}

// Shutdown is retryable: expiry bounds this caller's wait, while owned cleanup
// continues and a later call can join it. No lock is held through callbacks.
func (h *chatHost) Shutdown(ctx context.Context) error {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.stopping = true
		readers := make([]router.WebSocketReadInterrupter, 0, len(h.readers))
		for reader := range h.readers {
			readers = append(readers, reader)
		}
		h.mu.Unlock()
		go func() {
			for _, reader := range readers {
				_ = reader.InterruptRead()
			}
			if h.hub != nil {
				h.stopErr = h.hub.Close()
			}
			h.workers.Wait()
			h.room.Close()
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

func (h *chatHost) Run(ctx context.Context, address string, drain time.Duration) error {
	if err := ctx.Err(); err != nil {
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
	case <-ctx.Done():
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var host *chatHost
	if os.Getenv("USE_HIGH_LEVEL") == "true" {
		log.Println("Using HIGH-LEVEL WebSocket implementation")
		host = newHighLevelChatRoom()
	} else {
		log.Println("Using LOW-LEVEL WebSocket implementation")
		host = newLowLevelChatRoom()
	}
	address := os.Getenv("CHAT_ADDR")
	if address == "" {
		address = ":8089"
	}
	log.Printf("Chat Room Server starting on %s", address)
	if err := host.Run(ctx, address, 5*time.Second); err != nil {
		log.Fatal(err)
	}
}
