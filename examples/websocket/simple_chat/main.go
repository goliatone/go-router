package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/goliatone/go-router"
)

// ChatMessage represents a chat message
type ChatMessage struct {
	Type      string    `json:"type"`
	User      string    `json:"user"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Room      string    `json:"room,omitempty"`
}

// ChatServer manages chat rooms and connections
type ChatServer struct {
	clients map[router.WebSocketContext]string          // client -> username
	rooms   map[string]map[router.WebSocketContext]bool // room -> clients
	mutex   sync.RWMutex
}

// NewChatServer creates a new chat server
func NewChatServer() *ChatServer {
	return &ChatServer{
		clients: make(map[router.WebSocketContext]string),
		rooms:   make(map[string]map[router.WebSocketContext]bool),
	}
}

// Join adds a client to a room
func (cs *ChatServer) Join(ws router.WebSocketContext, room, username string) {
	log.Printf("JOIN: Attempting to acquire lock for user=%s, room=%s", username, room)
	cs.mutex.Lock()
	log.Printf("JOIN: Lock acquired for user=%s, room=%s", username, room)

	// Add client
	cs.clients[ws] = username

	// Create room if it doesn't exist
	if cs.rooms[room] == nil {
		cs.rooms[room] = make(map[router.WebSocketContext]bool)
	}

	// Add client to room
	cs.rooms[room][ws] = true

	log.Printf("User '%s' joined room '%s'", username, room)

	// Prepare join message while holding lock
	joinMsg := ChatMessage{
		Type:      "user_joined",
		User:      "System",
		Message:   fmt.Sprintf("%s joined the chat", username),
		Timestamp: time.Now(),
		Room:      room,
	}

	// CRITICAL: Release lock before broadcasting to avoid deadlock
	// BroadcastToRoom needs to acquire RLock, but we're holding Lock
	cs.mutex.Unlock()

	// Now broadcast without holding the lock
	cs.BroadcastToRoom(room, joinMsg, nil)

	log.Printf("JOIN: Completed for user=%s, room=%s", username, room)
}

// Leave removes a client from all rooms
func (cs *ChatServer) Leave(ws router.WebSocketContext) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	username, exists := cs.clients[ws]
	if !exists {
		return
	}

	// Find and remove from all rooms
	var userRooms []string
	for room, clients := range cs.rooms {
		if clients[ws] {
			delete(clients, ws)
			userRooms = append(userRooms, room)

			// Clean up empty rooms
			if len(clients) == 0 {
				delete(cs.rooms, room)
			}
		}
	}

	// Remove from clients
	delete(cs.clients, ws)

	log.Printf("User '%s' left rooms: %v", username, userRooms)

	// Notify rooms about user leaving
	for _, room := range userRooms {
		leaveMsg := ChatMessage{
			Type:      "user_left",
			User:      "System",
			Message:   fmt.Sprintf("%s left the chat", username),
			Timestamp: time.Now(),
			Room:      room,
		}
		cs.BroadcastToRoom(room, leaveMsg, nil)
	}
}

// BroadcastToRoom sends a message to all clients in a room
func (cs *ChatServer) BroadcastToRoom(room string, msg ChatMessage, exclude router.WebSocketContext) {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	clients := cs.rooms[room]
	if clients == nil {
		return
	}

	for client := range clients {
		if client != exclude {
			if err := client.WriteJSON(msg); err != nil {
				log.Printf("Error sending to client: %v", err)
			}
		}
	}
}

// GetRoomUsers returns list of users in a room
func (cs *ChatServer) GetRoomUsers(room string) []string {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	var users []string
	clients := cs.rooms[room]
	if clients == nil {
		return users
	}

	for client := range clients {
		if username, exists := cs.clients[client]; exists {
			users = append(users, username)
		}
	}
	return users
}

func main() {
	// Create server adapter
	app := router.NewHTTPServer()

	// Create chat server
	chatServer := NewChatServer()

	// WebSocket configuration with sensible defaults
	// DefaultWebSocketConfig() already includes proper timeouts to prevent hanging connections:
	// - ReadTimeout: 60s, WriteTimeout: 10s, PingPeriod: 54s, PongWait: 60s
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"}

	// Chat WebSocket handler
	chatHandler := func(ws router.WebSocketContext) error {
		log.Printf("New WebSocket connection: %s", ws.ConnectionID())

		// Send welcome message
		welcome := ChatMessage{
			Type:      "welcome",
			User:      "System",
			Message:   "Welcome! Please join a room to start chatting.",
			Timestamp: time.Now(),
		}
		ws.WriteJSON(welcome)

		var currentRoom string
		var username string

		// Message handling loop
		for {
			log.Printf("Message loop iteration for user=%s, room=%s", username, currentRoom)
			messageType, data, err := ws.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				break
			}

			log.Printf("Received message type %d: %s", messageType, string(data))

			if messageType != router.TextMessage {
				log.Printf("Ignoring non-text message type: %d", messageType)
				continue
			}

			var msg ChatMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("JSON parse error: %v", err)
				continue
			}

			switch msg.Type {
			case "join":
				// Join a room
				username = msg.User
				currentRoom = msg.Room
				log.Printf("Processing JOIN: user=%s, room=%s", username, currentRoom)
				if currentRoom == "" {
					currentRoom = "general" // Default room
				}

				chatServer.Join(ws, currentRoom, username)

				// Send room info
				users := chatServer.GetRoomUsers(currentRoom)
				roomInfo := ChatMessage{
					Type:      "room_info",
					User:      "System",
					Message:   fmt.Sprintf("Users in %s: %v", currentRoom, users),
					Timestamp: time.Now(),
					Room:      currentRoom,
				}
				ws.WriteJSON(roomInfo)
				log.Printf("JOIN completed for user=%s, room=%s. Continuing message loop...", username, currentRoom)

			case "message":
				log.Printf("Processing MESSAGE: user=%s, room=%s, message=%s", username, currentRoom, msg.Message)
				// Send message to room
				if currentRoom == "" {
					log.Printf("User %s tried to send message without joining room", username)
					errorMsg := ChatMessage{
						Type:      "error",
						User:      "System",
						Message:   "Please join a room first",
						Timestamp: time.Now(),
					}
					ws.WriteJSON(errorMsg)
					continue
				}

				// Broadcast message to room
				msg.User = username
				msg.Timestamp = time.Now()
				msg.Room = currentRoom
				log.Printf("Broadcasting message from %s in room %s: %s", username, currentRoom, msg.Message)
				chatServer.BroadcastToRoom(currentRoom, msg, nil)

			case "list_rooms":
				// List available rooms
				chatServer.mutex.RLock()
				var rooms []string
				for room := range chatServer.rooms {
					rooms = append(rooms, room)
				}
				chatServer.mutex.RUnlock()

				roomList := ChatMessage{
					Type:      "room_list",
					User:      "System",
					Message:   fmt.Sprintf("Available rooms: %v", rooms),
					Timestamp: time.Now(),
				}
				ws.WriteJSON(roomList)

			case "list_users":
				// List users in current room
				if currentRoom == "" {
					continue
				}

				users := chatServer.GetRoomUsers(currentRoom)
				userList := ChatMessage{
					Type:      "user_list",
					User:      "System",
					Message:   fmt.Sprintf("Users in %s: %v", currentRoom, users),
					Timestamp: time.Now(),
					Room:      currentRoom,
				}
				ws.WriteJSON(userList)

			default:
				log.Printf("Unknown message type: %s", msg.Type)
			}
		}

		// Clean up when connection closes
		chatServer.Leave(ws)
		return nil
	}

	// Register routes
	app.Router().Get("/", chatHomeHandler)
	app.Router().WebSocket("/ws/chat", config, chatHandler)

	// API endpoints
	app.Router().Get("/api/stats", func(ctx router.Context) error {
		chatServer.mutex.RLock()
		stats := map[string]any{
			"connected_users": len(chatServer.clients),
			"active_rooms":    len(chatServer.rooms),
			"rooms":           make(map[string]int),
		}
		for room, clients := range chatServer.rooms {
			stats["rooms"].(map[string]int)[room] = len(clients)
		}
		chatServer.mutex.RUnlock()

		return ctx.JSON(200, stats)
	})

	// Start server
	port := ":8081"
	log.Printf("Chat Server starting on %s", port)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws/chat", port)
	log.Printf("Chat page: http://localhost%s/", port)
	log.Printf("Stats API: http://localhost%s/api/stats", port)

	if err := app.Serve(port); err != nil {
		log.Fatal("Server error:", err)
	}
}

// Home page with chat client
func chatHomeHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Simple Chat Room</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; background: #f5f5f5; }
        .container { max-width: 800px; margin: 0 auto; padding: 20px; }
        .chat-container {
            background: white;
            border-radius: 10px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            padding: 20px;
        }
        .header { text-align: center; margin-bottom: 20px; }
        .user-setup {
            display: flex;
            gap: 10px;
            margin-bottom: 20px;
            padding: 15px;
            background: #f8f9fa;
            border-radius: 8px;
        }
        .user-setup input, .user-setup select, .user-setup button {
            padding: 8px;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
        #messages {
            height: 400px;
            border: 1px solid #ddd;
            border-radius: 8px;
            overflow-y: auto;
            padding: 15px;
            background: #fafafa;
            margin-bottom: 15px;
        }
        .message {
            margin: 8px 0;
            padding: 8px 12px;
            border-radius: 6px;
            word-wrap: break-word;
        }
        .message-user { background: #e3f2fd; border-left: 3px solid #2196f3; }
        .message-system { background: #fff3e0; border-left: 3px solid #ff9800; font-style: italic; }
        .message-error { background: #ffebee; border-left: 3px solid #f44336; }
        .message-self { background: #e8f5e8; border-left: 3px solid #4caf50; }
        .message-info { background: #f3e5f5; border-left: 3px solid #9c27b0; }
        .message-meta { font-size: 0.8em; color: #666; margin-top: 4px; }
        .input-area {
            display: flex;
            gap: 10px;
        }
        .input-area input {
            flex: 1;
            padding: 10px;
            border: 1px solid #ddd;
            border-radius: 6px;
        }
        .input-area button {
            padding: 10px 20px;
            border: none;
            border-radius: 6px;
            background: #2196f3;
            color: white;
            cursor: pointer;
        }
        .input-area button:disabled {
            background: #ccc;
            cursor: not-allowed;
        }
        .status {
            padding: 10px;
            text-align: center;
            border-radius: 6px;
            margin-bottom: 15px;
            font-weight: bold;
        }
        .status-disconnected { background: #ffebee; color: #c62828; }
        .status-connected { background: #e8f5e8; color: #2e7d2e; }
        .commands {
            margin-top: 20px;
            padding: 15px;
            background: #f8f9fa;
            border-radius: 8px;
            border-left: 4px solid #17a2b8;
        }
        .commands h4 { margin-top: 0; color: #17a2b8; }
        .commands code {
            background: #e9ecef;
            padding: 2px 6px;
            border-radius: 3px;
            font-family: 'Courier New', monospace;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="chat-container">
            <div class="header">
                <h1>💬 Simple Chat Room</h1>
                <p>Real-time chat using WebSockets</p>
            </div>

            <div id="status" class="status status-disconnected">
                Disconnected - Enter your details to join
            </div>

            <div class="user-setup">
                <input type="text" id="usernameInput" placeholder="Your username" value="">
                <select id="roomSelect">
                    <option value="general">General</option>
                    <option value="random">Random</option>
                    <option value="tech">Tech Talk</option>
                    <option value="gaming">Gaming</option>
                </select>
                <button onclick="joinChat()">Join Chat</button>
                <button onclick="disconnect()">Leave</button>
            </div>

            <div id="messages"></div>

            <div class="input-area">
                <input type="text" id="messageInput" placeholder="Type your message..." disabled
                       onkeypress="if(event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); sendMessage(); }">
                <button id="sendButton" onclick="sendMessage()" disabled>Send</button>
            </div>

            <div class="commands">
                <h4>Available Commands:</h4>
                <ul>
                    <li><code>/list_rooms</code> - Show available rooms</li>
                    <li><code>/list_users</code> - Show users in current room</li>
                    <li><code>/help</code> - Show this help</li>
                </ul>
            </div>
        </div>
    </div>

    <script>
        let ws = null;
        let currentUser = '';
        let currentRoom = '';
        let connected = false;

        const messages = document.getElementById('messages');
        const status = document.getElementById('status');
        const messageInput = document.getElementById('messageInput');
        const sendButton = document.getElementById('sendButton');
        const usernameInput = document.getElementById('usernameInput');
        const roomSelect = document.getElementById('roomSelect');

        function updateStatus(text, isConnected) {
            status.textContent = text;
            status.className = 'status ' + (isConnected ? 'status-connected' : 'status-disconnected');
            
            messageInput.disabled = !isConnected;
            sendButton.disabled = !isConnected;
            connected = isConnected;
        }

        function addMessage(msg, type = 'info') {
            const div = document.createElement('div');
            div.className = 'message message-' + type;
            
            if (typeof msg === 'object') {
                let content = '';
                if (msg.user && msg.user !== 'System') {
                    content += '<strong>' + escapeHtml(msg.user) + ':</strong> ';
                } else if (msg.user === 'System') {
                    content += '<strong>📢 System:</strong> ';
                }
                content += escapeHtml(msg.message);
                
                const meta = '<div class="message-meta">' + 
                           new Date(msg.timestamp).toLocaleTimeString() +
                           (msg.room ? ' in #' + msg.room : '') +
                           '</div>';
                           
                div.innerHTML = content + meta;
            } else {
                div.innerHTML = escapeHtml(msg) + 
                              '<div class="message-meta">' + new Date().toLocaleTimeString() + '</div>';
            }
            
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        function escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }

        function joinChat() {
            if (connected) {
                addMessage('Already connected', 'system');
                return;
            }

            const username = usernameInput.value.trim();
            const room = roomSelect.value;

            if (!username) {
                alert('Please enter a username');
                return;
            }

            currentUser = username;
            currentRoom = room;

            updateStatus('Connecting...', false);
            
            ws = new WebSocket('ws://localhost:8081/ws/chat');

            ws.onopen = function() {
                console.log('WebSocket connected successfully');
                updateStatus('Connected to ' + currentRoom, true);
                
                // Join the selected room
                const joinMsg = {
                    type: 'join',
                    user: currentUser,
                    room: currentRoom
                };
                console.log('Sending join message:', joinMsg);
                ws.send(JSON.stringify(joinMsg));
                
                addMessage('Connected to chat room: #' + currentRoom, 'system');
                messageInput.focus();
            };

            ws.onmessage = function(event) {
                try {
                    const msg = JSON.parse(event.data);
                    let msgType = 'info';
                    
                    switch(msg.type) {
                        case 'welcome':
                        case 'room_info':
                        case 'room_list':
                        case 'user_list':
                            msgType = 'system';
                            break;
                        case 'error':
                            msgType = 'error';
                            break;
                        case 'user_joined':
                        case 'user_left':
                            msgType = 'info';
                            break;
                        case 'message':
                            msgType = msg.user === currentUser ? 'self' : 'user';
                            break;
                    }
                    
                    addMessage(msg, msgType);
                } catch (e) {
                    addMessage(event.data, 'system');
                }
            };

            ws.onclose = function(event) {
                updateStatus('Disconnected from chat', false);
                addMessage('Connection closed' + (event.reason ? ': ' + event.reason : ''), 'system');
                ws = null;
            };

            ws.onerror = function(error) {
                addMessage('Connection error occurred', 'error');
                console.error('WebSocket error:', error);
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
            }
        }

        function sendMessage() {
            console.log('sendMessage called, connected:', connected, 'ws:', !!ws);
            
            if (!connected || !ws) {
                console.log('Not connected - connected:', connected, 'ws:', !!ws);
                addMessage('Not connected to chat', 'error');
                return;
            }

            const text = messageInput.value.trim();
            console.log('Message text:', text);
            if (!text) return;

            // Handle commands
            if (text.startsWith('/')) {
                console.log('Handling command:', text);
                const command = text.substring(1);
                handleCommand(command);
                messageInput.value = '';
                return;
            }

            // Send regular message
            const msg = {
                type: 'message',
                message: text
            };

            console.log('Sending message:', msg);
            ws.send(JSON.stringify(msg));
            messageInput.value = '';
            messageInput.focus();
        }

        function handleCommand(command) {
            switch(command) {
                case 'list_rooms':
                    ws.send(JSON.stringify({type: 'list_rooms'}));
                    break;
                case 'list_users':
                    ws.send(JSON.stringify({type: 'list_users'}));
                    break;
                case 'help':
                    addMessage('Available commands: /list_rooms, /list_users, /help', 'system');
                    break;
                default:
                    addMessage('Unknown command: /' + command + '. Type /help for available commands.', 'error');
            }
        }

        // Generate default username
        usernameInput.value = 'User_' + Math.random().toString(36).substr(2, 6);
        
        // Auto-focus username input
        usernameInput.focus();
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.Send([]byte(html))
}
