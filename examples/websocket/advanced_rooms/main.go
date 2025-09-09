package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/goliatone/go-router"
)

// Global hub instance
var hub *router.WSHub

func main() {
	// Create HTTP server adapter
	app := router.NewHTTPServer()

	// Initialize WebSocket hub for advanced room management
	hub = router.NewWSHub()

	// Configure WebSocket settings for room management
	config := router.DefaultWebSocketConfig()
	config.Origins = []string{"*"} // Allow all origins for demo
	// Increase timeouts for room interactions
	config.ReadTimeout = 300 * time.Second // 5 minutes
	config.PingPeriod = 60 * time.Second   // Send ping every minute
	config.PongWait = 120 * time.Second    // Wait up to 2 minutes for pong

	// Set up the predefined rooms and configurations
	setupAdvancedRooms()

	// WebSocket handler for room management
	roomHandler := func(ws router.WebSocketContext) error {
		log.Printf("[%s] Room management client connected", ws.ConnectionID()[:8])

		// Create WSClient wrapper for the hub
		client := &WSClientAdapter{
			ws:    ws,
			id:    ws.ConnectionID(),
			data:  make(map[string]any),
			ctx:   context.Background(),
			rooms: make(map[string]bool),
		}

		// Send welcome message with available rooms
		ws.WriteJSON(map[string]any{
			"type":    "welcome",
			"message": "Connected to Advanced Room Management System",
			"rooms":   hub.ListRooms(),
			"stats":   hub.RoomStats(),
		})

		// Message processing loop
		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("[%s] Read error: %v", ws.ConnectionID()[:8], err)
				break
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			log.Printf("[%s] Received message: %s", ws.ConnectionID()[:8], msgType)

			switch msgType {
			case "room:join":
				handleRoomJoin(client, msg)
			case "room:leave":
				handleRoomLeave(client, msg)
			case "room:message":
				handleRoomMessage(client, msg)
			case "room:create":
				handleRoomCreate(client, msg)
			case "rooms:list":
				handleRoomsList(client)
			case "room:info":
				handleRoomInfo(client, msg)
			default:
				log.Printf("[%s] Unknown message type: %s", ws.ConnectionID()[:8], msgType)
			}
		}

		// Cleanup: leave all rooms
		client.LeaveAllRooms()
		log.Printf("[%s] Client disconnected and cleaned up", ws.ConnectionID()[:8])
		return nil
	}

	// Register routes
	app.Router().Get("/", homeHandler)
	app.Router().WebSocket("/ws/rooms", config, roomHandler)

	// Health check endpoint
	app.Router().Get("/api/rooms", func(ctx router.Context) error {
		rooms := hub.ListRooms()
		return ctx.JSON(200, map[string]any{
			"rooms": rooms,
			"stats": hub.RoomStats(),
		})
	})

	// Start server
	port := ":8087"
	log.Printf("Advanced Room Management Server starting on %s", port)
	log.Printf("WebSocket endpoint: ws://localhost%s/ws/rooms", port)
	log.Printf("Test page: http://localhost%s/", port)

	if err := app.Serve(port); err != nil {
		log.Fatal("Server error:", err)
	}
}

// WSClient adapter to work with the hub
type WSClientAdapter struct {
	ws    router.WebSocketContext
	id    string
	data  map[string]any
	ctx   context.Context
	rooms map[string]bool // Track joined rooms
}

func (c *WSClientAdapter) ID() string                { return c.id }
func (c *WSClientAdapter) Get(key string) any        { return c.data[key] }
func (c *WSClientAdapter) Set(key string, value any) { c.data[key] = value }
func (c *WSClientAdapter) GetString(key string) string {
	if v, ok := c.data[key].(string); ok {
		return v
	}
	return ""
}
func (c *WSClientAdapter) GetBool(key string) bool {
	if v, ok := c.data[key].(bool); ok {
		return v
	}
	return false
}
func (c *WSClientAdapter) GetInt(key string) int {
	if v, ok := c.data[key].(int); ok {
		return v
	}
	return 0
}
func (c *WSClientAdapter) IsConnected() bool {
	// Check if WebSocket connection is still open
	return c.ws != nil
}
func (c *WSClientAdapter) Join(room string) error {
	// WSClient interface compatibility - not used in this adapter
	return nil
}
func (c *WSClientAdapter) JoinWithContext(ctx context.Context, room string) error {
	return c.Join(room)
}
func (c *WSClientAdapter) Leave(room string) error {
	// WSClient interface compatibility - not used in this adapter
	return nil
}
func (c *WSClientAdapter) LeaveWithContext(ctx context.Context, room string) error {
	return c.Leave(room)
}
func (c *WSClientAdapter) OnJSON(event string, handler router.JSONHandler) error {
	// WSClient interface compatibility - not implemented in this adapter
	return nil
}
func (c *WSClientAdapter) OnMessage(handler router.MessageHandler) error {
	// WSClient interface compatibility - not implemented in this adapter
	return nil
}
func (c *WSClientAdapter) SendJSON(data any) error { return c.ws.WriteJSON(data) }
func (c *WSClientAdapter) Query(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}
func (c *WSClientAdapter) Room(roomName string) router.RoomBroadcaster {
	// Return a simplified room broadcaster
	return nil // Simplified implementation
}
func (c *WSClientAdapter) Rooms() []string {
	// Return list of joined rooms
	return []string{}
}
func (c *WSClientAdapter) SetContext(ctx context.Context) {
	c.ctx = ctx
}
func (c *WSClientAdapter) SetWithContext(ctx context.Context, key string, value any) {
	c.Set(key, value)
}
func (c *WSClientAdapter) Broadcast(data []byte) error {
	// Not needed for this adapter
	return nil
}
func (c *WSClientAdapter) BroadcastJSON(data any) error {
	// Not needed for this adapter
	return nil
}
func (c *WSClientAdapter) BroadcastJSONWithContext(ctx context.Context, data any) error {
	// Not needed for this adapter
	return nil
}
func (c *WSClientAdapter) BroadcastWithContext(ctx context.Context, data []byte) error {
	// Not needed for this adapter
	return nil
}
func (c *WSClientAdapter) Close(code int, message string) error {
	return nil
}
func (c *WSClientAdapter) CloseWithContext(ctx context.Context, code int, message string) error {
	return nil
}
func (c *WSClientAdapter) Conn() router.WebSocketContext {
	return c.ws
}
func (c *WSClientAdapter) ConnectionID() string {
	return c.id
}
func (c *WSClientAdapter) Context() context.Context {
	return c.ctx
}
func (c *WSClientAdapter) Emit(event string, data any) error {
	return c.ws.WriteJSON(map[string]any{"event": event, "data": data})
}
func (c *WSClientAdapter) Send(data []byte) error {
	return c.ws.WriteMessage(1, data) // TextMessage
}
func (c *WSClientAdapter) SendWithContext(ctx context.Context, data []byte) error {
	return c.Send(data)
}
func (c *WSClientAdapter) SendJSONWithContext(ctx context.Context, data any) error {
	return c.SendJSON(data)
}
func (c *WSClientAdapter) EmitWithContext(ctx context.Context, event string, data any) error {
	return c.Emit(event, data)
}
func (c *WSClientAdapter) LeaveAllRooms() {
	// Leave all joined rooms
	for roomID := range c.rooms {
		if room, err := hub.GetRoom(roomID); err == nil {
			room.RemoveClient(c.ctx, c)
			log.Printf("[%s] Left room %s during cleanup", c.id[:8], roomID)
		}
	}
	c.rooms = make(map[string]bool)
}

// Message handlers
func handleRoomJoin(client *WSClientAdapter, msg map[string]any) {
	roomID, ok := msg["roomId"].(string)
	if !ok {
		client.SendJSON(map[string]any{"type": "error", "message": "Missing roomId"})
		return
	}

	username, ok := msg["username"].(string)
	if !ok {
		username = "Anonymous"
	}
	client.Set("username", username)

	room, err := hub.GetRoom(roomID)
	if err != nil {
		client.SendJSON(map[string]any{"type": "error", "message": "Room not found"})
		return
	}

	// Try to join the room
	err = room.AddClient(client.ctx, client)
	if err != nil {
		client.SendJSON(map[string]any{
			"type":    "error",
			"message": fmt.Sprintf("Failed to join room: %v", err),
		})
		return
	}

	// Track room membership
	client.rooms[roomID] = true

	client.SendJSON(map[string]any{
		"type":     "room:joined",
		"roomId":   roomID,
		"roomInfo": room.GetInfo(),
		"message":  fmt.Sprintf("Successfully joined %s", room.Name()),
	})
}

func handleRoomLeave(client *WSClientAdapter, msg map[string]any) {
	roomID, ok := msg["roomId"].(string)
	if !ok {
		client.SendJSON(map[string]any{"type": "error", "message": "Missing roomId"})
		return
	}

	room, err := hub.GetRoom(roomID)
	if err != nil {
		client.SendJSON(map[string]any{"type": "error", "message": "Room not found"})
		return
	}

	err = room.RemoveClient(client.ctx, client)
	if err != nil {
		client.SendJSON(map[string]any{"type": "error", "message": "Failed to leave room"})
		return
	}

	// Remove from tracked rooms
	delete(client.rooms, roomID)

	client.SendJSON(map[string]any{
		"type":    "room:left",
		"roomId":  roomID,
		"message": fmt.Sprintf("Left room %s", room.Name()),
	})
}

func handleRoomMessage(client *WSClientAdapter, msg map[string]any) {
	roomID, ok := msg["roomId"].(string)
	if !ok {
		client.SendJSON(map[string]any{"type": "error", "message": "Missing roomId"})
		return
	}

	message, ok := msg["message"].(string)
	if !ok {
		client.SendJSON(map[string]any{"type": "error", "message": "Missing message"})
		return
	}

	room, err := hub.GetRoom(roomID)
	if err != nil {
		client.SendJSON(map[string]any{"type": "error", "message": "Room not found"})
		return
	}

	// Broadcast message to all clients in the room
	broadcastData := map[string]any{
		"type":      "room:message",
		"roomId":    roomID,
		"username":  client.GetString("username"),
		"message":   message,
		"timestamp": time.Now().Unix(),
	}

	log.Printf("[%s] Broadcasting message to room %s: %s", client.id[:8], roomID, message)
	err = room.Emit(client.ctx, "room:message", broadcastData)
	if err != nil {
		log.Printf("[%s] Failed to broadcast message: %v", client.id[:8], err)
	}
}

func handleRoomCreate(client *WSClientAdapter, msg map[string]any) {
	roomName, ok := msg["name"].(string)
	if !ok {
		client.SendJSON(map[string]any{"type": "error", "message": "Missing room name"})
		return
	}

	roomType, ok := msg["roomType"].(string)
	if !ok {
		roomType = "chat"
	}

	config := router.RoomConfig{
		MaxClients:       20,
		DestroyWhenEmpty: true,
		TrackPresence:    true,
		Private:          false,
		Type:             roomType,
		Tags:             []string{"user-created"},
	}

	roomID := fmt.Sprintf("user-%d", time.Now().UnixNano())
	room, err := hub.CreateRoom(client.ctx, roomID, roomName, config)
	if err != nil {
		client.SendJSON(map[string]any{"type": "error", "message": "Failed to create room"})
		return
	}

	client.SendJSON(map[string]any{
		"type":     "room:created",
		"roomId":   roomID,
		"roomInfo": room.GetInfo(),
		"message":  fmt.Sprintf("Created room: %s", roomName),
	})
}

func handleRoomsList(client *WSClientAdapter) {
	rooms := hub.ListRooms()
	stats := hub.RoomStats()

	client.SendJSON(map[string]any{
		"type":  "rooms:list",
		"rooms": rooms,
		"stats": stats,
	})
}

func handleRoomInfo(client *WSClientAdapter, msg map[string]any) {
	roomID, ok := msg["roomId"].(string)
	if !ok {
		client.SendJSON(map[string]any{"type": "error", "message": "Missing roomId"})
		return
	}

	room, err := hub.GetRoom(roomID)
	if err != nil {
		client.SendJSON(map[string]any{"type": "error", "message": "Room not found"})
		return
	}

	client.SendJSON(map[string]any{
		"type":     "room:info",
		"roomId":   roomID,
		"info":     room.GetInfo(),
		"presence": room.GetPresence(),
		"stats": map[string]any{
			"clientCount": room.ClientCount(),
			"maxClients":  "unknown", // MaxClients method not available
		},
	})
}

// Set up predefined advanced rooms for demonstration
func setupAdvancedRooms() {
	ctx := context.Background()

	// Game rooms
	gameConfigs := []struct {
		ID   string
		Name string
		Type string
		Max  int
	}{
		{"chess-1", "Chess Tournament #1", "game", 2},
		{"poker-1", "High Stakes Poker", "game", 6},
	}

	for _, config := range gameConfigs {
		room, err := hub.CreateRoom(ctx, config.ID, config.Name, router.RoomConfig{
			MaxClients:       config.Max,
			DestroyWhenEmpty: false, // Keep for demo
			TrackPresence:    true,
			Private:          false,
			Type:             config.Type,
			Tags:             []string{"game", "featured"},
		})
		if err != nil {
			log.Printf("Failed to create room %s: %v", config.ID, err)
			continue
		}

		room.SetMetadata("featured", true)
		room.SetMetadata("description", fmt.Sprintf("A %s room for competitive play", config.Type))
	}

	// Chat rooms
	chatConfigs := []struct {
		ID   string
		Name string
		Tags []string
	}{
		{"lobby", "Main Lobby", []string{"public", "general"}},
		{"tech-talk", "Tech Discussion", []string{"public", "technology"}},
		{"random", "Random Chat", []string{"public", "casual"}},
		{"vip-lounge", "VIP Lounge", []string{"premium", "exclusive"}},
	}

	for _, config := range chatConfigs {
		room, err := hub.CreateRoom(ctx, config.ID, config.Name, router.RoomConfig{
			MaxClients:       100,
			DestroyWhenEmpty: false, // Persistent
			TrackPresence:    true,
			Private:          config.ID == "vip-lounge",
			Type:             "chat",
			Tags:             config.Tags,
		})
		if err != nil {
			log.Printf("Failed to create room %s: %v", config.ID, err)
			continue
		}

		room.SetMetadata("featured", config.ID == "lobby")
		room.SetMetadata("description", fmt.Sprintf("A %s chat room", config.Name))
	}

	// Document collaboration room
	docRoom, err := hub.CreateRoom(ctx, "doc-collab", "Collaborative Document", router.RoomConfig{
		MaxClients:       10,
		DestroyWhenEmpty: false,
		TrackPresence:    true,
		Private:          false,
		Type:             "document",
		Tags:             []string{"collaboration", "productivity"},
	})
	if err != nil {
		log.Printf("Failed to create document room: %v", err)
	} else {
		docRoom.SetMetadata("featured", true)
		docRoom.SetMetadata("description", "Real-time collaborative document editing")
	}

	// Log statistics
	stats := hub.RoomStats()
	log.Printf("Room Statistics: %+v", stats)
	log.Println("Advanced room management examples configured")
}

// Home page with interactive room management interface
func homeHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Advanced Room Management</title>
    <meta charset="utf-8">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; max-width: 1200px; }
        .container { margin: 0 auto; display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
        .panel { background: #f5f5f5; padding: 20px; border-radius: 8px; }
        .room-list { max-height: 300px; overflow-y: auto; }
        .room-item {
            background: white; padding: 10px; margin: 5px 0;
            border-radius: 5px; border-left: 4px solid #007bff;
            cursor: pointer;
        }
        .room-item:hover { background: #e9ecef; }
        .room-item.joined { border-left-color: #28a745; background: #d4edda; }
        .room-type-game { border-left-color: #ff6b6b; }
        .room-type-chat { border-left-color: #4ecdc4; }
        .room-type-document { border-left-color: #ffe66d; }
        #messages {
            height: 300px; overflow-y: auto; padding: 10px;
            background: white; border: 1px solid #ddd; border-radius: 4px;
        }
        .message { margin: 8px 0; padding: 5px; }
        .message.system { background: #fff3cd; color: #856404; border-radius: 3px; }
        .message.error { background: #f8d7da; color: #721c24; border-radius: 3px; }
        .message.success { background: #d4edda; color: #155724; border-radius: 3px; }
        input, button { padding: 8px; margin: 5px 0; }
        input[type="text"] { width: 200px; }
        button { background: #007bff; color: white; border: none; border-radius: 4px; cursor: pointer; }
        button:hover { background: #0056b3; }
        button:disabled { background: #6c757d; cursor: not-allowed; }
        .controls { margin: 10px 0; }
        .status { padding: 5px 10px; border-radius: 3px; font-weight: bold; }
        .status.connected { background: #28a745; color: white; }
        .status.disconnected { background: #dc3545; color: white; }
        .room-info { font-size: 0.9em; color: #666; }
        .stats { background: #e9ecef; padding: 10px; border-radius: 4px; margin: 10px 0; }
    </style>
</head>
<body>
    <h1>🏠 Advanced Room Management System</h1>
    <p>Demonstrate WebSocket room features: join/leave rooms, real-time messaging, room creation, and presence tracking.</p>

    <div class="container">
        <div class="panel">
            <h2>🎮 Room Directory</h2>
            <div class="controls">
                <input type="text" id="usernameInput" placeholder="Your username" value="User123">
                <button onclick="connect()">Connect</button>
                <button onclick="disconnect()">Disconnect</button>
                <span id="status" class="status disconnected">Disconnected</span>
            </div>

            <div class="controls">
                <button onclick="refreshRooms()">🔄 Refresh Rooms</button>
                <button onclick="showCreateForm()">➕ Create Room</button>
            </div>

            <!-- Room creation form -->
            <div id="createForm" style="display:none; background:white; padding:10px; border-radius:4px; margin:10px 0;">
                <h4>Create New Room</h4>
                <input type="text" id="newRoomName" placeholder="Room name">
                <select id="newRoomType">
                    <option value="chat">Chat Room</option>
                    <option value="game">Game Room</option>
                    <option value="document">Document Room</option>
                </select>
                <br>
                <button onclick="createRoom()">Create</button>
                <button onclick="hideCreateForm()">Cancel</button>
            </div>

            <div id="roomStats" class="stats">
                <strong>Room Statistics:</strong><br>
                <span id="statsContent">Loading...</span>
            </div>

            <div class="room-list" id="roomList">
                <div style="text-align:center; color:#666;">Connect to see available rooms</div>
            </div>
        </div>

        <div class="panel">
            <h2>💬 Room Chat</h2>
            <div id="currentRoom">
                <strong>Current Room:</strong> <span id="currentRoomName">None</span>
                <button onclick="leaveCurrentRoom()" id="leaveBtn" style="display:none;">Leave Room</button>
            </div>

            <div id="messages"></div>

            <div class="controls">
                <input type="text" id="messageInput" placeholder="Type a message..."
                       onkeypress="if(event.key === 'Enter') sendMessage()" disabled>
                <button onclick="sendMessage()" id="sendBtn" disabled>Send</button>
            </div>
        </div>
    </div>

    <script>
        let ws = null;
        let joinedRooms = new Set();
        let currentRoom = null;

        function addMessage(msg, type = 'info') {
            const messages = document.getElementById('messages');
            const div = document.createElement('div');
            div.className = 'message ' + type;
            div.innerHTML = new Date().toLocaleTimeString() + ' - ' + msg;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        function updateStatus(text, connected) {
            const status = document.getElementById('status');
            status.textContent = text;
            status.className = 'status ' + (connected ? 'connected' : 'disconnected');
        }

        function connect() {
            if (ws && ws.readyState === WebSocket.OPEN) {
                addMessage('Already connected', 'system');
                return;
            }

            ws = new WebSocket('ws://localhost:8087/ws/rooms');

            ws.onopen = function() {
                updateStatus('Connected', true);
                addMessage('✅ Connected to Room Management System', 'success');
            };

            ws.onmessage = function(event) {
                try {
                    const data = JSON.parse(event.data);
                    handleMessage(data);
                } catch (e) {
                    addMessage('📥 ' + event.data, 'info');
                }
            };

            ws.onerror = function(error) {
                addMessage('❌ WebSocket error occurred', 'error');
                console.error('WebSocket error:', error);
            };

            ws.onclose = function(event) {
                updateStatus('Disconnected', false);
                addMessage('🔌 Disconnected: ' + (event.reason || 'Connection closed'), 'system');
                ws = null;
                joinedRooms.clear();
                updateRoomList([]);
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
            }
        }

        function handleMessage(data) {
            switch (data.type) {
                case 'welcome':
                    addMessage('🎉 ' + data.message, 'success');
                    if (data.rooms) {
                        updateRoomList(data.rooms);
                    }
                    if (data.stats) {
                        updateStats(data.stats);
                    }
                    break;

                case 'rooms:list':
                    updateRoomList(data.rooms);
                    updateStats(data.stats);
                    break;

                case 'room:joined':
                    joinedRooms.add(data.roomId);
                    currentRoom = data.roomId;
                    updateCurrentRoom(data.roomInfo);
                    addMessage('✅ Joined room: ' + data.roomInfo.name, 'success');
                    break;

                case 'room:left':
                    joinedRooms.delete(data.roomId);
                    if (currentRoom === data.roomId) {
                        currentRoom = null;
                        updateCurrentRoom(null);
                    }
                    addMessage('👋 ' + data.message, 'system');
                    break;

                case 'room:message':
                    // Handle nested message structure from room.Emit()
                    const msgData = data.data || data;
                    if (msgData.roomId === currentRoom) {
                        addMessage('💬 ' + msgData.username + ': ' + msgData.message, 'info');
                    }
                    break;

                case 'room:created':
                    addMessage('🏠 ' + data.message, 'success');
                    refreshRooms();
                    break;

                case 'error':
                    addMessage('❌ Error: ' + data.message, 'error');
                    break;

                default:
                    addMessage('📨 ' + JSON.stringify(data), 'info');
            }
        }

        function updateRoomList(rooms) {
            const roomList = document.getElementById('roomList');
            if (!rooms || rooms.length === 0) {
                roomList.innerHTML = '<div style="text-align:center; color:#666;">No rooms available</div>';
                return;
            }

            roomList.innerHTML = rooms.map(room => 
                '<div class="room-item room-type-' + room.type + ' ' + (joinedRooms.has(room.id) ? 'joined' : '') + '"' +
                     ' onclick="joinRoom(\'' + room.id + '\')">' +
                    '<strong>' + room.name + '</strong>' +
                    '<div class="room-info">' +
                        'Type: ' + room.type + ' | Clients: ' + room.clientCount + '/' + (room.maxClients || '∞') +
                        ' | Private: ' + (room.private ? 'Yes' : 'No') +
                        (room.tags ? ' | Tags: ' + room.tags.join(', ') : '') +
                    '</div>' +
                '</div>'
            ).join('');
        }

        function updateStats(stats) {
            if (!stats) return;
            const content = document.getElementById('statsContent');
            content.innerHTML = 
                'Total Rooms: ' + (stats.total_rooms || stats.TotalRooms || 0) + '<br>' +
                'Active Rooms: ' + (stats.active_rooms || stats.ActiveRooms || 0) + '<br>' +
                'Total Clients: ' + (stats.total_clients || stats.TotalClients || 0) + '<br>' +
                'Room Types: ' + Object.entries(stats.room_types || stats.RoomTypes || {}).map(([k,v]) => k + ': ' + v).join(', ');
        }

        function updateCurrentRoom(roomInfo) {
            const nameSpan = document.getElementById('currentRoomName');
            const leaveBtn = document.getElementById('leaveBtn');
            const messageInput = document.getElementById('messageInput');
            const sendBtn = document.getElementById('sendBtn');

            if (roomInfo) {
                nameSpan.textContent = roomInfo.name;
                leaveBtn.style.display = 'inline';
                messageInput.disabled = false;
                sendBtn.disabled = false;
                messageInput.focus();
            } else {
                nameSpan.textContent = 'None';
                leaveBtn.style.display = 'none';
                messageInput.disabled = true;
                sendBtn.disabled = true;
            }
        }

        function joinRoom(roomId) {
            if (!ws || ws.readyState !== WebSocket.OPEN) {
                addMessage('❌ Not connected to server', 'error');
                return;
            }

            const username = document.getElementById('usernameInput').value || 'Anonymous';

            ws.send(JSON.stringify({
                type: 'room:join',
                roomId: roomId,
                username: username
            }));
        }

        function leaveCurrentRoom() {
            if (!currentRoom) return;

            ws.send(JSON.stringify({
                type: 'room:leave',
                roomId: currentRoom
            }));
        }

        function sendMessage() {
            if (!currentRoom || !ws || ws.readyState !== WebSocket.OPEN) return;

            const input = document.getElementById('messageInput');
            const message = input.value.trim();
            if (!message) return;

            ws.send(JSON.stringify({
                type: 'room:message',
                roomId: currentRoom,
                message: message
            }));

            input.value = '';
        }

        function refreshRooms() {
            if (!ws || ws.readyState !== WebSocket.OPEN) return;

            ws.send(JSON.stringify({
                type: 'rooms:list'
            }));
        }

        function showCreateForm() {
            document.getElementById('createForm').style.display = 'block';
        }

        function hideCreateForm() {
            document.getElementById('createForm').style.display = 'none';
        }

        function createRoom() {
            const name = document.getElementById('newRoomName').value.trim();
            const type = document.getElementById('newRoomType').value;

            if (!name) {
                alert('Please enter a room name');
                return;
            }

            ws.send(JSON.stringify({
                type: 'room:create',
                name: name,
                roomType: type
            }));

            // Clear form
            document.getElementById('newRoomName').value = '';
            hideCreateForm();
        }

        // Auto-connect on load
        window.onload = function() {
            addMessage('🚀 Advanced Room Management ready. Click Connect to start!', 'system');
        };
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.Send([]byte(html))
}

// Helper types and functions
type ChatMessage struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type CursorPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Selection struct {
	Start CursorPosition `json:"start"`
	End   CursorPosition `json:"end"`
}
