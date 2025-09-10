# WebSocket Authentication Example

This example demonstrates **proper WebSocket authentication patterns** using JWT tokens and role-based authorization with the go-router unified WebSocket interface.

## Features

### 🔐 **Authentication Flow**
- **JWT Token Validation**: Tokens sent via WebSocket message after connection
- **Message-Based Authentication**: Demonstrates secure authentication flow via WebSocket messages
- **Role-Based Authorization**: Admin, moderator, and user roles with different permissions
- **Secure Token Handling**: Proper validation and error handling

### 🌐 **Unified WebSocket Interface**
- **Adapter Agnostic**: Same code works with both Fiber and HTTPRouter adapters
- **Proper Factory Registration**: Correct WebSocket factory setup for each adapter
- **OnConnect Authentication**: Authentication handled in the WebSocket config OnConnect handler

### 💬 **Real-time Features**
- **Chat System**: Role-based chat with user identification
- **User Presence**: Live tracking of connected users with join/leave notifications
- **Admin Commands**: Admin-only command execution with broadcast announcements
- **Connection Management**: Proper client state management and cleanup

### 🎯 **Demo Interface**
- **Interactive Web UI**: Complete HTML interface for testing all features
- **Pre-generated Tokens**: Demo tokens for admin, user, and moderator roles
- **Real-time Updates**: Live connection status and user list updates
- **Error Handling**: Clear error messages and connection state feedback

## Quick Start

### Run with Fiber Adapter (Default)
```bash
go run main.go
# or
go run main.go fiber
```
Visit: http://localhost:3000

### Run with HTTPRouter Adapter
```bash
go run main.go httprouter
```
Visit: http://localhost:8080

## Architecture

### Authentication Pattern
```go
// 1. WebSocket config with OnConnect handler for initial setup
config := router.WebSocketConfig{
    OnConnect: func(ws router.WebSocketContext) error {
        // Send authentication request
        return ws.WriteJSON(map[string]any{
            "type":    "auth_required",
            "message": "Please send authentication message with your token",
        })
    },
}

// 2. WebSocket handler with message-based authentication
handler := func(ws router.WebSocketContext) error {
    for {
        var msg map[string]any
        if err := ws.ReadJSON(&msg); err != nil {
            break
        }
        
        // Check if client is authenticated
        client, exists := getAuthenticatedClient(ws.ConnectionID())
        if !exists {
            // Handle authentication message
            if msg["type"] == "auth" {
                token := msg["token"].(string)
                claims, err := authenticator.ValidateToken(token)
                if err != nil {
                    ws.WriteJSON(map[string]any{"type": "auth_error", "message": "Invalid token"})
                    continue
                }
                // Store authenticated client and send success
                addAuthenticatedClient(ws.ConnectionID(), claims)
                ws.WriteJSON(map[string]any{"type": "auth_success", "message": "Authenticated"})
            }
        } else {
            // Handle authenticated messages
            handleAuthenticatedMessage(client, msg)
        }
    }
    return nil
}

// 3. Register WebSocket endpoint
app.Router().WebSocket("/ws", config, handler)
```

### Key Differences from Problematic Patterns

| ❌ **Problematic Pattern** | ✅ **Correct Pattern** |
|---------------------------|------------------------|
| WSHub OnConnect for auth | WebSocket config OnConnect for auth |
| Factory mismatch | Proper factory registration per adapter |
| Complex dual auth paths | Single clean authentication flow |
| Error swallowing | Proper error handling and logging |
| Missing client state | Proper client lifecycle management |

## Testing

### 1. **Connection Test**
- Select a demo user (Admin/User/Moderator)
- Click "Connect"
- Should see "✅ WebSocket connected successfully"
- Status should show your role and username

### 2. **Authentication Test**
- Try connecting without selecting a user → Should fail
- Try with invalid token → Should show authentication error
- Valid token → Should authenticate and show welcome message

### 3. **Chat Test**
- Open multiple browser tabs
- Connect with different user types
- Send chat messages → Should broadcast to all connected users
- User join/leave notifications should appear

### 4. **Role-Based Features**
- **Admin Commands**: Only admin users can execute commands
- **User List**: All users can request connected users list
- **Ping/Pong**: Basic connectivity test for all users

### 5. **Connection Management**
- Connect/disconnect multiple times
- Check that users are properly added/removed from active list
- Refresh users list via HTTP API to verify cleanup

## Demo Tokens

The example generates JWT tokens for testing:

```
Admin User:     admin-001 / admin / admin
Regular User:   user-001 / john_doe / user  
Moderator User: mod-001 / jane_smith / moderator
```

Tokens are displayed in the web interface and logged to console on startup.

## WebSocket Endpoints

- **WebSocket**: `ws://localhost:3000/ws?token=<jwt-token>` (Fiber)
- **WebSocket**: `ws://localhost:8080/ws?token=<jwt-token>` (HTTPRouter)
- **HTTP API**: `GET /api/users` (Connected users list)

## Message Types

### Client → Server
```json
{"type": "chat_message", "text": "Hello everyone!"}
{"type": "admin_command", "command": "maintenance mode on"}
{"type": "get_users"}
{"type": "ping"}
```

### Server → Client
```json
{"type": "welcome", "message": "Welcome!", "user_id": "...", "username": "...", "role": "..."}
{"type": "auth_error", "message": "Invalid token"}
{"type": "chat_message", "username": "john_doe", "text": "Hello!", "role": "user", "timestamp": "..."}
{"type": "user_joined", "username": "jane_smith", "role": "moderator", "timestamp": "..."}
{"type": "admin_announcement", "message": "Admin executed: ...", "admin": "admin", "timestamp": "..."}
{"type": "users_list", "users": [...], "count": 3}
{"type": "pong", "timestamp": "..."}
{"type": "error", "message": "Error description"}
```

## Key Learnings

### ✅ **What Works**
1. **Query parameter access in WebSocket config OnConnect** - Works perfectly
2. **WSHub + Fiber adapter** - Fully compatible, no issues
3. **Unified WebSocket interface** - Same code works across adapters
4. **JWT authentication in OnConnect** - Proper and secure pattern

### ❌ **What Doesn't Work**
1. **Query parameter access in WSHub OnConnect** - Limited access after upgrade
2. **Dual authentication paths** - Creates complexity and race conditions
3. **Improper factory registration** - Causes handshake failures
4. **Error swallowing** - Masks actual authentication issues

## Production Considerations

1. **Security**: Change JWT signing key and implement proper key management
2. **Rate Limiting**: Add connection and message rate limiting
3. **Monitoring**: Add metrics for connections, authentication failures, etc.
4. **Scalability**: Consider using Redis for shared state across multiple server instances
5. **Error Handling**: Implement comprehensive error logging and alerting

## Conclusion

This example demonstrates the **correct way** to implement WebSocket authentication with go-router. The key insight is that **query parameter authentication should be handled in the WebSocket config OnConnect handler**, not in WSHub OnConnect handlers. This pattern works reliably with both Fiber and HTTPRouter adapters and provides a clean, secure authentication flow.