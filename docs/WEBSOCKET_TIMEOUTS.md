# WebSocket Timeout Configuration

This document explains how WebSocket timeouts work in go-router and how they prevent common connection issues like hanging connections and resource leaks.

## Overview

WebSocket connections are long-lived and can become "zombie" connections when network issues occur. Without proper timeout configuration, these dead connections can:

- Block server resources (goroutines, memory, mutex locks)
- Cause API endpoints to hang indefinitely 
- Lead to resource exhaustion under load
- Create poor user experience

The `DefaultWebSocketConfig()` includes carefully chosen timeout values that prevent these issues.

## Timeout Parameters

### HandshakeTimeout (Default: 10s)
**Purpose**: Maximum time for WebSocket handshake completion
**Effect**: Prevents slow handshake attacks and hanging upgrade attempts
**Process**: 
1. Client requests WebSocket upgrade
2. Server starts handshake timer
3. If handshake not completed within 10s → connection rejected

```go
config.HandshakeTimeout = 10 * time.Second
```

### ReadTimeout (Default: 60s)
**Purpose**: Compatibility field retained for higher-level WebSocket helpers.
**Effect**: Adapter read deadlines are managed with `PongWait` so idle but healthy browser clients can remain connected while ping/pong keepalive is active.
**Process**:
1. Server calls `ws.ReadMessage()`
2. The adapter sets a read deadline from `PongWait` when read deadlines are enabled
3. Browser clients answer server pings with pongs, extending that deadline

```go
config.ReadTimeout = 60 * time.Second
```

Use `DisableReadDeadline: true` only when another component owns stale connection cleanup.

### WriteTimeout (Default: 10s)
**Purpose**: Maximum time for write operations to complete  
**Effect**: Ensures server doesn't hang when sending data to slow/dead clients
**Process**:
1. Server calls `ws.WriteMessage()` or `ws.WriteJSON()`  
2. If write doesn't complete within 10s → operation times out
3. Connection marked as failed

```go
config.WriteTimeout = 10 * time.Second
```

### PingPeriod (Default: 54s)
**Purpose**: How often server sends ping frames to check connection health
**Effect**: Proactive detection of broken connections
**Process**:
1. Server automatically sends ping frame every 54s
2. Client must respond with pong frame
3. If client doesn't respond within PongWait → connection closed

```go
config.PingPeriod = 54 * time.Second // Must be < PongWait
```

### PongWait (Default: 60s)
**Purpose**: Maximum time to wait for pong response after sending ping
**Effect**: Primary mechanism for detecting and cleaning up dead connections
**Process**:
1. Server sends ping frame
2. Waits up to 60s for pong response
3. If no pong received → connection considered dead and closed

```go
config.PongWait = 60 * time.Second // Must be > PingPeriod
```

### DisableKeepAlive
**Purpose**: Explicitly opt out of automatic server pings and pong-based deadline management.

```go
config.DisableKeepAlive = true
```

Zero `PingPeriod` or `PongWait` values mean "use defaults"; they do not disable keepalive.

### DisableReadDeadline
**Purpose**: Keep server pings enabled but disable adapter-managed read deadlines.

```go
config.DisableReadDeadline = true
```

## How Timeouts Work Together

The timeout parameters work as a coordinated system:

```
Connection Lifecycle with Timeouts:

1. Handshake Phase:
   ┌─────────────────┐
   │ HandshakeTimeout│ ← 10s max for upgrade
   └─────────────────┘

2. Active Connection:
   ┌──────────┐    ┌──────────┐    ┌──────────┐
   │ PongWait │    │PingPeriod│    │WriteTimeout│
   │   60s    │    │   54s    │    │   10s    │
   └──────────┘    └──────────┘    └──────────┘
        ↓              ↓              ↓
   Dead-peer       Sends health    Prevents slow
   deadline        check pings     write blocking

3. Health Check Cycle:
   Server ──ping──→ Client ──pong──→ Server
   └─── PongWait: 60s max wait ────┘
```

## Common Scenarios

### Scenario 1: Network Disconnection
```
1. Client network disconnects (WiFi off, mobile data lost)
2. Server ping receives no pong → PongWait read deadline triggers
3. Handler exits, calls chatServer.Leave(ws)
4. Mutex locks released, API endpoints become responsive
```

### Scenario 2: Client Application Crash  
```
1. Client app crashes without closing WebSocket
2. Server sends ping after 54s → no response
3. PongWait timeout (60s) triggers → connection closed
4. Resources cleaned up automatically
```

### Scenario 3: Slow Client Response
```
1. Client becomes very slow (low CPU, network congestion)
2. Server writes message → WriteTimeout (10s) triggers if too slow
3. Connection marked as failed and cleaned up
```

## Impact on Server Stability

**Without Proper Timeouts:**
- WebSocket handlers block indefinitely in `ws.ReadMessage()`
- Zombie connections accumulate over time
- Mutex locks held by dead handlers
- API endpoints (like `/api/stats`) hang waiting for locks
- Server becomes unresponsive

**With Default Keepalive:**
- Dead connections detected within 60s
- Automatic cleanup of resources
- Mutex locks released promptly
- API endpoints remain responsive
- Server scales properly under load

## Real-World Example: Chat Server

The simple_chat example demonstrates this:

```go
// These defaults prevent the hanging /api/stats issue:
config := router.DefaultWebSocketConfig()

// API endpoint can safely acquire read lock:
app.Router().Get("/api/stats", func(ctx router.Context) error {
    chatServer.mutex.RLock()         // Won't hang indefinitely
    defer chatServer.mutex.RUnlock() // Thanks to timeout cleanup
    
    return ctx.JSON(200, stats)
})
```

## Customization Guidelines

### Development/Testing
```go
config := router.DefaultWebSocketConfig()
config.PingPeriod = 10 * time.Second   // More frequent health checks
config.PongWait = 15 * time.Second     // Quick failure detection
```

### High-Load Production
```go
config := router.DefaultWebSocketConfig()  
config.WriteTimeout = 5 * time.Second  // Faster write failure detection
config.PingPeriod = 30 * time.Second   // More frequent health checks
```

### Low-Latency Applications
```go
config := router.DefaultWebSocketConfig()
config.WriteTimeout = 2 * time.Second  // Very fast write timeouts
config.PingPeriod = 15 * time.Second   // Frequent ping checks
config.PongWait = 20 * time.Second     // Quick dead connection detection
```

## Best Practices

1. **Always use DefaultWebSocketConfig()**: It includes battle-tested timeout values
2. **PingPeriod < PongWait**: Essential relationship for health checking
3. **Match timeout to use case**: Real-time games need shorter timeouts than file transfers  
4. **Monitor timeout logs**: Track connection failures in production
5. **Test timeout behavior**: Simulate network disconnections in testing

## Troubleshooting

### Symptom: API endpoints hanging
**Cause**: Dead WebSocket connections holding locks
**Solution**: Keep automatic ping/pong enabled, or provide another cleanup path before using `DisableReadDeadline`

### Symptom: Frequent connection drops
**Cause**: Timeouts too aggressive for network conditions
**Solution**: Increase `PongWait` and keep `PingPeriod < PongWait`

### Symptom: Slow connection cleanup
**Cause**: PingPeriod/PongWait too long for use case
**Solution**: Decrease ping/pong intervals for faster detection

### Symptom: High CPU usage
**Cause**: Too frequent ping/pong cycles
**Solution**: Increase PingPeriod (but keep < PongWait)

The default configuration balances connection stability with resource cleanup, preventing the hanging connection issues that can break server APIs.
