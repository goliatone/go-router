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
					// Client just authenticated, send welcome
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

func main() {
	fmt.Println("🚀 Starting WebSocket Client Demo Server")

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
		setupClientRoutes(app, wsServer, adminToken, userToken, moderatorToken)

		fmt.Println("\n🌐 Fiber server starting on http://localhost:3000")
		fmt.Println("WebSocket endpoint: ws://localhost:3000/ws")
		fmt.Println("WebSocket client: http://localhost:3000/client/")
		fmt.Println("Test page: http://localhost:3000/client/test")
		log.Fatal(app.Serve(":3000"))

	case "httprouter", "http":
		app := router.NewHTTPServer()
		setupWebSocketRoutes(app, wsServer)
		setupClientRoutes(app, wsServer, adminToken, userToken, moderatorToken)

		fmt.Println("\n🌐 HTTPRouter server starting on http://localhost:8080")
		fmt.Println("WebSocket endpoint: ws://localhost:8080/ws")
		fmt.Println("WebSocket client: http://localhost:8080/client/")
		fmt.Println("Test page: http://localhost:8080/client/test")
		log.Fatal(app.Serve(":8080"))

	default:
		fmt.Printf("Unknown adapter: %s\n", adapterType)
		fmt.Println("Usage: go run main.go [fiber|httprouter]")
		os.Exit(1)
	}
}

func setupWebSocketRoutes[T any](app router.Server[T], wsServer *WebSocketServer) {
	// WebSocket configuration using OnPreUpgrade hook
	config := router.WebSocketConfig{
		Origins: []string{"*"},

		// OnPreUpgrade: Clean extraction and validation BEFORE WebSocket upgrade
		OnPreUpgrade: func(c router.Context) (router.UpgradeData, error) {
			wsServer.logger.Info("OnPreUpgrade: extracting and validating token")

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

			// Get pre-validated auth data using the UpgradeData method
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

func setupClientRoutes[T any](app router.Server[T], wsServer *WebSocketServer, adminToken, userToken, moderatorToken string) {
	router.RegisterWSHandlers(app.Router(), router.WSClientHandlerConfig{
		Debug:    true,
		Minified: true,
	})

	// If you want to register all WebSocket client routes manually:
	// app.Router().Get("/client/client.js", router.WSClientHandler())
	// app.Router().Get("/client/client.min.js", router.WSClientMinHandler())
	// app.Router().Get("/client/client.d.ts", router.WSClientTypesHandler())
	// app.Router().Get("/client/examples.js", router.WSExamplesHandler())
	// app.Router().Get("/client/test", router.WSTestHandler())
	// app.Router().Get("/client/info", router.WebSocketClientInfoHandler())
	// app.Router().Get("/client/", func(c router.Context) error {
	// 	return c.Redirect("/client/test", 302)
	// })

	// Home page with information about available endpoints
	app.Router().Get("/", func(c router.Context) error {
		html := createIndexHTML()
		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		return c.Status(200).Send([]byte(html))
	})

	// Serve a small placeholder favicon to avoid 404 noise in browsers
	app.Router().Get("/favicon.ico", func(c router.Context) error {
		c.SetHeader("Content-Type", "image/x-icon")
		return c.Status(204).Send(nil)
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

	// API endpoint to get demo tokens
	app.Router().Get("/api/tokens", func(c router.Context) error {
		// Mint fresh demo tokens on each request so they don't expire during long-running sessions
		adminToken, _ := wsServer.authenticator.CreateToken("admin-001", "admin", "admin")
		userToken, _ := wsServer.authenticator.CreateToken("user-001", "john_doe", "user")
		moderatorToken, _ := wsServer.authenticator.CreateToken("mod-001", "jane_smith", "moderator")

		return c.JSON(200, map[string]any{
			"tokens": map[string]string{
				"admin":     adminToken,
				"user":      userToken,
				"moderator": moderatorToken,
			},
		})
	})

	// Quiet service worker lookups from the test page
	app.Router().Get("/sw.js", func(c router.Context) error {
		c.SetHeader("Content-Type", "application/javascript")
		return c.Status(200).SendString("// no-op service worker for demo\n")
	})

	wsServer.logger.Info("Client routes configured with embedded WebSocket client library")
}

func createIndexHTML() string {
	return `
<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Client Demo Server</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .container { max-width: 800px; margin: 0 auto; }
        .endpoint { margin: 20px 0; padding: 15px; background: #f5f5f5; border-radius: 5px; }
        .endpoint h3 { margin-top: 0; color: #333; }
        .endpoint a { color: #007bff; text-decoration: none; }
        .endpoint a:hover { text-decoration: underline; }
        code { background: #e9ecef; padding: 2px 4px; border-radius: 3px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 WebSocket Client Demo Server</h1>
        <p>This server demonstrates the embedded WebSocket client library for go-router.</p>

        <div class="endpoint">
            <h3>📡 WebSocket Endpoint</h3>
            <p>Connect to: <code>ws://localhost:3000/ws?token=&lt;jwt-token&gt;</code></p>
            <p>Authentication required via JWT token in query parameter.</p>
        </div>

        <div class="endpoint">
            <h3>📚 WebSocket Client Library</h3>
            <p><a href="/client/client.js">Download Client Library</a> (Development version)</p>
            <p><a href="/client/client.min.js">Download Minified Client</a> (Production version)</p>
            <p><a href="/client/client.d.ts">Download TypeScript Definitions</a></p>
            <p><a href="/client/examples.js">View Usage Examples</a></p>
            <p><a href="/client/info">View Library Information</a></p>
        </div>

        <div class="endpoint">
            <h3>🧪 Interactive Test Page</h3>
            <p><a href="/client/test">Open WebSocket Test Client</a></p>
            <p>Test the WebSocket connection and library features interactively.</p>
        </div>

        <div class="endpoint">
            <h3>🔧 API Endpoints</h3>
            <p><a href="/api/users">GET /api/users</a> - List connected users</p>
            <p><a href="/api/tokens">GET /api/tokens</a> - Get demo authentication tokens</p>
        </div>

        <div class="endpoint">
            <h3>💻 Usage Example</h3>
            <pre><code>// Include the client library
&lt;script src="/client/client.min.js"&gt;&lt;/script&gt;

// Create and connect client
const client = new WebSocketClient('ws://localhost:3000/ws', {
    token: 'your-jwt-token-here',
    autoReconnect: true,
    debug: true
});

client.on('connected', () =&gt; console.log('Connected!'));
client.on('auth_success', (data) =&gt; console.log('Authenticated:', data));
client.connect();</code></pre>
        </div>
    </div>
</body>
</html>
`
}
