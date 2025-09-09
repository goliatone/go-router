# WebSocket Examples

This directory contains comprehensive WebSocket examples demonstrating the unified WebSocket interface in go-router. All examples are complete applications that can be run directly and showcase different WebSocket patterns and features.

## Available Examples

### 1. 🚀 Advanced Features Dashboard (`advanced/`)
**The flagship example** - A comprehensive WebSocket dashboard showcasing all major WebSocket features and patterns in one application.

```bash
go run examples/websocket/advanced/main.go
# Visit http://localhost:8080
```

**Features:**
- **File Transfer System**: Upload/download files with progress tracking
- **Realttime Collaboration**: Google Docs style collaborative text editing
- **Room Management**: Advanced room based messaging with client tracking
- **Reconnection & Queueing**: Message persistence across connection drops
- **Multi-Protocol Support**: JSON, Binary, and MessagePack protocol switching
- **Interactive Dashboard**: Tabbed interface with real time stats
- **Connection Management**: Advanced WebSocket lifecycle handling

### 2. 🏠 Advanced Room System (`advanced_rooms/`)
Sophisticated room based messaging with advanced client management and statistics.

```bash
go run examples/websocket/advanced_rooms/main.go
# Visit http://localhost:8087
```

**Features:**
- Advanced room creation and management
- Real time client tracking per room
- Room statistics and analytics
- Multi client broadcasting
- Room based message history
- Client join/leave notifications

### 3. 💬 Simple Chat (`simple_chat/`)
A complete multiuser chat room demonstrating basic real time messaging patterns.

```bash
go run examples/websocket/simple_chat/main.go
# Visit http://localhost:8080
```

**Features:**
- Multi-user chat rooms with broadcasting
- User join/leave notifications
- Message history and user lists
- Room management commands (`/help`, `/users`, `/history`)
- Persistent message storage

### 4. 🎭 Event-Driven Chat (`event_system_chat/`)
Advanced chat system showcasing event driven architecture patterns.

```bash
go run examples/websocket/event_system_chat/main.go
# Visit http://localhost:8080
```

**Features:**
- Event based messaging (`ping`, `get_users`, `message`)
- User presence management
- Real time user list updates
- Event acknowledgment patterns
- Advanced message routing

### 5. 🔄 Echo Server (`echo/`)
Basic WebSocket echo server demonstrating fundamental communication patterns.

```bash
go run examples/websocket/echo/main.go
# Visit http://localhost:8080
```

**Features:**
- Simple echo messaging with enhanced responses
- Ping/pong frame handling
- JSON message communication
- Connection lifecycle callbacks
- Interactive HTML client

### 6. 🏪 Chat Room (`chat_room/` & `chatroom_standalone/`)
Different implementations of chat room functionality for comparison.

```bash
# Basic chat room
go run examples/websocket/chat_room/main.go

# Standalone version
go run examples/websocket/chatroom_standalone/main.go
```

### 7. 📡 JSON API (`jsonapi/`)
Demonstrates WebSocket API patterns with JSON messaging.

```bash
go run examples/websocket/jsonapi/main.go
```

### 8. ⚖️ Adapter Comparison (`ws_adapter/`)
**Unified interface demonstration** - Shows how identical WebSocket code works across different adapters.

```bash
# Run with Fiber adapter (default)
go run examples/websocket/ws_adapter/main.go
# Visit http://localhost:3000

# Or with HTTPRouter adapter
go run examples/websocket/ws_adapter/main.go httprouter
# Visit http://localhost:8080
```

**Features:**
- Same WebSocket handler code works with both Fiber and HTTPRouter
- Multiple WebSocket endpoints (`/ws/echo`, `/ws/chat`)
- Adapter switching via command line argument
- Side-by-side comparison interface
- Demonstrates true adapter portability

### 9. 🌐 Fiber Integration (`ws_fiber/`)
Shows WebSocket usage specifically with the Fiber adapter.

```bash
go run examples/websocket/ws_fiber/main.go
# Visit http://localhost:3000
```

### 10. 🔧 Simple Implementation (`simple/`)
Minimal WebSocket implementation for learning basic patterns.

```bash
go run examples/websocket/simple/main.go
```

## Unified WebSocket Interface

All examples use the new unified WebSocket interface that works across adapters:

```go
// Works with both Fiber and HTTPRouter adapters
app := router.NewHTTPServer() // or router.NewFiberAdapter()

config := router.DefaultWebSocketConfig()
config.Origins = []string{"*"}

handler := func(ws router.WebSocketContext) error {
    // Your WebSocket logic here
    return nil
}

app.Router().WebSocket("/ws", config, handler)
```

## Key Patterns Demonstrated

### Configuration
```go
config := router.DefaultWebSocketConfig()
config.Origins = []string{"*"}
config.OnConnect = func(ws router.WebSocketContext) error {
    log.Printf("Client connected: %s", ws.ConnectionID())
    return ws.WriteJSON(map[string]string{"type": "welcome"})
}
config.OnDisconnect = func(ws router.WebSocketContext, err error) {
    log.Printf("Client disconnected: %s", ws.ConnectionID())
}
```

### Message Handling
```go
func wsHandler(ws router.WebSocketContext) error {
    for {
        messageType, data, err := ws.ReadMessage()
        if err != nil {
            break
        }

        // Process message and respond
        response := processMessage(data)
        if err := ws.WriteJSON(response); err != nil {
            break
        }
    }
    return nil
}
```

### Broadcasting
```go
// Simple broadcast pattern
clients := make(map[string]router.WebSocketContext)
var clientsMutex sync.RWMutex

// Broadcast to all connected clients
clientsMutex.RLock()
for _, client := range clients {
    if err := client.WriteJSON(message); err != nil {
        // Handle error
    }
}
clientsMutex.RUnlock()
```

## Running Examples

Each example is completely standalone and can be run directly:

1. Navigate to the project root directory
2. Run any example: `go run examples/websocket/{example}/main.go`
3. Open your browser to the URL shown in the console output
4. Interact with the WebSocket interface

## Testing

All examples include comprehensive HTML clients for testing WebSocket functionality:

- **Connection Management**: Connect/disconnect buttons
- **Message Sending**: Text input with Enter key support
- **Real-time Display**: Message history with timestamps
- **Status Indicators**: Visual connection status
- **Command Support**: Special commands and events

## Adapter Compatibility

The unified WebSocket interface ensures your code works identically across adapters:

- **HTTPRouter**: Uses `gorilla/websocket` internally
- **Fiber**: Uses `gofiber/contrib/websocket` internally
- **Same API**: Identical `WebSocketContext` interface for both

Switch between adapters without changing your WebSocket handler code:

```go
// This handler works with ANY adapter
handler := func(ws router.WebSocketContext) error {
    return ws.WriteJSON(map[string]string{"message": "Hello!"})
}

// Use with HTTPRouter
app1 := router.NewHTTPServer()
app1.Router().WebSocket("/ws", config, handler)

// Use with Fiber - same handler!
app2 := router.NewFiberAdapter()
app2.Router().WebSocket("/ws", config, handler)
```



## 🧪 Testing Instructions

### Advanced Features Dashboard

The most comprehensive example with detailed testing instructions for each feature:

#### File Transfer System
```bash
go run examples/websocket/advanced/main.go
# Visit http://localhost:8080
```
1. **Connect** to File Transfer System
2. **Upload a file**: Select a file (max 8MB) and click "Upload File"
3. **Download files**: Click "List Files" then "Download" any file
4. **Monitor progress**: Watch real-time upload/download progress messages

#### Real-time Collaboration
1. **Open two browser tabs** to http://localhost:8080
2. **Both tabs**: Navigate to "Collaboration" tab and connect
3. **Tab 1**: Create a new document or select existing one
4. **Tab 2**: Join the same document
5. **Test real-time editing**: Type in either tab - changes appear instantly in both
6. **Version tracking**: Watch version numbers increment with each edit
7. **Multi-user awareness**: See "edited by [client-id]" messages

#### Reconnection & Message Queueing
1. **Tab 1**:
   - Connect → Note your session ID (e.g., `1757440355297688000`)
   - Click "Quick Test" → Queues a message to yourself
   - Disconnect → Should show "✅ Disconnected successfully"
2. **Tab 2**:
   - Connect with Tab 1's session ID → Should show "🎉 Successfully reconnected"
   - Should automatically receive the queued message
   - Click "Get Queued Messages" → Should show "📭 No queued messages found" (already delivered)
3. **Tab 1**: Send another message to that session while Tab 2 is connected
4. **Tab 2**: Click "Get Queued Messages" → Should show the new message

#### Multi-Protocol Support
1. **Connect** with "JSON Protocol" (default)
2. **Test functionality**: Send ping, echo messages - note "JSON: 1" counter
3. **Switch protocols**:
   - Change dropdown to "Binary Protocol" → Automatically reconnects
   - Test ping/echo → Note "Binary: 1, JSON: 0" counters
   - Switch to "MessagePack" → Note counter changes
4. **Reset stats**: Click "Reset Stats" to clear all counters

### Room Management (Advanced Rooms)
```bash
go run examples/websocket/advanced_rooms/main.go
# Visit http://localhost:8087
```
1. **Create rooms**: Enter room name and type, click "Create Room"
2. **Join rooms**: Select room from dropdown, click "Join Room"
3. **Multi-tab testing**: Open multiple tabs, join same room
4. **Send messages**: Type and send - should appear in all connected clients
5. **Monitor stats**: Watch real-time room statistics update

### Simple Chat Testing
```bash
go run examples/websocket/simple_chat/main.go
# Visit http://localhost:8080
```
1. **Multiple users**: Open several browser tabs
2. **Join chat**: Enter different usernames in each tab
3. **Send messages**: Messages broadcast to all connected users
4. **Commands**: Try `/help`, `/users`, `/history`
5. **User tracking**: Watch join/leave notifications

## 🔧 Running Examples

Each example is completely standalone:

1. **Navigate to project root**: `cd go-router/`
2. **Run any example**: `go run examples/websocket/{example_name}/main.go`
3. **Open browser**: Visit the URL shown in console output
4. **Start testing**: Use the interactive web interface

### Port Information
- **Advanced Dashboard**: http://localhost:8080
- **Advanced Rooms**: http://localhost:8087
- **Adapter Comparison**: http://localhost:3000 (Fiber) or http://localhost:8080 (HTTPRouter)
- **Most other examples**: http://localhost:8080
- **Fiber examples**: http://localhost:3000

## 🔍 Troubleshooting

### Common Issues

**"Connection failed" or "WebSocket error"**
- Ensure the server is running: `go run examples/websocket/{example}/main.go`
- Check the correct port in your browser URL
- Verify firewall isn't blocking the port

**"File upload failed" (Advanced example)**
- File size must be ≤ 8MB
- Ensure stable WebSocket connection before uploading
- Check browser console for specific error messages

**"Session not found" (Reconnection feature)**
- Server may have restarted (sessions are in-memory)
- Click "Clear Stored Session" to start fresh
- Ensure both tabs use the exact same session ID

**"Protocol stats not updating" (Multi-protocol)**
- Click "Reset Stats" to clear counters
- Ensure you're properly disconnecting before switching protocols
- Refresh stats manually with "Get Stats" button

**Real-time collaboration not syncing**
- Both clients must be connected to the collaboration system
- Both must join the same document ID
- Check that auto-save is working (should show "Saved" status)

## 🏗️ Architecture Patterns Demonstrated

### Connection Management
- WebSocket lifecycle handling (connect/disconnect/error)
- Connection pooling and cleanup
- Client identification and tracking

### Message Patterns
- Request/response messaging
- Real-time broadcasting
- Event-driven communication
- Binary and text message handling

### Advanced Features
- Session persistence and reconnection
- Message queuing and delivery
- Multi-client synchronization
- Protocol negotiation and switching
- File transfer over WebSocket
- Real-time collaborative editing

### Scalability Patterns
- Room-based message routing
- Client state management
- Resource cleanup and memory management
- Connection monitoring and health checks

## 📚 Learning Path

**Beginners**: Start with these examples in order:
1. `simple/` - Basic WebSocket concepts
2. `echo/` - Message handling patterns
3. `simple_chat/` - Multi-client broadcasting

**Intermediate**: Explore these features:
4. `event_system_chat/` - Event-driven architecture
5. `advanced_rooms/` - Room management
6. `ws_fiber/` - Framework integration

**Advanced**: Master these complex patterns:
7. `advanced/` - All advanced features in one comprehensive example

## 🤝 Contributing

When adding new WebSocket examples:
- Include comprehensive HTML test clients
- Add proper error handling and logging
- Document all features and testing steps
- Follow the established code patterns
- Update this README with your example

## 📜 License

These examples are part of the go-router project and follow the same license terms.
