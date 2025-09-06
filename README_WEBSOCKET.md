# WebSocket Documentation

This document provides comprehensive documentation for the WebSocket implementation in `go-router`, a high-level, event-driven WebSocket API that simplifies real-time application development.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Core Components](#core-components)
- [API Reference](#api-reference)
- [Middleware](#middleware)
- [Room Management](#room-management)
- [Event System](#event-system)
- [Examples](#examples)
- [Configuration](#configuration)
- [Error Handling](#error-handling)
- [Best Practices](#best-practices)

## Overview

The WebSocket implementation provides:

- **High-level abstractions** that eliminate boilerplate code
- **Event-driven architecture** for clean, maintainable code
- **Automatic lifecycle management** with built-in ping/pong handling
- **Room system** for targeted message broadcasting
- **Comprehensive middleware** for authentication, logging, metrics, and more
- **Context support** throughout the API
- **Type-safe JSON handling** with structured events
- **Client state management** for per-connection data

## Quick Start

### Simple Echo Server

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/goliatone/go-router"
)

func main() {
    app := router.NewFiberAdapter()

    // Simple WebSocket handler using NewWSHandler
    app.Router().Get("/ws", router.NewWSHandler(func(ctx context.Context, client router.WSClient) error {
        // Handle incoming messages
        client.OnMessage(func(ctx context.Context, data []byte) error {
            fmt.Printf("Received: %s\n", data)
            return client.Send([]byte("Echo: " + string(data)))
        })

        // Handle JSON events
        client.OnJSON("ping", func(ctx context.Context, data json.RawMessage) error {
            return client.SendJSON(map[string]string{"type": "pong"})
        })

        // Wait for disconnection
        <-ctx.Done()
        return nil
    }))

    log.Fatal(app.Serve(":8080"))
}
```

### Chat Room Application

```go
func main() {
    app := router.NewFiberAdapter()
    hub := router.NewWSHub()

    // Handle connections
    hub.OnConnect(func(ctx context.Context, client router.WSClient, _ any) error {
        username := client.Query("username", "Guest")
        client.Set("username", username)

        // Join the main chat room
        client.Join("main")

        // Broadcast join message
        client.Room("main").Emit("user_joined", map[string]string{
            "username": username,
            "message": username + " joined the chat",
        })

        return nil
    })

    // Handle chat messages
    hub.On("chat_message", func(ctx context.Context, client router.WSClient, data any) error {
        username := client.GetString("username")

        // Broadcast to everyone in the room
        client.Room("main").Emit("new_message", map[string]any{
            "username": username,
            "message": data,
            "timestamp": time.Now(),
        })

        return nil
    })

    app.Router().Get("/ws/chat", hub.Handler())
    log.Fatal(app.Serve(":8080"))
}
```

## Core Components

### WSHub

The `WSHub` is the central component that manages WebSocket connections, events, and room operations.

```go
// Create a new hub
hub := router.NewWSHub()

// Configure event handlers
hub.OnConnect(connectHandler)
hub.OnDisconnect(disconnectHandler)
hub.On("custom_event", eventHandler)
hub.OnError(errorHandler)

// Broadcasting
hub.Broadcast(data)
hub.BroadcastJSON(message)
hub.Room("room_name").Emit("event", data)
```

### WSClient

The `WSClient` interface provides per-connection operations and state management.

```go
// Message handling
client.OnMessage(messageHandler)
client.OnJSON("event_type", jsonHandler)

// Sending messages
client.Send([]byte("Hello"))
client.SendJSON(map[string]string{"type": "greeting"})

// Room operations
client.Join("room_name")
client.Leave("room_name")
client.Room("room_name").Emit("event", data)

// State management
client.Set("key", "value")
value := client.GetString("key")

// Connection management
client.Close(router.CloseNormalClosure, "goodbye")
```

### NewWSHandler

For simple use cases, `NewWSHandler` provides a minimal setup:

```go
handler := router.NewWSHandler(func(ctx context.Context, client router.WSClient) error {
    // Your WebSocket logic here
    return nil
})

app.Router().Get("/ws", handler)
```

## API Reference

### WSHub Methods

#### Connection Management
- `OnConnect(handler EventHandler)` - Register connect handler
- `OnDisconnect(handler EventHandler)` - Register disconnect handler
- `OnError(handler WSErrorHandler)` - Register error handler
- `Close()` - Shutdown the hub

#### Event System
- `On(event string, handler EventHandler)` - Register event handler
- `Emit(event string, data any)` - Trigger event for all clients
- `EmitWithContext(ctx context.Context, event string, data any)` - Trigger event with context

#### Broadcasting
- `Broadcast(data []byte)` - Send to all clients
- `BroadcastJSON(v any)` - Send JSON to all clients
- `BroadcastWithContext(ctx context.Context, data []byte)` - Send with context
- `Room(name string) RoomBroadcaster` - Get room broadcaster

#### Information
- `ClientCount()` - Get number of connected clients
- `Clients()` - Get all connected clients

### WSClient Methods

#### Identification
- `ID() string` - Get client unique ID
- `ConnectionID() string` - Get connection ID

#### Context
- `Context() context.Context` - Get client context
- `SetContext(ctx context.Context)` - Set client context

#### Message Handling
- `OnMessage(handler MessageHandler)` - Register message handler
- `OnJSON(event string, handler JSONHandler)` - Register JSON event handler

#### Sending Messages
- `Send(data []byte)` - Send raw data
- `SendJSON(v any)` - Send JSON data
- `SendWithContext(ctx context.Context, data []byte)` - Send with context
- `SendJSONWithContext(ctx context.Context, v any)` - Send JSON with context

#### Broadcasting
- `Broadcast(data []byte)` - Broadcast to all clients
- `BroadcastJSON(v any)` - Broadcast JSON to all clients
- `BroadcastWithContext(ctx context.Context, data []byte)` - Broadcast with context

#### Room Management
- `Join(room string)` - Join a room
- `Leave(room string)` - Leave a room
- `Room(name string) RoomBroadcaster` - Get room broadcaster
- `Rooms() []string` - Get joined rooms

#### State Management
- `Set(key string, value any)` - Set state value
- `Get(key string) any` - Get state value
- `GetString(key string) string` - Get string value
- `GetInt(key string) int` - Get integer value
- `GetBool(key string) bool` - Get boolean value

#### Connection Control
- `Close(code int, reason string)` - Close connection
- `IsConnected() bool` - Check connection status
- `Query(key string, defaultValue ...string) string` - Get query parameter

#### Events
- `Emit(event string, data any)` - Emit event to client
- `EmitWithContext(ctx context.Context, event string, data any)` - Emit event with context

## Middleware

The WebSocket middleware system provides cross-cutting concerns like authentication, logging, and error handling.

### Authentication Middleware

```go
// Token validator (implement WSTokenValidator interface)
type MyTokenValidator struct{}

func (v *MyTokenValidator) Validate(token string) (router.WSAuthClaims, error) {
    // Your token validation logic
    return claims, nil
}

// Configure auth middleware
authMiddleware := router.NewWSAuth(router.WSAuthConfig{
    TokenValidator: &MyTokenValidator{},
    TokenExtractor: func(ctx context.Context, client router.WSClient) (string, error) {
        // Extract token from query param or header
        return client.Query("token"), nil
    },
})

// Use with NewWSHandler
handler := router.NewWSHandler(authMiddleware(func(ctx context.Context, client router.WSClient) error {
    // Authenticated handler
    claims, _ := router.WSAuthClaimsFromContext(ctx)
    client.Set("user_id", claims.UserID())
    return nil
}))
```

### Logging Middleware

```go
loggerMiddleware := router.NewWSLogger(router.WSLoggerConfig{
    Logger: myLogger,
    Formatter: func(client router.WSClient, start, stop time.Time) string {
        return fmt.Sprintf("WS %s connected for %v", client.ID(), stop.Sub(start))
    },
})
```

### Metrics Middleware

```go
type MyMetricsSink struct{}

func (s *MyMetricsSink) RecordConnection(metrics router.WSConnectionMetrics) {
    // Send metrics to your monitoring system
}

metricsMiddleware := router.NewWSMetrics(router.WSMetricsConfig{
    Sink: &MyMetricsSink{},
})
```

### Rate Limiting Middleware

```go
rateLimitMiddleware := router.NewWSRateLimit(router.WSRateLimitConfig{
    Store: router.NewWSInMemoryRateLimitStore(rate.Limit(10), 5), // 10/sec, burst 5
    KeyFunc: func(client router.WSClient) string {
        return client.Conn().RemoteAddr() // Rate limit by IP
    },
})
```

### Recovery Middleware

```go
recoverMiddleware := router.NewWSRecover(router.WSRecoverConfig{
    Logger: myLogger,
    EnableStackTrace: true,
})
```

### Chaining Middleware

```go
chainedMiddleware := router.ChainWSMiddleware(
    recoverMiddleware,
    loggerMiddleware,
    authMiddleware,
    rateLimitMiddleware,
)

handler := router.NewWSHandler(chainedMiddleware(func(ctx context.Context, client router.WSClient) error {
    // Your handler with all middleware applied
    return nil
}))
```

## Room Management

The room system allows targeted message broadcasting and client grouping.

### Basic Room Operations

```go
// Join a room
client.Join("game-lobby")

// Leave a room
client.Leave("game-lobby")

// Get rooms client is in
rooms := client.Rooms()

// Broadcast to room
client.Room("game-lobby").Emit("player_joined", playerData)

// Broadcast to room except specific clients
client.Room("game-lobby").Except(client).Emit("other_player_moved", moveData)
```

### Advanced Room Broadcasting

```go
// Room broadcaster interface
room := client.Room("game-lobby")

// Emit events to room
room.Emit("event", data)
room.EmitWithContext(ctx, "event", data)

// Exclude specific clients
room.Except(client1, client2).Emit("event", data)

// Get room clients
clients := room.Clients()
```

### Room-based Chat Example

```go
hub := router.NewWSHub()

hub.OnConnect(func(ctx context.Context, client router.WSClient, _ any) error {
    // Join default room
    client.Join("general")

    // Announce join
    client.Room("general").Except(client).Emit("user_joined", map[string]string{
        "user": client.GetString("username"),
    })

    return nil
})

hub.On("join_room", func(ctx context.Context, client router.WSClient, data any) error {
    roomName := data.(string)
    client.Join(roomName)

    // Notify room
    client.Room(roomName).Emit("user_joined_room", map[string]string{
        "user": client.GetString("username"),
        "room": roomName,
    })

    return nil
})
```

## Event System

The event system provides structured communication between clients and server.

### Server-side Event Handling

```go
hub := router.NewWSHub()

// Handle custom events
hub.On("chat_message", func(ctx context.Context, client router.WSClient, data any) error {
    message := data.(map[string]any)

    // Process and broadcast
    response := map[string]any{
        "type": "new_message",
        "user": client.GetString("username"),
        "text": message["text"],
        "timestamp": time.Now(),
    }

    client.Room("chat").Emit("message", response)
    return nil
})

// Handle typed events with validation
hub.On("game_move", func(ctx context.Context, client router.WSClient, data any) error {
    move, ok := data.(GameMove)
    if !ok {
        return errors.New("invalid move data")
    }

    // Validate move
    if !isValidMove(move) {
        client.Emit("move_error", "Invalid move")
        return nil
    }

    // Broadcast move
    client.Room("game").Except(client).Emit("opponent_move", move)
    return nil
})
```

### Client-side Event Handling

```go
client.OnJSON("ping", func(ctx context.Context, data json.RawMessage) error {
    return client.SendJSON(map[string]string{"type": "pong"})
})

client.OnJSON("game_invite", func(ctx context.Context, data json.RawMessage) error {
    var invite GameInvite
    if err := json.Unmarshal(data, &invite); err != nil {
        return err
    }

    // Process invite
    return handleGameInvite(ctx, client, invite)
})
```

### Event Message Format

WebSocket events use a standard JSON format:

```json
{
    "type": "event_name",
    "data": {
        // event-specific data
    }
}
```

## Examples

### File Transfer WebSocket

```go
hub := router.NewWSHub()

hub.On("file_upload", func(ctx context.Context, client router.WSClient, data any) error {
    fileData := data.(map[string]any)
    fileName := fileData["name"].(string)
    content := fileData["content"].(string)

    // Decode base64 content
    decoded, err := base64.StdEncoding.DecodeString(content)
    if err != nil {
        client.Emit("upload_error", "Invalid file content")
        return nil
    }

    // Save file
    err = ioutil.WriteFile("uploads/"+fileName, decoded, 0644)
    if err != nil {
        client.Emit("upload_error", "Save failed")
        return nil
    }

    // Notify success
    client.Emit("upload_success", map[string]string{
        "file": fileName,
        "size": fmt.Sprintf("%d bytes", len(decoded)),
    })

    return nil
})
```

### Real-time Game State

```go
type GameState struct {
    Players []Player `json:"players"`
    Board   [][]int   `json:"board"`
    Turn    string    `json:"current_turn"`
}

hub := router.NewWSHub()

hub.On("make_move", func(ctx context.Context, client router.WSClient, data any) error {
    move := data.(GameMove)
    gameID := client.GetString("game_id")

    // Update game state
    newState, err := game.ProcessMove(gameID, move)
    if err != nil {
        client.Emit("invalid_move", err.Error())
        return nil
    }

    // Broadcast new state to game room
    client.Room(gameID).Emit("game_state", newState)

    // Check for game end
    if winner := game.CheckWinner(newState); winner != "" {
        client.Room(gameID).Emit("game_over", map[string]string{
            "winner": winner,
        })
    }

    return nil
})
```

### Live Dashboard with Metrics

```go
hub := router.NewWSHub()

// Periodic metrics broadcasting
go func() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            metrics := collectMetrics()
            hub.Room("dashboard").Emit("metrics_update", metrics)
        }
    }
}()

hub.OnConnect(func(ctx context.Context, client router.WSClient, _ any) error {
    // Join dashboard room if authorized
    if client.GetString("role") == "admin" {
        client.Join("dashboard")

        // Send current metrics
        client.Emit("metrics_update", collectMetrics())
    }

    return nil
})
```

## Configuration

### WebSocket Configuration

```go
config := router.WebSocketConfig{
    // Buffer sizes
    ReadBufferSize:  4096,
    WriteBufferSize: 4096,

    // Timeouts
    HandshakeTimeout: 10 * time.Second,
    ReadTimeout:      60 * time.Second,
    WriteTimeout:     10 * time.Second,

    // Keep-alive
    PingPeriod: 54 * time.Second,
    PongWait:   60 * time.Second,

    // Message limits
    MaxMessageSize: 1024 * 1024, // 1MB

    // Compression
    EnableCompression: true,
    CompressionLevel:  6,

    // Origins (CORS)
    Origins: []string{"https://myapp.com", "https://admin.myapp.com"},

    // Custom origin checker
    CheckOrigin: func(origin string) bool {
        return strings.HasSuffix(origin, ".myapp.com")
    },
}

// Apply configuration
app.Router().Get("/ws", handler, router.WebSocketUpgrade(config))
```

### Hub Configuration

```go
hubConfig := router.WSHubConfig{
    MaxMessageSize:    512 * 1024, // 512KB
    HandshakeTimeout:  15 * time.Second,
    ReadTimeout:       120 * time.Second,
    WriteTimeout:      30 * time.Second,
    PingPeriod:       45 * time.Second,
    PongWait:         60 * time.Second,
    ReadBufferSize:   2048,
    WriteBufferSize:  2048,
    EnableCompression: true,
}

hub := router.NewWSHub(func(config *router.WSHubConfig) {
    *config = hubConfig
})
```

## Error Handling

The WebSocket implementation follows a comprehensive error handling strategy.

### Error Types

1. **Connection Errors** - Handled by the hub's error handlers
2. **Message Errors** - Returned from event handlers
3. **System Errors** - Logged centrally with context

### Error Handling Example

```go
hub := router.NewWSHub()

// Global error handler
hub.OnError(func(ctx context.Context, client router.WSClient, err error) {
    log.Printf("WebSocket error for client %s: %v", client.ID(), err)

    // Optional: Send error to client
    client.Emit("error", map[string]string{
        "message": "An error occurred",
        "code":    "INTERNAL_ERROR",
    })
})

// Event handler with error handling
hub.On("risky_operation", func(ctx context.Context, client router.WSClient, data any) error {
    if err := validateInput(data); err != nil {
        // Return error to be handled by error handler
        return fmt.Errorf("validation failed: %w", err)
    }

    result, err := performOperation(data)
    if err != nil {
        // Send specific error to client
        client.Emit("operation_error", map[string]string{
            "message": err.Error(),
            "code":    "OPERATION_FAILED",
        })
        return nil // Don't trigger global error handler
    }

    client.Emit("operation_success", result)
    return nil
})
```

### Connection Error Codes

Use standard WebSocket close codes:

```go
// Normal closure
client.Close(router.CloseNormalClosure, "goodbye")

// Protocol error
client.Close(router.CloseProtocolError, "invalid message format")

// Policy violation (e.g., authentication failure)
client.Close(router.ClosePolicyViolation, "authentication required")

// Message too big
client.Close(router.CloseMessageTooBig, "message exceeds limit")

// Internal server error
client.Close(router.CloseInternalServerErr, "server error")
```

## Best Practices

### 1. Connection Lifecycle Management

```go
hub.OnConnect(func(ctx context.Context, client router.WSClient, _ any) error {
    // Set up client state
    client.Set("connected_at", time.Now())
    client.Set("user_id", extractUserID(ctx))

    // Join appropriate rooms
    userRole := extractUserRole(ctx)
    client.Join("users")
    if userRole == "admin" {
        client.Join("admins")
    }

    return nil
})

hub.OnDisconnect(func(ctx context.Context, client router.WSClient, _ any) error {
    // Clean up resources
    userID := client.GetString("user_id")
    cleanupUserResources(userID)

    // Notify other users
    client.Broadcast([]byte(fmt.Sprintf(`{"type":"user_offline","user":"%s"}`, userID)))

    return nil
})
```

### 2. Structured Event Handling

```go
// Define event types
type ChatMessage struct {
    Text      string    `json:"text"`
    Timestamp time.Time `json:"timestamp"`
    UserID    string    `json:"user_id"`
}

// Type-safe event handler
hub.On("chat_message", func(ctx context.Context, client router.WSClient, data any) error {
    var msg ChatMessage

    // Parse and validate
    msgBytes, _ := json.Marshal(data)
    if err := json.Unmarshal(msgBytes, &msg); err != nil {
        return fmt.Errorf("invalid message format: %w", err)
    }

    // Add metadata
    msg.UserID = client.GetString("user_id")
    msg.Timestamp = time.Now()

    // Validate content
    if len(msg.Text) == 0 || len(msg.Text) > 500 {
        client.Emit("message_error", "Message length must be 1-500 characters")
        return nil
    }

    // Broadcast to room
    roomID := client.GetString("room_id")
    client.Room(roomID).Emit("new_message", msg)

    return nil
})
```

### 3. State Management

```go
// Use typed getters with defaults
func getUserRole(client router.WSClient) string {
    if role := client.GetString("role"); role != "" {
        return role
    }
    return "user" // default
}

func isAuthenticated(client router.WSClient) bool {
    return client.Get("authenticated") == true
}

// Store complex state as JSON
type UserState struct {
    Preferences map[string]any `json:"preferences"`
    LastSeen    time.Time      `json:"last_seen"`
}

func setUserState(client router.WSClient, state UserState) {
    client.Set("user_state", state)
}

func getUserState(client router.WSClient) UserState {
    if state := client.Get("user_state"); state != nil {
        if userState, ok := state.(UserState); ok {
            return userState
        }
    }
    return UserState{Preferences: make(map[string]any)}
}
```

### 4. Room Strategy

```go
// Use hierarchical room names
client.Join("game:123")           // specific game
client.Join("game:123:spectators") // game spectators
client.Join("lobby")              // general lobby

// Dynamic room creation based on user attributes
func assignToRoom(client router.WSClient) {
    userLevel := client.GetInt("level")
    region := client.GetString("region")

    roomName := fmt.Sprintf("matchmaking:%s:level_%d", region, userLevel/10*10)
    client.Join(roomName)
}

// Clean room strategy for broadcasts
func broadcastGameUpdate(client router.WSClient, gameData GameData) {
    gameRoom := fmt.Sprintf("game:%s", gameData.ID)

    // Different data for players vs spectators
    client.Room(gameRoom).Emit("game_update", gameData.PlayerView())
    client.Room(gameRoom + ":spectators").Emit("game_update", gameData.SpectatorView())
}
```

### 5. Performance Considerations

```go
// Use context for cancellation
hub.On("long_operation", func(ctx context.Context, client router.WSClient, data any) error {
    result := make(chan Result)
    errCh := make(chan error)

    go func() {
        res, err := performLongOperation(data)
        if err != nil {
            errCh <- err
            return
        }
        result <- res
    }()

    select {
    case res := <-result:
        client.Emit("operation_complete", res)
    case err := <-errCh:
        client.Emit("operation_error", err.Error())
    case <-ctx.Done():
        return ctx.Err() // Client disconnected
    }

    return nil
})

// Batch operations for efficiency
type MessageBatch struct {
    Messages []Message `json:"messages"`
    RoomID   string    `json:"room_id"`
}

hub.On("batch_messages", func(ctx context.Context, client router.WSClient, data any) error {
    batch := data.(MessageBatch)

    // Process all messages at once
    processedMessages := processBatch(batch.Messages)

    // Single broadcast
    client.Room(batch.RoomID).Emit("message_batch", processedMessages)

    return nil
})
```

### 6. Security Best Practices

```go
// Always validate and sanitize input
hub.On("user_input", func(ctx context.Context, client router.WSClient, data any) error {
    input, ok := data.(string)
    if !ok {
        return errors.New("invalid input type")
    }

    // Sanitize
    sanitized := html.EscapeString(input)

    // Validate length
    if len(sanitized) > 1000 {
        client.Emit("input_error", "Input too long")
        return nil
    }

    // Check permissions
    if !canPerformAction(client, "send_message") {
        client.Emit("permission_error", "Not authorized")
        return nil
    }

    // Process safely
    processUserInput(sanitized)
    return nil
})

// Use authentication middleware
authMiddleware := router.NewWSAuth(router.WSAuthConfig{
    TokenValidator: myTokenValidator,
    OnAuthFailure: func(ctx context.Context, client router.WSClient, err error) error {
        log.Printf("Auth failure for %s: %v", client.ID(), err)
        return client.Close(router.ClosePolicyViolation, "Authentication required")
    },
})
```

This documentation provides a comprehensive guide to using the WebSocket implementation in `go-router`. The high-level API simplifies real-time application development while providing the flexibility and control needed for complex applications.
