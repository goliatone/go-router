package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/goliatone/go-router"
)

// defaultLogger implements a simple logger
type defaultLogger struct{}

func (l *defaultLogger) Info(msg string, args ...any) {
	log.Printf("[INFO] %s %v", msg, args)
}

func (l *defaultLogger) Error(msg string, args ...any) {
	log.Printf("[ERROR] %s %v", msg, args)
}

func (l *defaultLogger) Debug(msg string, args ...any) {
	log.Printf("[DEBUG] %s %v", msg, args)
}

func (l *defaultLogger) Warn(msg string, args ...any) {
	log.Printf("[WARN] %s %v", msg, args)
}

// Simple JWT claims for demo purposes
type DemoJWTClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func (c *DemoJWTClaims) UserIDString() string { return c.UserID }
func (c *DemoJWTClaims) RoleString() string   { return c.Role }

// Demo authenticator that validates JWT tokens
type DemoAuthenticator struct {
	signingKey []byte
}

func (a *DemoAuthenticator) ValidateToken(tokenString string) (*DemoJWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &DemoJWTClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.signingKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*DemoJWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func (a *DemoAuthenticator) CreateToken(userID, username, role string) (string, error) {
	claims := &DemoJWTClaims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.signingKey)
}

// WebSocket client state management
type AuthenticatedClient struct {
	ID       string
	UserID   string
	Username string
	Role     string
	WS       router.WebSocketContext
	JoinedAt time.Time
}

type WebSocketServer struct {
	authenticator *DemoAuthenticator
	clients       map[string]*AuthenticatedClient
	clientsMutex  sync.RWMutex
	logger        router.Logger
}

func NewWebSocketServer() *WebSocketServer {
	return &WebSocketServer{
		authenticator: &DemoAuthenticator{
			signingKey: []byte("demo-secret-key-change-in-production"),
		},
		clients: make(map[string]*AuthenticatedClient),
		logger:  &defaultLogger{},
	}
}

func (s *WebSocketServer) addClient(client *AuthenticatedClient) {
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()
	s.clients[client.ID] = client

	s.logger.Info("Client authenticated and added",
		"client_id", client.ID,
		"user_id", client.UserID,
		"username", client.Username,
		"role", client.Role,
		"total_clients", len(s.clients))
}

func (s *WebSocketServer) removeClient(clientID string) {
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()

	if client, exists := s.clients[clientID]; exists {
		delete(s.clients, clientID)
		s.logger.Info("Client disconnected and removed",
			"client_id", clientID,
			"user_id", client.UserID,
			"username", client.Username,
			"total_clients", len(s.clients))
	}
}

func (s *WebSocketServer) getClient(clientID string) (*AuthenticatedClient, bool) {
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()
	client, exists := s.clients[clientID]
	return client, exists
}

func (s *WebSocketServer) getAllClients() []*AuthenticatedClient {
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()

	clients := make([]*AuthenticatedClient, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	return clients
}

func (s *WebSocketServer) broadcastToAll(message any) {
	clients := s.getAllClients()
	for _, client := range clients {
		if err := client.WS.WriteJSON(message); err != nil {
			s.logger.Error("Failed to send broadcast message",
				"client_id", client.ID,
				"error", err)
		}
	}
}

func (s *WebSocketServer) broadcastToRole(role string, message any) {
	clients := s.getAllClients()
	for _, client := range clients {
		if client.Role == role {
			if err := client.WS.WriteJSON(message); err != nil {
				s.logger.Error("Failed to send role broadcast message",
					"client_id", client.ID,
					"role", role,
					"error", err)
			}
		}
	}
}

func (s *WebSocketServer) CreateWebSocketHandler() func(router.WebSocketContext) error {
	return func(ws router.WebSocketContext) error {
		clientID := ws.ConnectionID()
		s.logger.Info("New WebSocket connection", "client_id", clientID)

		// Handle incoming messages
		for {
			var msg map[string]any
			if err := ws.ReadJSON(&msg); err != nil {
				s.logger.Info("Client disconnected", "client_id", clientID, "error", err)
				break
			}

			// Check if client is authenticated
			client, exists := s.getClient(clientID)
			if !exists {
				// Client not authenticated, handle authentication messages
				if err := s.handleUnauthenticatedMessage(ws, msg); err != nil {
					s.logger.Error("Error handling unauthenticated message", "client_id", clientID, "error", err)
				}
				// Check if they just authenticated
				if client, exists := s.getClient(clientID); exists {
					// Client just authenticated, send welcome and broadcast join
					ws.WriteJSON(map[string]any{
						"type":     "welcome",
						"message":  fmt.Sprintf("Welcome %s! You are connected as %s", client.Username, client.Role),
						"user_id":  client.UserID,
						"username": client.Username,
						"role":     client.Role,
					})

					// Broadcast user joined to all other clients
					s.broadcastToAll(map[string]any{
						"type":      "user_joined",
						"user_id":   client.UserID,
						"username":  client.Username,
						"role":      client.Role,
						"timestamp": time.Now().Format(time.RFC3339),
					})
				}
			} else {
				// Client is authenticated, handle normal messages
				if err := s.handleMessage(client, msg); err != nil {
					s.logger.Error("Error handling message", "client_id", clientID, "error", err)
				}
			}
		}

		// Clean up on disconnect
		if client, exists := s.getClient(clientID); exists {
			s.removeClient(clientID)

			// Broadcast user left
			s.broadcastToAll(map[string]any{
				"type":      "user_left",
				"user_id":   client.UserID,
				"username":  client.Username,
				"timestamp": time.Now().Format(time.RFC3339),
			})
		}

		return nil
	}
}

func (s *WebSocketServer) handleMessage(client *AuthenticatedClient, msg map[string]any) error {
	msgType, ok := msg["type"].(string)
	if !ok {
		return client.WS.WriteJSON(map[string]any{
			"type":    "error",
			"message": "Invalid message format",
		})
	}

	switch msgType {
	case "chat_message":
		return s.handleChatMessage(client, msg)
	case "admin_command":
		return s.handleAdminCommand(client, msg)
	case "get_users":
		return s.handleGetUsers(client)
	case "ping":
		return s.handlePing(client)
	default:
		return client.WS.WriteJSON(map[string]any{
			"type":    "error",
			"message": fmt.Sprintf("Unknown message type: %s", msgType),
		})
	}
}

// handleUnauthenticatedMessage handles messages from clients that haven't authenticated yet
func (s *WebSocketServer) handleUnauthenticatedMessage(ws router.WebSocketContext, msg map[string]any) error {
	msgType, ok := msg["type"].(string)
	if !ok {
		return ws.WriteJSON(map[string]any{
			"type":    "error",
			"message": "Invalid message format",
		})
	}

	if msgType != "auth" {
		return ws.WriteJSON(map[string]any{
			"type":    "auth_error",
			"message": "Authentication required. Send auth message first.",
		})
	}

	// Handle authentication message
	token, ok := msg["token"].(string)
	if !ok || token == "" {
		return ws.WriteJSON(map[string]any{
			"type":    "auth_error",
			"message": "Token is required",
		})
	}

	s.logger.Info("Processing authentication message",
		"client_id", ws.ConnectionID(),
		"token_length", len(token))

	// Validate token
	claims, err := s.authenticator.ValidateToken(token)
	if err != nil {
		s.logger.Error("Token validation failed",
			"client_id", ws.ConnectionID(),
			"error", err)
		return ws.WriteJSON(map[string]any{
			"type":    "auth_error",
			"message": "Invalid token",
		})
	}

	// Create authenticated client
	client := &AuthenticatedClient{
		ID:       ws.ConnectionID(),
		UserID:   claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		WS:       ws,
		JoinedAt: time.Now(),
	}

	// Add to authenticated clients
	s.addClient(client)

	s.logger.Info("Client authenticated successfully",
		"client_id", ws.ConnectionID(),
		"user_id", claims.UserID,
		"username", claims.Username,
		"role", claims.Role)

	// Send success response
	return ws.WriteJSON(map[string]any{
		"type":     "auth_success",
		"message":  fmt.Sprintf("Welcome %s! You are authenticated as %s", claims.Username, claims.Role),
		"user_id":  claims.UserID,
		"username": claims.Username,
		"role":     claims.Role,
	})
}

func (s *WebSocketServer) handleChatMessage(client *AuthenticatedClient, msg map[string]any) error {
	text, ok := msg["text"].(string)
	if !ok {
		return client.WS.WriteJSON(map[string]any{
			"type":    "error",
			"message": "Message text required",
		})
	}

	// Broadcast chat message to all clients
	response := map[string]any{
		"type":      "chat_message",
		"user_id":   client.UserID,
		"username":  client.Username,
		"role":      client.Role,
		"text":      text,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	s.broadcastToAll(response)
	s.logger.Info("Chat message broadcasted",
		"from_user", client.Username,
		"message_length", len(text))

	return nil
}

func (s *WebSocketServer) handleAdminCommand(client *AuthenticatedClient, msg map[string]any) error {
	if client.Role != "admin" {
		return client.WS.WriteJSON(map[string]any{
			"type":    "error",
			"message": "Admin privileges required",
		})
	}

	command, ok := msg["command"].(string)
	if !ok {
		return client.WS.WriteJSON(map[string]any{
			"type":    "error",
			"message": "Command required",
		})
	}

	// Execute admin command (demo purposes)
	s.logger.Info("Admin command executed",
		"admin", client.Username,
		"command", command)

	// Broadcast admin announcement
	s.broadcastToAll(map[string]any{
		"type":      "admin_announcement",
		"message":   fmt.Sprintf("Admin %s executed: %s", client.Username, command),
		"admin":     client.Username,
		"timestamp": time.Now().Format(time.RFC3339),
	})

	return nil
}

func (s *WebSocketServer) handleGetUsers(client *AuthenticatedClient) error {
	clients := s.getAllClients()
	users := make([]map[string]any, len(clients))

	for i, c := range clients {
		users[i] = map[string]any{
			"user_id":   c.UserID,
			"username":  c.Username,
			"role":      c.Role,
			"joined_at": c.JoinedAt.Format(time.RFC3339),
		}
	}

	return client.WS.WriteJSON(map[string]any{
		"type":  "users_list",
		"users": users,
		"count": len(users),
	})
}

func (s *WebSocketServer) handlePing(client *AuthenticatedClient) error {
	return client.WS.WriteJSON(map[string]any{
		"type":      "pong",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *WebSocketServer) CreateOnConnectHandler() func(router.WebSocketContext) error {
	return func(ws router.WebSocketContext) error {
		clientID := ws.ConnectionID()
		s.logger.Info("WebSocket OnConnect called", "client_id", clientID)

		// For Fiber WebSocket, query parameters need to be extracted differently
		// The token should be passed via the WebSocket URL when connecting
		// We'll implement a message-based authentication instead since query access is limited

		// Send authentication request
		ws.WriteJSON(map[string]any{
			"type":    "auth_required",
			"message": "Please send authentication message with your token",
		})

		s.logger.Info("Authentication required message sent", "client_id", clientID)
		return nil
	}
}

func main() {
	fmt.Println("🚀 Starting WebSocket Authentication Demo")

	// Determine adapter type
	adapterType := "fiber" // default
	if len(os.Args) > 1 {
		adapterType = os.Args[1]
	}

	wsServer := NewWebSocketServer()

	// Create demo tokens for testing
	adminToken, _ := wsServer.authenticator.CreateToken("admin-001", "admin", "admin")
	userToken, _ := wsServer.authenticator.CreateToken("user-001", "john_doe", "user")
	moderatorToken, _ := wsServer.authenticator.CreateToken("mod-001", "jane_smith", "moderator")

	fmt.Println("\n🔑 Demo tokens for testing:")
	fmt.Printf("Admin token:     %s\n", adminToken)
	fmt.Printf("User token:      %s\n", userToken)
	fmt.Printf("Moderator token: %s\n", moderatorToken)

	switch adapterType {
	case "fiber":
		app := router.NewFiberAdapter()
		setupWebSocketRoutes(app, wsServer)
		setupHTTPRoutes(app, wsServer, adminToken, userToken, moderatorToken)

		fmt.Println("\n🌐 Fiber server starting on http://localhost:3000")
		fmt.Println("WebSocket endpoint: ws://localhost:3000/ws?token=<your-token>")
		log.Fatal(app.Serve(":3000"))

	case "httprouter", "http":
		app := router.NewHTTPServer()
		setupWebSocketRoutes(app, wsServer)
		setupHTTPRoutes(app, wsServer, adminToken, userToken, moderatorToken)

		fmt.Println("\n🌐 HTTPRouter server starting on http://localhost:8080")
		fmt.Println("WebSocket endpoint: ws://localhost:8080/ws?token=<your-token>")
		log.Fatal(app.Serve(":8080"))

	default:
		fmt.Printf("Unknown adapter: %s\n", adapterType)
		fmt.Println("Usage: go run main.go [fiber|httprouter]")
		os.Exit(1)
	}
}

func setupWebSocketRoutes[T any](app router.Server[T], wsServer *WebSocketServer) {
	// Register appropriate WebSocket factory
	switch app := any(app).(type) {
	case *router.FiberAdapter:
		router.RegisterFiberWebSocketFactory(wsServer.logger)
	case *router.HTTPServer:
		router.RegisterHTTPRouterWebSocketFactory(nil)
	default:
		log.Printf("Unknown adapter type: %T", app)
	}

	// No longer need middleware workaround! OnPreUpgrade hook handles everything

	// WebSocket configuration using OnPreUpgrade hook
	config := router.WebSocketConfig{
		Origins: []string{"*"},

		// OnPreUpgrade: Clean extraction and validation BEFORE WebSocket upgrade
		OnPreUpgrade: func(c router.Context) (router.UpgradeData, error) {
			wsServer.logger.Info("OnPreUpgrade: extracting and validating token with guaranteed HTTP context")

			// Extract token with guaranteed HTTP context access
			token := c.Query("token")
			if token == "" {
				wsServer.logger.Error("No token parameter provided")
				return nil, fmt.Errorf("token parameter required")
			}

			// Pre-validate token before upgrade
			claims, err := wsServer.authenticator.ValidateToken(token)
			if err != nil {
				wsServer.logger.Error("Invalid token in query parameter", "error", err)
				return nil, fmt.Errorf("invalid token: %w", err)
			}

			wsServer.logger.Info("Token validated successfully in OnPreUpgrade",
				"user", claims.Username, "role", claims.Role)

			// Return structured data for WebSocket context
			return router.UpgradeData{
				"token":     token,
				"user_id":   claims.UserID,
				"username":  claims.Username,
				"role":      claims.Role,
				"auth_time": time.Now(),
			}, nil
		},

		// OnConnect: Clean access to pre-validated data
		OnConnect: func(ws router.WebSocketContext) error {
			wsServer.logger.Info("OnConnect: accessing pre-validated upgrade data")

			// Get pre-validated auth data using the new UpgradeData method
			userID := router.GetUpgradeDataWithDefault(ws, "user_id", "").(string)
			username := router.GetUpgradeDataWithDefault(ws, "username", "").(string)
			role := router.GetUpgradeDataWithDefault(ws, "role", "").(string)

			if userID == "" || username == "" || role == "" {
				wsServer.logger.Error("Upgrade data not available")
				return fmt.Errorf("authentication data missing")
			}

			// Create authenticated client directly
			client := &AuthenticatedClient{
				ID:       ws.ConnectionID(),
				UserID:   userID,
				Username: username,
				Role:     role,
				WS:       ws,
				JoinedAt: time.Now(),
			}

			wsServer.addClient(client)

			wsServer.logger.Info("Client authenticated via OnPreUpgrade hook",
				"client_id", ws.ConnectionID(),
				"user", username,
				"role", role)

			return ws.WriteJSON(map[string]any{
				"type":     "auth_success",
				"message":  fmt.Sprintf("Authenticated via OnPreUpgrade as %s (%s)", username, role),
				"user_id":  userID,
				"username": username,
				"role":     role,
			})
		},
		OnDisconnect: func(ws router.WebSocketContext, err error) {
			wsServer.logger.Info("WebSocket disconnected",
				"client_id", ws.ConnectionID(),
				"error", err)
		},
	}

	// Register WebSocket endpoint
	app.Router().WebSocket("/ws", config, wsServer.CreateWebSocketHandler())

	wsServer.logger.Info("WebSocket routes configured with OnPreUpgrade hook authentication")
}

func setupHTTPRoutes[T any](app router.Server[T], wsServer *WebSocketServer, adminToken, userToken, moderatorToken string) {
	// Home page with demo interface
	app.Router().Get("/", func(c router.Context) error {
		html := createDemoHTML(adminToken, userToken, moderatorToken)
		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		return c.Status(200).Send([]byte(html))
	})

	// API endpoint to get current connected users
	app.Router().Get("/api/users", func(c router.Context) error {
		clients := wsServer.getAllClients()
		users := make([]map[string]any, len(clients))

		for i, client := range clients {
			users[i] = map[string]any{
				"user_id":   client.UserID,
				"username":  client.Username,
				"role":      client.Role,
				"joined_at": client.JoinedAt.Format(time.RFC3339),
			}
		}

		return c.JSON(200, map[string]any{
			"users": users,
			"count": len(users),
		})
	})

	wsServer.logger.Info("HTTP routes configured")
}

func createDemoHTML(adminToken, userToken, moderatorToken string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Authentication Demo</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .container { max-width: 1200px; margin: 0 auto; }
        .section { margin: 20px 0; padding: 20px; border: 1px solid #ddd; border-radius: 5px; }
        .status { padding: 10px; margin: 10px 0; border-radius: 5px; font-weight: bold; }
        .connected { background-color: #d4edda; color: #155724; }
        .disconnected { background-color: #f8d7da; color: #721c24; }
        .messages { height: 300px; border: 1px solid #ccc; padding: 10px; overflow-y: scroll; margin: 10px 0; background-color: #f9f9f9; }
        .message { margin: 5px 0; padding: 5px; }
        .message.chat { background-color: #e3f2fd; }
        .message.system { background-color: #f3e5f5; }
        .message.error { background-color: #ffebee; color: #c62828; }
        input[type=text] { width: 70%%; padding: 10px; }
        button { padding: 10px 20px; margin: 5px; cursor: pointer; }
        select { padding: 10px; margin: 5px; }
        .token-section { background-color: #f5f5f5; padding: 15px; margin: 10px 0; }
        .token { font-family: monospace; font-size: 12px; word-break: break-all; background-color: white; padding: 5px; margin: 5px 0; }
        .users-list { background-color: #f0f8ff; padding: 10px; margin: 10px 0; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🔐 WebSocket Authentication Demo</h1>
        <p>This demo shows proper WebSocket authentication with JWT tokens and role-based features.</p>

        <!-- Connection Status -->
        <div class="section">
            <h3>Connection Status</h3>
            <div id="status" class="status disconnected">Disconnected</div>

            <div class="token-section">
                <label for="tokenSelect">Select Demo User:</label>
                <select id="tokenSelect">
                    <option value="%s">Admin User</option>
                    <option value="%s">Regular User</option>
                    <option value="%s">Moderator User</option>
                </select>
                <button onclick="connect()">Connect</button>
                <button onclick="disconnect()">Disconnect</button>
            </div>

            <div class="token-section">
                <h4>Demo Tokens:</h4>
                <div><strong>Admin:</strong> <div class="token">%s</div></div>
                <div><strong>User:</strong> <div class="token">%s</div></div>
                <div><strong>Moderator:</strong> <div class="token">%s</div></div>
            </div>
        </div>

        <!-- Messages -->
        <div class="section">
            <h3>Messages & Events</h3>
            <div id="messages" class="messages"></div>
            <button onclick="clearMessages()">Clear Messages</button>
            <button onclick="getUsers()">Get Connected Users</button>
            <button onclick="ping()">Send Ping</button>
        </div>

        <!-- Chat -->
        <div class="section">
            <h3>Chat (All Users)</h3>
            <div>
                <input type="text" id="chatInput" placeholder="Type your message..." />
                <button onclick="sendChat()">Send Message</button>
            </div>
        </div>

        <!-- Admin Commands -->
        <div class="section">
            <h3>Admin Commands (Admin Only)</h3>
            <div>
                <input type="text" id="adminInput" placeholder="Admin command..." />
                <button onclick="sendAdminCommand()">Execute Command</button>
            </div>
            <p><em>Note: Only admin users can execute commands</em></p>
        </div>

        <!-- Connected Users -->
        <div class="section">
            <h3>Connected Users</h3>
            <div id="usersList" class="users-list">
                <p>Connect to see active users</p>
            </div>
            <button onclick="refreshUsers()">Refresh Users</button>
        </div>
    </div>

    <script>
        let ws = null;
        let pingInterval = null;
        let currentToken = '';
        let currentUser = null;

        function addMessage(text, type = 'system') {
            const messages = document.getElementById('messages');
            const div = document.createElement('div');
            div.className = 'message ' + type;
            div.innerHTML = '<strong>' + new Date().toLocaleTimeString() + '</strong> ' + text;
            messages.appendChild(div);
            messages.scrollTop = messages.scrollHeight;
        }

        function updateStatus(status, connected = false) {
            const statusEl = document.getElementById('status');
            statusEl.textContent = status;
            statusEl.className = 'status ' + (connected ? 'connected' : 'disconnected');
        }

        function connect() {
            if (ws) {
                addMessage('Already connected', 'error');
                return;
            }

            const tokenSelect = document.getElementById('tokenSelect');
            currentToken = tokenSelect.value;

            if (!currentToken) {
                addMessage('Please select a demo user', 'error');
                return;
            }

            const wsUrl = 'ws://' + window.location.host + '/ws?token=' + currentToken;
            addMessage('Connecting with token to: ' + wsUrl.replace(currentToken, '<token>'), 'system');

            ws = new WebSocket(wsUrl);

            ws.onopen = function() {
                updateStatus('Connected', true);
                addMessage('✅ WebSocket connected successfully', 'system');
                addMessage('🔐 Authentication handled via OnPreUpgrade hook', 'system');

                // Start sending pings every 30 seconds to keep connection alive
                pingInterval = setInterval(() => {
                    if (ws && ws.readyState === WebSocket.OPEN) {
                        sendMessage({ type: 'ping' });
                    }
                }, 30000);
                addMessage('🔄 Started automatic keepalive pings (every 30s)', 'system');
            };

            ws.onclose = function(event) {
                updateStatus('Disconnected', false);
                addMessage('❌ WebSocket disconnected (code: ' + event.code + ')', 'system');

                // Clear ping interval
                if (pingInterval) {
                    clearInterval(pingInterval);
                    pingInterval = null;
                    addMessage('⏹ Stopped automatic keepalive pings', 'system');
                }

                ws = null;
                currentUser = null;
            };

            ws.onerror = function(error) {
                addMessage('❌ WebSocket error: ' + error, 'error');
            };

            ws.onmessage = function(event) {
                try {
                    const data = JSON.parse(event.data);
                    handleMessage(data);
                } catch (e) {
                    addMessage('❌ Invalid JSON received: ' + event.data, 'error');
                }
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
            }
        }

        function handleMessage(data) {
            switch (data.type) {
                case 'auth_success':
                    currentUser = {
                        user_id: data.user_id,
                        username: data.username,
                        role: data.role
                    };
                    addMessage('✅ OnPreUpgrade authentication successful: ' + data.message, 'system');
                    updateUserInfo();
                    break;

                case 'welcome':
                    addMessage('🎉 ' + data.message, 'system');
                    break;

                case 'auth_error':
                    addMessage('🔒 Authentication failed: ' + data.message, 'error');
                    break;

                case 'chat_message':
                    const roleEmoji = data.role === 'admin' ? '👑' : data.role === 'moderator' ? '🛡️' : '👤';
                    addMessage(roleEmoji + ' <strong>' + data.username + '</strong>: ' + data.text, 'chat');
                    break;

                case 'user_joined':
                    const joinEmoji = data.role === 'admin' ? '👑' : data.role === 'moderator' ? '🛡️' : '👤';
                    addMessage('➕ ' + joinEmoji + ' ' + data.username + ' joined the chat', 'system');
                    break;

                case 'user_left':
                    addMessage('➖ ' + data.username + ' left the chat', 'system');
                    break;

                case 'admin_announcement':
                    addMessage('📢 <strong>ADMIN:</strong> ' + data.message, 'system');
                    break;

                case 'users_list':
                    updateUsersList(data.users);
                    addMessage('📋 Retrieved ' + data.count + ' connected users', 'system');
                    break;

                case 'pong':
                    addMessage('🏓 Pong received (keepalive)', 'system');
                    break;

                case 'error':
                    addMessage('❌ Error: ' + data.message, 'error');
                    break;

                default:
                    addMessage('📨 ' + JSON.stringify(data), 'system');
            }
        }

        function updateUserInfo() {
            if (currentUser) {
                const roleEmoji = currentUser.role === 'admin' ? '👑' : currentUser.role === 'moderator' ? '🛡️' : '👤';
                updateStatus('Connected as ' + roleEmoji + ' ' + currentUser.username + ' (' + currentUser.role + ')', true);
            }
        }

        function updateUsersList(users) {
            const usersEl = document.getElementById('usersList');
            if (users.length === 0) {
                usersEl.innerHTML = '<p>No users connected</p>';
                return;
            }

            let html = '<h4>Active Users (' + users.length + '):</h4>';
            users.forEach(user => {
                const roleEmoji = user.role === 'admin' ? '👑' : user.role === 'moderator' ? '🛡️' : '👤';
                html += '<div>' + roleEmoji + ' <strong>' + user.username + '</strong> (' + user.role + ') - joined: ' + new Date(user.joined_at).toLocaleTimeString() + '</div>';
            });
            usersEl.innerHTML = html;
        }

        function sendMessage(data) {
            if (!ws) {
                addMessage('❌ Not connected', 'error');
                return;
            }
            ws.send(JSON.stringify(data));
        }

        function sendChat() {
            const input = document.getElementById('chatInput');
            if (input.value.trim()) {
                sendMessage({
                    type: 'chat_message',
                    text: input.value
                });
                input.value = '';
            }
        }

        function sendAdminCommand() {
            const input = document.getElementById('adminInput');
            if (input.value.trim()) {
                sendMessage({
                    type: 'admin_command',
                    command: input.value
                });
                input.value = '';
            }
        }

        function getUsers() {
            sendMessage({ type: 'get_users' });
        }

        function ping() {
            sendMessage({ type: 'ping' });
        }

        function clearMessages() {
            document.getElementById('messages').innerHTML = '';
        }

        function refreshUsers() {
            fetch('/api/users')
                .then(response => response.json())
                .then(data => {
                    updateUsersList(data.users);
                    addMessage('🔄 Users list refreshed via HTTP API', 'system');
                })
                .catch(error => {
                    addMessage('❌ Failed to fetch users: ' + error, 'error');
                });
        }

        // Enter key support
        document.getElementById('chatInput').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') sendChat();
        });
        document.getElementById('adminInput').addEventListener('keypress', (e) => {
            if (e.key === 'Enter') sendAdminCommand();
        });

        // Initialize
        addMessage('🚀 WebSocket Authentication Demo loaded', 'system');
        addMessage('🔑 Select a demo user and click Connect to start', 'system');
    </script>
</body>
</html>`, adminToken, userToken, moderatorToken, adminToken, userToken, moderatorToken)
}
