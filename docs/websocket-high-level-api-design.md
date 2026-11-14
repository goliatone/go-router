# High-Level WebSocket API Design Document

## Executive Summary

This document explores design alternatives for creating a higher-level WebSocket abstraction layer for the go-router package, inspired by the developer experience of Socket.IO and ws (Node.js). The current implementation requires manual management of read/write pumps, channels, and connection lifecycle, which increases complexity and potential for errors.

## Current State Analysis

### Pain Points in Current Implementation

1. **Manual Pump Management**: Developers must implement read and write pumps manually
2. **Channel Management**: Direct handling of Go channels for message passing
3. **Boilerplate Code**: Significant repetitive code for basic functionality
4. **Connection Lifecycle**: Manual management of connection states and cleanup
5. **Error Handling**: Complex error propagation and recovery patterns
6. **Message Routing**: No built-in support for event-based message routing

### Example: Current Chat Implementation Complexity
```go
// Current approach requires ~300 lines for basic chat
go client.writePump()  // Manual pump
client.readPump()      // Blocking read
// Manual channel management, registration, etc.
```

## Design Goals

1. **Simplicity**: Reduce boilerplate code by 70-80%
2. **Safety**: Automatic resource management and cleanup
3. **Flexibility**: Support both simple and complex use cases
4. **Performance**: Maintain Go's performance characteristics
5. **Compatibility**: Work seamlessly with existing router package
6. **Familiarity**: Provide API patterns familiar to Socket.IO/ws users
7. **Idiomatic Go**: Proper context propagation and error handling
8. **Observability**: Built-in support for tracing, metrics, and logging through context

## Proposed Design Alternatives

### Alternative 1: Event-Driven API (Socket.IO Style)

#### Overview
Implement an event-based system similar to Socket.IO with automatic message routing and room management.

#### API Example
```go
// Server-side
ws := router.NewWebSocketHub()

ws.OnConnect(func(ctx context.Context, client *router.WSClient) error {
    log.Printf("Client connected: %s", client.ID())

    // Auto-join rooms with context
    if err := client.JoinWithContext(ctx, "public"); err != nil {
        return err
    }
    if err := client.JoinWithContext(ctx, "user:" + client.ID()); err != nil {
        return err
    }
    return nil
})

ws.On("message", func(ctx context.Context, client *router.WSClient, data any) error {
    // Broadcast to room with timeout
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    return client.Room("public").EmitWithContext(ctx, "message", map[string]any{
        "user": client.Get("username"),
        "text": data,
        "time": time.Now(),
    })
})

ws.On("typing", func(ctx context.Context, client *router.WSClient, data any) error {
    return client.Room("public").Except(client).EmitWithContext(ctx, "typing", map[string]any{
        "user": client.Get("username"),
    })
})

ws.OnDisconnect(func(ctx context.Context, client *router.WSClient) error {
    return ws.BroadcastWithContext(ctx, "user-left", client.Get("username"))
})

// Register with router
app.Router().WebSocket("/ws", ws.Handler())
```

#### Client-side (JavaScript)
```javascript
const ws = new WebSocketClient('ws://localhost:8080/ws');

ws.on('connect', () => {
    console.log('Connected');
});

ws.emit('message', 'Hello World');

ws.on('message', (data) => {
    console.log('Received:', data);
});
```

#### Pros
- Familiar to Socket.IO users
- Clean event-based API
- Automatic room management
- Built-in broadcasting capabilities
- Client state management
- Proper error propagation through returns
- Context-aware for cancellation and timeouts

#### Cons
- Requires custom protocol for event routing
- More complex internal implementation
- Potential performance overhead from reflection

### Alternative 2: Functional Middleware Chain

#### Overview
Use a middleware-based approach with composable handlers, similar to Express.js middleware.

#### API Example
```go
// Create WebSocket handler with middleware chain
wsHandler := router.NewWSHandler(
    router.WSAuth(),           // Authentication middleware
    router.WSRateLimit(100),   // Rate limiting
    router.WSCompress(),        // Compression
    router.WSRecover(),         // Panic recovery
).Handle(func(ctx context.Context, ws *router.WSContext) error {
    // Simple echo server with context
    ws.OnMessage(func(msgCtx context.Context, msg router.Message) error {
        // Context includes request-scoped values, deadlines
        if err := validateMessage(msgCtx, msg); err != nil {
            return fmt.Errorf("validation failed: %w", err)
        }
        return ws.SendWithContext(msgCtx, msg)
    })

    // Or pattern matching with error handling
    ws.OnJSON("chat.message", func(msgCtx context.Context, data ChatMessage) error {
        // Framework handles errors - logs, closes connection if needed
        if data.Message == "" {
            return router.ErrEmptyMessage
        }
        return ws.BroadcastWithContext(msgCtx, "chat.message", data)
    })

    return nil // Return error to reject connection
})

app.Router().Get("/ws", wsHandler)
```

#### Pros
- Composable and reusable components
- Clear separation of concerns
- Easy to test individual middlewares
- Familiar pattern from HTTP middleware
- Natural error handling through return values
- Full context support for request lifecycle

#### Cons
- Less intuitive for complex interactions
- May require more setup for simple use cases

### Alternative 3: Actor Model Approach

#### Overview
Each WebSocket connection is an actor that processes messages asynchronously.

#### API Example
```go
type ChatActor struct {
    router.WSActor
}

func (a *ChatActor) Receive(ctx context.Context, msg router.WSMessage) error {
    // Context carries request-scoped values and deadlines
    switch m := msg.(type) {
    case router.TextMessage:
        // Broadcast to all actors in group with context
        return a.TellWithContext(ctx, "broadcast", m, a.Group("chat"))
    case router.UserMessage:
        // Handle custom message types with error handling
        if m.Data == nil {
            return router.ErrInvalidMessage
        }
        return a.ReplyWithContext(ctx, router.TextMessage{
            Data: fmt.Sprintf("Echo: %s", m.Data),
        })
    }
    return nil
}

// Register actor factory
ws := router.NewWSSystem()
ws.Register("chat", func() router.Actor {
    return &ChatActor{}
})

app.Router().WebSocket("/ws", ws.Handler())
```

#### Pros
- Natural concurrency model
- Fault isolation
- Scalable to distributed systems
- Clear message flow

#### Cons
- Learning curve for actor model
- More complex for simple use cases
- Potential overhead for small applications

### Alternative 4: Stream-Based API

#### Overview
Treat WebSocket connections as bidirectional streams with operators for transformation and filtering.

#### API Example
```go
ws := router.NewWebSocketStream()

ws.Connect().
    Authenticate(authFunc).
    Parse(router.JSON).
    Filter(func(ctx context.Context, msg router.Message) (bool, error) {
        // Can check context for cancellation
        select {
        case <-ctx.Done():
            return false, ctx.Err()
        default:
            return msg.Type == "chat", nil
        }
    }).
    Map(func(ctx context.Context, msg router.Message) (router.Message, error) {
        msg.Data["timestamp"] = time.Now()
        return msg, nil
    }).
    BroadcastWithContext()

// Or using channels with context
ctx := context.Background()
incoming := ws.MessagesWithContext(ctx)
outgoing := ws.SenderWithContext(ctx)

go func() {
    for {
        select {
        case msg := <-incoming:
            processed, err := processMessage(ctx, msg)
            if err != nil {
                ws.HandleError(err)
                continue
            }
            outgoing <- processed
        case <-ctx.Done():
            return
        }
    }
}()
```

#### Pros
- Composable stream operations
- Natural for data transformation
- Good for real-time data processing
- Familiar to RxJS/reactive programming users

#### Cons
- Can be complex for stateful operations
- Learning curve for stream concepts

### Alternative 5: Declarative Configuration

#### Overview
Define WebSocket behavior through declarative configuration with minimal code.

#### API Example
```go
ws := router.WebSocketConfig{
    Routes: router.WSRoutes{
        "chat.message": router.BroadcastHandler("room:public"),
        "user.typing":  router.BroadcastExceptSender("room:public"),
        "user.join":    router.RoomJoinHandler(),
        "user.leave":   router.RoomLeaveHandler(),
    },

    Rooms: router.RoomConfig{
        AutoJoin: []string{"public"},
        MaxRooms: 10,
    },

    Middleware: []router.WSMiddleware{
        router.WSLogger(),
        router.WSRecover(),
        router.WSRateLimit(100),
    },

    OnConnect: func(ctx context.Context, client router.Client) error {
        // Can use context for timeout on connection setup
        setupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()

        username := client.Query("username")
        if username == "" {
            return router.ErrMissingUsername
        }
        client.SetWithContext(setupCtx, "username", username)
        return nil
    },

    OnError: func(ctx context.Context, client router.Client, err error) {
        // Centralized error handling
        log.Printf("WebSocket error for client %s: %v", client.ID(), err)
        if errors.Is(err, router.ErrRateLimitExceeded) {
            client.Close(router.CloseRateLimitExceeded)
        }
    },
}

app.Router().WebSocket("/ws", ws.Handler())
```

#### Pros
- Very concise for common patterns
- Easy to understand and maintain
- Good for standard use cases
- Configuration over code

#### Cons
- Less flexible for complex logic
- May need escape hatches for custom behavior

## Context and Error Handling Design Principles

### Context Propagation

Context is a first-class citizen in the API design, providing:

1. **Request Lifecycle Management**
   - Cancellation propagation through the entire request chain
   - Deadline enforcement for long-running operations
   - Graceful shutdown coordination

2. **Request-Scoped Values**
   - Authentication/authorization data
   - Request IDs for distributed tracing
   - User preferences and locale settings
   - Rate limiting quotas

3. **Observability Integration**
   ```go
   // Context carries tracing spans, metrics labels, and logger
   hub.On("message", func(ctx context.Context, client router.WSClient, msg Message) error {
       span, ctx := trace.StartSpan(ctx, "websocket.message")
       defer span.End()

       logger := log.FromContext(ctx)
       logger.Info("processing message", "client_id", client.ID())

       // Context flows through all operations
       return client.Room("chat").EmitWithContext(ctx, "message", msg)
   })
   ```

### Error Handling Strategy

1. **Return Errors, Don't Panic**
   - All handlers return errors for proper propagation
   - Framework handles logging and connection management
   - Panic recovery as middleware, not default behavior

2. **Error Classification**
   ```go
   // Well-defined error types for different scenarios
   var (
       ErrMessageTooLarge    = WSError{Code: 1009, Reason: "message too large"}
       ErrInvalidMessage     = WSError{Code: 1003, Reason: "invalid message format"}
       ErrRateLimitExceeded  = WSError{Code: 1008, Reason: "rate limit exceeded"}
       ErrUnauthorized       = WSError{Code: 1008, Reason: "unauthorized"}
       ErrInternalServer     = WSError{Code: 1011, Reason: "internal server error"}
   )
   ```

3. **Centralized Error Handling**
   ```go
   hub.OnError(func(ctx context.Context, client router.WSClient, err error) {
       // Extract metadata from context
       requestID := middleware.GetRequestID(ctx)
       userID := auth.GetUserID(ctx)

       // Log with context
       log.ErrorContext(ctx, "websocket error",
           "request_id", requestID,
           "user_id", userID,
           "client_id", client.ID(),
           "error", err,
       )

       // Send appropriate close frame based on error type
       var wsErr WSError
       if errors.As(err, &wsErr) {
           client.Close(wsErr.Code, wsErr.Reason)
       } else if errors.Is(err, context.DeadlineExceeded) {
           client.Close(1001, "timeout")
       } else {
           // Don't leak internal errors to client
           client.Close(1011, "internal error")
       }
   })
   ```

4. **Graceful Degradation**
   - Non-critical errors don't close connections
   - Retry logic for transient failures
   - Circuit breakers for downstream services

## Recommended Hybrid Approach

Based on analysis, we recommend a **hybrid approach** that combines the best aspects, with context and error handling as core design principles:

### Core API Design

```go
// 1. Simple API for basic use cases with error handling
ws := router.NewWSHandler(func(ctx context.Context, client router.WSClient) error {
    client.OnMessage(func(msgCtx context.Context, msg []byte) error {
        // Framework handles errors appropriately
        if len(msg) > 1024*1024 {
            return router.ErrMessageTooLarge
        }
        return client.BroadcastWithContext(msgCtx, msg)
    })
    return nil
})

// 2. Event-driven API for complex applications
hub := router.NewWSHub()

hub.OnConnect(func(ctx context.Context, client router.WSClient) error {
    // Context for connection setup with timeout
    setupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
    defer cancel()

    return client.JoinWithContext(setupCtx, "lobby")
})

hub.On("chat:message", func(ctx context.Context, client router.WSClient, msg ChatMessage) error {
    // Validate message
    if err := msg.Validate(); err != nil {
        return fmt.Errorf("invalid message: %w", err)
    }

    // Context propagates through the entire chain
    return client.Room("lobby").EmitWithContext(ctx, "chat:message", msg)
})

// 3. Middleware support with context propagation
hub.Use(
    router.WSAuth(),      // Adds auth info to context
    router.WSLogger(),    // Logs with request ID from context
    router.WSMetrics(),   // Tracks metrics with context labels
    router.WSTimeout(30*time.Second), // Sets context deadline
)

// 4. Stream operators with error handling
hub.Messages().
    FilterWithError(func(ctx context.Context, msg router.Message) (bool, error) {
        return router.ValidJSON(ctx, msg)
    }).
    MapWithError(func(ctx context.Context, msg router.Message) (router.Message, error) {
        return router.AddTimestamp(ctx, msg)
    }).
    Subscribe(func(ctx context.Context, msg router.Message) error {
        return hub.BroadcastWithContext(ctx, msg)
    })

// 5. Low-level access when needed
hub.OnRawConnection(func(ctx context.Context, conn router.WebSocketContext) error {
    // Direct access with context support
    // for performance-critical paths
    return handleRawConnection(ctx, conn)
})

// 6. Global error handler
hub.OnError(func(ctx context.Context, client router.WSClient, err error) {
    // Centralized error handling
    logger := router.LoggerFromContext(ctx)
    logger.Error("websocket error", "client", client.ID(), "error", err)

    // Different handling based on error type
    switch {
    case errors.Is(err, context.DeadlineExceeded):
        client.Close(router.CloseGoingAway, "timeout")
    case errors.Is(err, router.ErrRateLimitExceeded):
        client.Close(router.ClosePolicyViolation, "rate limit exceeded")
    default:
        client.Close(router.CloseInternalServerErr, "internal error")
    }
})
```

### Implementation Phases

#### Phase 1: Core Abstractions (Week 1-2)
- [ ] WSClient interface with automatic pump management
- [ ] Basic event emitter pattern
- [ ] Simple broadcast capabilities
- [ ] Automatic cleanup and resource management

#### Phase 2: Room Management (Week 3)
- [ ] Room join/leave functionality
- [ ] Room-based broadcasting
- [ ] Dynamic room creation
- [ ] Room presence tracking

#### Phase 3: Event System ✅ COMPLETED
- [x] Event routing with namespaces
- [x] Type-safe event handlers with generics
- [x] Event acknowledgments with timeout support
- [x] Event middleware chain (logging, rate limiting, auth, transform)
- [x] Event validation and schema support
- [x] Event history and replay functionality
- [x] Event batching and throttling
- [x] Comprehensive tests and examples

#### Phase 4: Advanced Features ✅ COMPLETED
- [x] Binary message support with codec
- [x] File upload/download with chunking and progress tracking
- [x] Compression support with configurable thresholds
- [x] Reconnection support with session management and message queueing
- [x] Message queueing with TTL and size limits
- [x] Heartbeat management for connection monitoring
- [x] Comprehensive tests and examples

### Example: Simplified Chat with New API

```go
// Before: ~300 lines
// After: ~40 lines with proper error handling

func main() {
    hub := router.NewWSHub()

    hub.OnConnect(func(ctx context.Context, client router.WSClient) error {
        username := client.Query("username", "Guest")
        client.SetWithContext(ctx, "username", username)

        if err := client.JoinWithContext(ctx, "chat"); err != nil {
            return fmt.Errorf("failed to join chat: %w", err)
        }

        return client.Room("chat").EmitWithContext(ctx, "user:joined", username)
    })

    hub.On("message", func(ctx context.Context, client router.WSClient, text string) error {
        // Validate message
        if text == "" {
            return router.ErrEmptyMessage
        }
        if len(text) > 1000 {
            return router.ErrMessageTooLong
        }

        return client.Room("chat").EmitWithContext(ctx, "message", map[string]any{
            "user": client.Get("username"),
            "text": text,
            "time": time.Now(),
        })
    })

    hub.OnDisconnect(func(ctx context.Context, client router.WSClient) error {
        // Context may have short deadline for cleanup
        cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
        defer cancel()

        return client.Room("chat").EmitWithContext(cleanupCtx, "user:left", client.Get("username"))
    })

    // Global error handler
    hub.OnError(func(ctx context.Context, client router.WSClient, err error) {
        log.Printf("Client %s error: %v", client.ID(), err)
    })

    app := router.NewHTTPServer()
    app.Router().Get("/ws", hub.Handler())
    if err := app.Serve(":8080"); err != nil {
        log.Fatal(err)
    }
}
```

## Performance Considerations

### Benchmarks to Track
1. **Connection overhead**: < 1ms per connection
2. **Message latency**: < 100μs for local broadcast
3. **Memory per connection**: < 10KB base overhead
4. **Throughput**: > 100K messages/second on single core

### Optimization Strategies
1. Pool allocations for common objects
2. Lock-free data structures where possible
3. Lazy initialization of features
4. Zero-allocation message routing for hot paths

## Migration Strategy

### Backward Compatibility
- Maintain existing low-level API
- Provide migration guide with examples
- Offer compatibility layer for gradual migration

### Migration Path
```go
// Step 1: Wrap existing handlers
ws := router.WrapLegacyHandler(existingHandler)

// Step 2: Gradually adopt new features
ws.Use(router.WSMetrics())  // Add monitoring

// Step 3: Refactor to new patterns
ws.On("message", newEventHandler)
```

## Testing Strategy

### Unit Tests
- Event routing accuracy
- Room management correctness
- Resource cleanup verification
- Error handling paths

### Integration Tests
- Multi-client scenarios
- Reconnection handling
- Load testing
- Protocol compliance

### Example Test
```go
func TestBroadcast(t *testing.T) {
    ctx := context.Background()
    hub := router.NewWSHub()

    // Test clients with context support
    client1 := hub.TestClient(ctx)
    client2 := hub.TestClient(ctx)

    // Test error handling
    err := client1.JoinWithContext(ctx, "room1")
    require.NoError(t, err)

    err = client2.JoinWithContext(ctx, "room1")
    require.NoError(t, err)

    // Test message broadcasting with context
    err = client1.EmitWithContext(ctx, "message", "hello")
    require.NoError(t, err)

    assert.Eventually(t, func() bool {
        msg, err := client2.ReceivedWithContext(ctx, "message")
        return err == nil && msg == "hello"
    }, time.Second, time.Millisecond)
}

func TestErrorHandling(t *testing.T) {
    ctx := context.Background()
    hub := router.NewWSHub()

    var capturedErr error
    hub.OnError(func(errCtx context.Context, client router.WSClient, err error) {
        capturedErr = err
    })

    hub.On("test", func(msgCtx context.Context, client router.WSClient, data any) error {
        return router.ErrMessageTooLarge
    })

    client := hub.TestClient(ctx)
    err := client.EmitWithContext(ctx, "test", "data")

    assert.ErrorIs(t, err, router.ErrMessageTooLarge)
    assert.ErrorIs(t, capturedErr, router.ErrMessageTooLarge)
}

func TestContextCancellation(t *testing.T) {
    hub := router.NewWSHub()

    // Create context with cancellation
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    hub.On("slow", func(msgCtx context.Context, client router.WSClient, data any) error {
        select {
        case <-time.After(5 * time.Second):
            return nil
        case <-msgCtx.Done():
            return msgCtx.Err()
        }
    })

    client := hub.TestClient(ctx)

    // Start slow operation
    errCh := make(chan error)
    go func() {
        errCh <- client.EmitWithContext(ctx, "slow", "data")
    }()

    // Cancel context
    cancel()

    // Should receive context cancelled error
    err := <-errCh
    assert.ErrorIs(t, err, context.Canceled)
}
```

## Security Considerations

### Built-in Protections
1. **Rate limiting**: Per-client and global limits
2. **Message size limits**: Configurable max message size
3. **Origin validation**: CORS-like origin checking
4. **Authentication hooks**: JWT, session, custom auth
5. **Input validation**: Schema validation for JSON messages

### Example Security Configuration
```go
hub := router.NewWSHub(
    router.WSMaxMessageSize(1024 * 1024),  // 1MB
    router.WSRateLimit(100, time.Minute),   // 100 msgs/min
    router.WSRequireAuth(jwtValidator),
    router.WSAllowOrigins("https://example.com"),
)
```

## Documentation Requirements

### API Documentation
- Comprehensive godoc comments
- Usage examples for each feature
- Performance characteristics
- Best practices guide

### Tutorials
1. Getting Started (Echo Server)
2. Building a Chat Application
3. Real-time Notifications
4. Collaborative Editing
5. Gaming/Real-time Updates

### Migration Guide
- Side-by-side comparisons
- Common patterns translation
- Performance optimization tips

## Key Design Improvements

### Context Integration
The design now fully embraces `context.Context` as a first-class citizen:
- **Every handler receives context** for cancellation, timeouts, and request-scoped values
- **Context flows through the entire request chain**, enabling proper distributed tracing
- **Graceful shutdown** through context cancellation propagation
- **Request-scoped metadata** (auth, request IDs, etc.) travels with the context

### Error Handling
The API now follows Go's idiomatic error handling patterns:
- **All handlers return errors** instead of forcing internal error handling
- **Framework manages error responses** appropriately (logging, closing connections)
- **Well-defined error types** for different scenarios (rate limits, validation, etc.)
- **Centralized error handling** with access to context for rich logging
- **Graceful degradation** for non-critical errors

### Benefits of These Changes
1. **Better Observability**: Context carries tracing spans, metrics labels, and loggers
2. **Cleaner Code**: Handlers focus on business logic, not error management
3. **Consistent Behavior**: Framework ensures consistent error handling across all connections
4. **Testability**: Errors are returned, making testing much easier
5. **Resource Management**: Context ensures proper cleanup even in error cases

## Conclusion

The recommended hybrid approach provides:
- **Immediate simplicity** for basic use cases
- **Power and flexibility** for complex applications
- **Familiar patterns** from popular libraries
- **Go-idiomatic** design with proper context and error handling
- **Performance** without sacrificing usability
- **Production-ready** with built-in observability and error management

This design reduces code complexity by 80-90% for common use cases while maintaining the flexibility to handle advanced scenarios. The integration of context and proper error handling ensures the API is not just easy to use, but also robust and production-ready. The phased implementation allows for iterative development and community feedback.

## Appendix: Comparison Matrix

| Feature | Current | Socket.IO Style | Middleware | Actor | Stream | Hybrid |
|---------|---------|----------------|------------|-------|---------|---------|
| Learning Curve | High | Low | Medium | High | Medium | Low |
| Boilerplate | High | Low | Medium | Medium | Low | Very Low |
| Flexibility | High | Medium | High | High | Medium | High |
| Performance | High | Medium | High | Medium | High | High |
| Testability | Low | High | High | High | Medium | High |
| Ecosystem | None | Rich | Medium | Low | Low | Growing |
| Type Safety | High | Medium | High | High | High | High |

## Next Steps

1. **Community Feedback**: Share design with community for input
2. **Prototype Development**: Build proof-of-concept for core API
3. **Performance Testing**: Benchmark against requirements
4. **API Refinement**: Iterate based on usage patterns
5. **Documentation**: Create comprehensive guides and examples
6. **Release Planning**: Phase rollout with feature flags
