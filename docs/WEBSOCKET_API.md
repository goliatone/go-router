# WebSocket API Reference

## Table of Contents
1. [Overview](#overview)
2. [Core Interfaces](#core-interfaces)
3. [Configuration](#configuration)
4. [Middleware](#middleware)
5. [Adapters](#adapters)
6. [Advanced Features](#advanced-features)
7. [Examples](#examples)
8. [Best Practices](#best-practices)
9. [Migration Guide](#migration-guide)
10. [Troubleshooting](#troubleshooting)

## Overview

The go-router WebSocket support provides a unified, adapter-agnostic interface for WebSocket communication across different HTTP frameworks. It supports both Fiber and HTTPRouter adapters with a consistent API.

### Key Features
- 🔌 Adapter-agnostic WebSocket interface
- 🏭 Factory pattern for context creation
- 🔧 Configurable middleware support
- 📊 JSON message handling
- ⏱️ Automatic deadline management
- 🔄 Subprotocol negotiation
- 🗜️ Compression support
- 🎯 Custom upgrader configuration
- ✅ **Guaranteed single hook invocation** - Lifecycle hooks called exactly once per connection
- 🔒 **Race-condition safe** - Thread-safe client management and state operations

## Core Interfaces

### WebSocketContext

The `WebSocketContext` interface extends the standard `Context` interface with WebSocket-specific functionality.

```go
type WebSocketContext interface {
    Context

    // Connection Management
    IsWebSocket() bool
    WebSocketUpgrade() error
    IsConnected() bool
    ConnectionID() string

    // Message Operations
    WriteMessage(messageType int, data []byte) error
    ReadMessage() (messageType int, p []byte, err error)
    WriteJSON(v any) error
    ReadJSON(v any) error

    // Connection Control
    Close() error
    CloseWithStatus(code int, reason string) error
    SetReadDeadline(t time.Time) error
    SetWriteDeadline(t time.Time) error

    // Ping/Pong
    WritePing(data []byte) error
    WritePong(data []byte) error
    SetPingHandler(handler func(data []byte) error)
    SetPongHandler(handler func(data []byte) error)
    SetCloseHandler(handler func(code int, text string) error)

    // Protocol Information
    Subprotocol() string
    Extensions() []string
    RemoteAddr() string
    LocalAddr() string
}
```

### Message Types

```go
const (
    TextMessage   = 1  // UTF-8 text data
    BinaryMessage = 2  // Binary data
    CloseMessage  = 8  // Connection close
    PingMessage   = 9  // Ping frame
    PongMessage   = 10 // Pong frame
)
```

### Close Codes

```go
const (
    CloseNormalClosure           = 1000 // Normal closure
    CloseGoingAway              = 1001 // Going away
    CloseProtocolError          = 1002 // Protocol error
    CloseUnsupportedData        = 1003 // Unsupported data
    CloseNoStatusReceived       = 1005 // No status received
    CloseAbnormalClosure        = 1006 // Abnormal closure
    CloseInvalidFramePayloadData = 1007 // Invalid payload
    ClosePolicyViolation        = 1008 // Policy violation
    CloseMessageTooBig          = 1009 // Message too big
    CloseMandatoryExtension     = 1010 // Mandatory extension
    CloseInternalServerErr      = 1011 // Internal error
    CloseServiceRestart         = 1012 // Service restart
    CloseTryAgainLater          = 1013 // Try again later
    CloseTLSHandshake           = 1015 // TLS handshake
)
```

## Configuration

### WebSocketConfig

Configure WebSocket behavior with the `WebSocketConfig` struct:

```go
config := WebSocketConfig{
    // Buffer sizes
    ReadBufferSize:   1024,
    WriteBufferSize:  1024,

    // Timeouts
    HandshakeTimeout: 10 * time.Second,
    ReadTimeout:      60 * time.Second, // Compatibility field for higher-level helpers
    WriteTimeout:     10 * time.Second,

    // Origins (CORS)
    Origins: []string{"https://example.com"},
    CheckOrigin: func(origin string) bool {
        return true // Custom origin validation
    },

    // Subprotocols
    Subprotocols: []string{"chat", "echo"},

    // Ping/Pong
    PingPeriod:          30 * time.Second,
    PongWait:            60 * time.Second,
    DisableKeepAlive:    false,
    DisableReadDeadline: false,

    // Event handlers
    OnConnect: func(ctx WebSocketContext) error {
        log.Printf("Client connected: %s", ctx.ConnectionID())
        return nil
    },
    OnDisconnect: func(ctx WebSocketContext, err error) {
        log.Printf("Client disconnected: %s", ctx.ConnectionID())
    },
    OnMessage: func(ctx WebSocketContext, msgType int, data []byte) error {
        log.Printf("Message received: %s", string(data))
        return nil
    },

    // Error handling
    ErrorHandler: func(ctx WebSocketContext, err error) {
        log.Printf("WebSocket error: %v", err)
    },

    // Metrics
    EnableMetrics: true,
    MetricsHandler: func(metric string, value any) {
        // Custom metrics collection
    },
}
```

### Default Configuration

```go
config := DefaultWebSocketConfig()
```

## Middleware

### WebSocket Upgrade Middleware

The `WebSocketUpgrade` middleware handles the WebSocket handshake:

```go
app.Router().Get("/ws", handler, WebSocketUpgrade(config))
```

### Usage Pattern

```go
func websocketHandler(ctx Context) error {
    // Cast to WebSocketContext
    wsCtx, ok := ctx.(WebSocketContext)
    if !ok {
        return ctx.Status(400).SendString("WebSocket required")
    }

    // Handle WebSocket communication
    for {
        msgType, data, err := wsCtx.ReadMessage()
        if err != nil {
            return err
        }

        // Process and respond
        if err := wsCtx.WriteMessage(msgType, data); err != nil {
            return err
        }
    }
}
```

## Adapters

### HTTPRouter Adapter

The HTTPRouter adapter uses gorilla/websocket for real WebSocket communication:

```go
// Register HTTPRouter factory
func init() {
    RegisterWebSocketFactory("httprouter", &HTTPRouterWebSocketFactory{})
}

// Usage with HTTPRouter
router := httprouter.New()
router.Handle("GET", "/ws", HTTPRouterWebSocketHandler(config, handler, views))
```

### Fiber Adapter

The Fiber adapter provides WebSocket support for Fiber v2:

```go
// Register Fiber factory
func init() {
    RegisterWebSocketFactory("fiber", &FiberWebSocketFactory{})
}

// Usage with Fiber
app := fiber.New()
app.Get("/ws", FiberWebSocketHandler(config, handler))
```

**Note**: Fiber v2 has limited WebSocket support. For production use, HTTPRouter is recommended.

## Advanced Features

### JSON Message Handling

#### JSONMessage Structure

```go
type JSONMessage struct {
    Type      string          `json:"type"`
    ID        string          `json:"id,omitempty"`
    Timestamp time.Time       `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}
```

#### JSON Message Router

```go
router := NewJSONMessageRouter(1024*1024) // 1MB limit

router.Register("chat", func(ctx WebSocketContext, msg *JSONMessage) error {
    // Handle chat messages
    return ctx.WriteJSON(map[string]string{
        "type": "chat_response",
        "message": "Message received",
    })
})

router.Register("command", func(ctx WebSocketContext, msg *JSONMessage) error {
    // Handle commands
    var cmd Command
    json.Unmarshal(msg.Data, &cmd)
    return processCommand(ctx, cmd)
})

// In handler
router.Route(wsContext)
```

### Deadline Management

Automatic connection timeout and health checks:

```go
config := WebSocketConfig{
    PingPeriod:   30 * time.Second,
    PongWait:     60 * time.Second,
    WriteTimeout: 10 * time.Second,
}

manager := NewDeadlineManager(wsContext, config)
manager.Start()
defer manager.Stop()

// Periodic health check
if err := manager.HealthCheck(); err != nil {
    log.Printf("Connection unhealthy: %v", err)
}
```

### Subprotocol Negotiation

```go
negotiator := NewSubprotocolNegotiator()

negotiator.Register(SubprotocolHandler{
    Name:    "chat",
    Version: "1.0",
    Validate: func(ctx WebSocketContext) error {
        // Validate protocol requirements
        return nil
    },
    Initialize: func(ctx WebSocketContext) error {
        // Initialize protocol
        return nil
    },
    HandleMessage: func(ctx WebSocketContext, msgType int, data []byte) error {
        // Handle protocol-specific messages
        return nil
    },
})

// Negotiate protocol
requested := []string{"chat", "echo"}
selected, handler := negotiator.NegotiateProtocol(requested)
```

### Compression Configuration

```go
compression := CompressionConfig{
    Enabled:           true,
    Level:             6,        // 1-9
    Threshold:         1024,     // Compress messages > 1KB
    NoContextTakeover: false,
}

manager := NewCompressionManager(compression)

if manager.ShouldCompress(data) {
    // Message will be compressed
}
```

### Custom Upgrader

```go
upgrader := NewCustomUpgrader(CustomUpgraderConfig{
    ReadBufferSize:   4096,
    WriteBufferSize:  4096,
    HandshakeTimeout: 10 * time.Second,
    Subprotocols:     []string{"chat", "echo"},

    BeforeUpgrade: func(ctx Context) error {
        // Pre-upgrade validation
        if !isAuthorized(ctx) {
            return errors.New("unauthorized")
        }
        return nil
    },

    AfterUpgrade: func(ctx WebSocketContext) error {
        // Post-upgrade initialization
        return sendWelcomeMessage(ctx)
    },

    ValidateOrigin: func(origin string) bool {
        return isAllowedOrigin(origin)
    },

    ValidateHeaders: func(headers map[string]string) error {
        // Custom header validation
        return nil
    },

    ErrorHandler: func(ctx Context, err error) {
        log.Printf("Upgrade error: %v", err)
    },
})

wsCtx, err := upgrader.Upgrade(httpContext)
```

### Connection Pool

Manage multiple WebSocket connections:

```go
pool := NewConnectionPool(100) // Max 100 connections

// Add connection
if err := pool.Add(wsContext); err != nil {
    log.Printf("Pool full: %v", err)
}

// Get connection
if ctx, ok := pool.Get(connectionID); ok {
    ctx.WriteMessage(TextMessage, []byte("Hello"))
}

// Broadcast to all
pool.Broadcast(TextMessage, []byte("Server announcement"))

// Get all connections
for _, ctx := range pool.GetAll() {
    // Process each connection
}

// Remove connection
pool.Remove(connectionID)

// Close all connections
pool.CloseAll()
```

### Broadcasting

Broadcast messages to multiple connections:

```go
connections := []WebSocketContext{ctx1, ctx2, ctx3}

// Broadcast JSON with size limit
err := BroadcastJSON(connections, map[string]any{
    "type": "notification",
    "message": "Server update",
}, 1024*1024) // 1MB limit

if err != nil {
    log.Printf("Broadcast failed: %v", err)
}
```

## Examples

### Echo Server

```go
func echoHandler(ctx Context) error {
    wsCtx, ok := ctx.(WebSocketContext)
    if !ok {
        return ctx.Status(400).SendString("WebSocket required")
    }

    for {
        msgType, data, err := wsCtx.ReadMessage()
        if err != nil {
            return err
        }

        if err := wsCtx.WriteMessage(msgType, data); err != nil {
            return err
        }
    }
}
```

### Chat Room

```go
type ChatRoom struct {
    clients map[string]WebSocketContext
    broadcast chan []byte
    mu sync.RWMutex
}

func (room *ChatRoom) handleClient(ctx WebSocketContext) {
    room.mu.Lock()
    room.clients[ctx.ConnectionID()] = ctx
    room.mu.Unlock()

    defer func() {
        room.mu.Lock()
        delete(room.clients, ctx.ConnectionID())
        room.mu.Unlock()
    }()

    for {
        _, msg, err := ctx.ReadMessage()
        if err != nil {
            break
        }
        room.broadcast <- msg
    }
}

func (room *ChatRoom) broadcaster() {
    for msg := range room.broadcast {
        room.mu.RLock()
        for _, client := range room.clients {
            client.WriteMessage(TextMessage, msg)
        }
        room.mu.RUnlock()
    }
}
```

### JSON API

```go
func apiHandler(ctx WebSocketContext) error {
    router := NewJSONMessageRouter(1024*1024)

    router.Register("get", func(ctx WebSocketContext, msg *JSONMessage) error {
        var req GetRequest
        json.Unmarshal(msg.Data, &req)

        data := fetchData(req.Resource, req.ID)
        return ctx.WriteJSON(JSONMessage{
            Type: "response",
            ID:   msg.ID,
            Data: data,
        })
    })

    router.Register("subscribe", func(ctx WebSocketContext, msg *JSONMessage) error {
        var sub SubscribeRequest
        json.Unmarshal(msg.Data, &sub)

        // Add to subscribers
        addSubscriber(sub.Topic, ctx)

        return ctx.WriteJSON(JSONMessage{
            Type: "subscribed",
            ID:   msg.ID,
        })
    })

    return router.Route(ctx)
}
```

## Best Practices

### 1. Connection Management

- Always use connection IDs for tracking
- Implement proper cleanup on disconnect
- Set appropriate timeouts
- Use connection pools for scalability

### 2. Error Handling

```go
func handleWebSocket(ctx WebSocketContext) error {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Panic recovered: %v", r)
            ctx.CloseWithStatus(CloseInternalServerErr, "Internal error")
        }
    }()

    // Set error handler
    ctx.SetCloseHandler(func(code int, text string) error {
        log.Printf("Connection closed: %d - %s", code, text)
        return nil
    })

    // Main loop with error handling
    for {
        msgType, data, err := ctx.ReadMessage()
        if err != nil {
            if websocket.IsUnexpectedCloseError(err,
                CloseGoingAway, CloseAbnormalClosure) {
                log.Printf("Unexpected close: %v", err)
            }
            return err
        }

        if err := processMessage(ctx, msgType, data); err != nil {
            log.Printf("Process error: %v", err)
            // Continue processing other messages
        }
    }
}
```

### 3. Message Size Limits

```go
const maxMessageSize = 1024 * 1024 // 1MB

func validateMessage(data []byte) error {
    if len(data) > maxMessageSize {
        return &WebSocketError{
            Code: CloseMessageTooBig,
            Message: "Message exceeds size limit",
        }
    }
    return nil
}
```

### 4. Concurrency

```go
type SafeConnection struct {
    ctx WebSocketContext
    writeMu sync.Mutex
}

func (c *SafeConnection) WriteMessage(msgType int, data []byte) error {
    c.writeMu.Lock()
    defer c.writeMu.Unlock()
    return c.ctx.WriteMessage(msgType, data)
}
```

### 5. Graceful Shutdown

```go
func gracefulShutdown(pool *ConnectionPool) {
    // Notify all clients
    pool.Broadcast(TextMessage, []byte("Server shutting down"))

    // Give clients time to disconnect
    time.Sleep(5 * time.Second)

    // Close remaining connections
    pool.CloseAll()
}
```

## Migration Guide

### From Standard net/http

```go
// Before: Using gorilla/websocket directly
upgrader := websocket.Upgrader{}
conn, err := upgrader.Upgrade(w, r, nil)

// After: Using go-router WebSocket
config := DefaultWebSocketConfig()
app.Router().Get("/ws", handler, WebSocketUpgrade(config))
```

### From Fiber WebSocket

```go
// Before: Fiber v2 WebSocket
app.Get("/ws", websocket.New(func(c *websocket.Conn) {
    // Handle connection
}))

// After: go-router WebSocket
app.Router().Get("/ws", handler, WebSocketUpgrade(config))
```

### From Echo WebSocket

```go
// Before: Echo WebSocket
e.GET("/ws", func(c echo.Context) error {
    websocket.Handler(func(ws *websocket.Conn) {
        // Handle connection
    }).ServeHTTP(c.Response(), c.Request())
    return nil
})

// After: go-router WebSocket
app.Router().Get("/ws", handler, WebSocketUpgrade(config))
```

## Troubleshooting

### Common Issues

#### 1. Connection Upgrade Fails

**Problem**: WebSocket upgrade returns 400 Bad Request

**Solution**:
```go
config := WebSocketConfig{
    CheckOrigin: func(origin string) bool {
        return true // Allow all origins for debugging
    },
}
```

#### 2. Messages Not Received

**Problem**: Client sends messages but server doesn't receive them

**Solution**:
```go
// Ensure you're reading in a loop
for {
    msgType, data, err := ctx.ReadMessage()
    if err != nil {
        log.Printf("Read error: %v", err)
        break
    }
    // Process message
}
```

#### 3. Connection Timeout

**Problem**: Connection closes after period of inactivity

**Solution**:
```go
config := WebSocketConfig{
    PingPeriod: 30 * time.Second,
    PongWait:   60 * time.Second,
}

// Or use deadline manager
manager := NewDeadlineManager(ctx, config)
manager.Start()
```

#### 4. Memory Leaks

**Problem**: Memory usage increases over time

**Solution**:
```go
// Ensure proper cleanup
defer func() {
    ctx.Close()
    removeFromPool(ctx.ConnectionID())
    cancelSubscriptions(ctx.ConnectionID())
}()
```

#### 5. Concurrent Write Panic

**Problem**: "concurrent write to websocket connection" panic

**Solution**:
```go
type SafeConn struct {
    ctx WebSocketContext
    mu  sync.Mutex
}

func (c *SafeConn) WriteMessage(mt int, data []byte) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.ctx.WriteMessage(mt, data)
}
```

### Debug Logging

Enable detailed logging for troubleshooting:

```go
config := WebSocketConfig{
    OnConnect: func(ctx WebSocketContext) error {
        log.Printf("[WS] Connected: %s from %s",
            ctx.ConnectionID(), ctx.RemoteAddr())
        return nil
    },
    OnMessage: func(ctx WebSocketContext, mt int, data []byte) error {
        log.Printf("[WS] Message: type=%d size=%d from=%s",
            mt, len(data), ctx.ConnectionID())
        return nil
    },
    OnDisconnect: func(ctx WebSocketContext, err error) {
        log.Printf("[WS] Disconnected: %s error=%v",
            ctx.ConnectionID(), err)
    },
    ErrorHandler: func(ctx WebSocketContext, err error) {
        log.Printf("[WS] Error: %s error=%v",
            ctx.ConnectionID(), err)
    },
}
```

### Performance Monitoring

```go
config := WebSocketConfig{
    EnableMetrics: true,
    MetricsHandler: func(metric string, value any) {
        switch metric {
        case "message_received":
            metrics.IncrementCounter("ws.messages.received")
        case "message_sent":
            metrics.IncrementCounter("ws.messages.sent")
        case "connection_duration":
            metrics.RecordHistogram("ws.connection.duration", value.(float64))
        }
    },
}
```

## Security Considerations

### Origin Validation

```go
config := WebSocketConfig{
    Origins: []string{
        "https://example.com",
        "https://*.example.com",
    },
    CheckOrigin: func(origin string) bool {
        // Custom validation logic
        return isValidOrigin(origin)
    },
}
```

### Authentication

```go
func authenticatedWebSocket(ctx Context) error {
    // Verify authentication before upgrade
    token := ctx.Header("Authorization")
    if !validateToken(token) {
        return ctx.Status(401).SendString("Unauthorized")
    }

    // Proceed with WebSocket
    wsCtx, ok := ctx.(WebSocketContext)
    if !ok {
        return ctx.Status(400).SendString("WebSocket required")
    }

    // Handle authenticated connection
    return handleAuthenticatedConnection(wsCtx)
}
```

### Rate Limiting

```go
type RateLimiter struct {
    connections map[string]int
    mu          sync.RWMutex
}

func (rl *RateLimiter) Allow(ip string) bool {
    rl.mu.Lock()
    defer rl.mu.Unlock()

    if rl.connections[ip] >= 10 { // Max 10 connections per IP
        return false
    }

    rl.connections[ip]++
    return true
}
```

### Input Validation

```go
func validateInput(data []byte) error {
    // Check size
    if len(data) > maxMessageSize {
        return errors.New("message too large")
    }

    // Validate JSON if expected
    if !json.Valid(data) {
        return errors.New("invalid JSON")
    }

    // Check for malicious content
    if containsMaliciousContent(data) {
        return errors.New("malicious content detected")
    }

    return nil
}
```

## Performance Optimization

### Buffer Management

```go
config := WebSocketConfig{
    ReadBufferSize:  4096, // Increase for large messages
    WriteBufferSize: 4096, // Increase for high throughput
}
```

### Message Batching

```go
type MessageBatcher struct {
    messages [][]byte
    mu       sync.Mutex
    ticker   *time.Ticker
}

func (b *MessageBatcher) Add(msg []byte) {
    b.mu.Lock()
    b.messages = append(b.messages, msg)
    b.mu.Unlock()
}

func (b *MessageBatcher) Flush(ctx WebSocketContext) {
    b.mu.Lock()
    messages := b.messages
    b.messages = nil
    b.mu.Unlock()

    if len(messages) > 0 {
        batch := combineBatch(messages)
        ctx.WriteMessage(BinaryMessage, batch)
    }
}
```

### Connection Pooling

```go
type ConnectionManager struct {
    pools map[string]*ConnectionPool
    mu    sync.RWMutex
}

func (m *ConnectionManager) GetPool(room string) *ConnectionPool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.pools[room]
}
```

## Testing

### Unit Testing

```go
func TestWebSocketHandler(t *testing.T) {
    // Create mock context
    ctx := newMockWebSocketContext()
    ctx.mockUpgrade()

    // Test message handling
    ctx.WriteMessage(TextMessage, []byte("test"))

    msgType, data, err := ctx.ReadMessage()
    assert.NoError(t, err)
    assert.Equal(t, TextMessage, msgType)
    assert.Equal(t, "test", string(data))
}
```

### Integration Testing

```go
func TestWebSocketIntegration(t *testing.T) {
    // Create test server
    server := httptest.NewServer(router)
    defer server.Close()

    // Connect client
    wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
    ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
    require.NoError(t, err)
    defer ws.Close()

    // Test communication
    err = ws.WriteMessage(websocket.TextMessage, []byte("ping"))
    require.NoError(t, err)

    _, data, err := ws.ReadMessage()
    require.NoError(t, err)
    assert.Equal(t, "pong", string(data))
}
```

### Load Testing

```go
func TestWebSocketLoad(t *testing.T) {
    server := startTestServer()
    defer server.Close()

    var wg sync.WaitGroup
    errors := make(chan error, 100)

    // Spawn concurrent clients
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()

            client := connectClient(server.URL)
            defer client.Close()

            // Send messages
            for j := 0; j < 100; j++ {
                msg := fmt.Sprintf("Message %d-%d", id, j)
                if err := client.WriteMessage(TextMessage, []byte(msg)); err != nil {
                    errors <- err
                    return
                }
            }
        }(i)
    }

    wg.Wait()
    close(errors)

    // Check for errors
    for err := range errors {
        t.Errorf("Load test error: %v", err)
    }
}
```

## Conclusion

The go-router WebSocket implementation provides a robust, scalable, and easy-to-use solution for real-time communication in Go applications. With support for multiple adapters, advanced features, and comprehensive testing, it's suitable for production use in a wide range of applications.

For more examples and the latest updates, visit the [GitHub repository](https://github.com/goliatone/go-router).
