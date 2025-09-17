package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/goliatone/go-router"
)

// Example: JSON API over WebSocket

// APIRequest represents an API request
type APIRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Params  json.RawMessage   `json:"params,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// APIResponse represents an API response
type APIResponse struct {
	ID      string    `json:"id"`
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
	Time    time.Time `json:"time"`
}

// APIError represents an error response
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// Subscription represents a data subscription
type Subscription struct {
	ID       string
	Resource string
	Filters  map[string]any
	Client   *APIClient
}

// APIClient represents a connected API client
type APIClient struct {
	ID            string
	Conn          router.WebSocketContext
	Subscriptions map[string]*Subscription
	mu            sync.RWMutex
}

// DataStore simulates a data store
type DataStore struct {
	mu       sync.RWMutex
	users    map[string]map[string]any
	products map[string]map[string]any
	orders   map[string]map[string]any
}

// Global data store
var store = &DataStore{
	users:    make(map[string]map[string]any),
	products: make(map[string]map[string]any),
	orders:   make(map[string]map[string]any),
}

// Global subscription manager
var (
	subscriptions = make(map[string][]*Subscription)
	subMu         sync.RWMutex
)

func init() {
	// Seed some data
	store.users["1"] = map[string]any{
		"id":      "1",
		"name":    "John Doe",
		"email":   "john@example.com",
		"created": time.Now().Add(-24 * time.Hour),
	}

	store.products["1"] = map[string]any{
		"id":          "1",
		"name":        "Widget",
		"price":       9.99,
		"stock":       100,
		"description": "A useful widget",
	}

	store.products["2"] = map[string]any{
		"id":          "2",
		"name":        "Gadget",
		"price":       19.99,
		"stock":       50,
		"description": "An amazing gadget",
	}
}

// ExampleJSONAPIServer demonstrates a WebSocket JSON API server implementation
// NOTE: This is example code showing the intended API usage
func main() {
	// Create router
	app := router.NewHTTPServer()

	// WebSocket configuration
	wsConfig := router.DefaultWebSocketConfig()
	wsConfig.Origins = []string{"*"} // Allow all origins for demo
	// Increase read timeout for interactive demo (clients may not send data frequently)
	wsConfig.ReadTimeout = 300 * time.Second // 5 minutes
	wsConfig.PingPeriod = 60 * time.Second   // Send ping every minute
	wsConfig.PongWait = 120 * time.Second    // Wait up to 2 minutes for pong

	// JSON API WebSocket handler
	apiHandler := func(ws router.WebSocketContext) error {
		// Create API client
		client := &APIClient{
			ID:            ws.ConnectionID(),
			Conn:          ws,
			Subscriptions: make(map[string]*Subscription),
		}

		// Send welcome message
		welcome := APIResponse{
			ID:      "welcome",
			Success: true,
			Data: map[string]any{
				"message":    "Connected to JSON API",
				"version":    "1.0.0",
				"connection": client.ID,
			},
			Time: time.Now(),
		}
		ws.WriteJSON(welcome)

		// Handle requests
		for {
			var req APIRequest
			if err := ws.ReadJSON(&req); err != nil {
				log.Printf("Read error: %v", err)
				break
			}

			// Process request
			response := processRequest(client, req)

			// Send response
			if err := ws.WriteJSON(response); err != nil {
				log.Printf("Write error: %v", err)
				break
			}
		}

		// Cleanup subscriptions
		client.mu.Lock()
		for _, sub := range client.Subscriptions {
			removeSubscription(sub)
		}
		client.mu.Unlock()

		return nil
	}

	// Register routes
	app.Router().Get("/", apiPageHandler)
	app.Router().WebSocket("/ws/api", wsConfig, apiHandler)

	// Start server
	port := ":8083"
	log.Printf("JSON API Server starting on %s", port)
	log.Printf("Open http://localhost%s/ to test the API", port)

	log.Fatal(app.Serve(port))
}

// Process API request
func processRequest(client *APIClient, req APIRequest) APIResponse {
	resp := APIResponse{
		ID:   req.ID,
		Time: time.Now(),
	}

	switch req.Method {
	case "GET":
		data, err := handleGet(req.Path, req.Params)
		if err != nil {
			resp.Success = false
			resp.Error = err
		} else {
			resp.Success = true
			resp.Data = data
		}

	case "POST":
		data, err := handlePost(req.Path, req.Params)
		if err != nil {
			resp.Success = false
			resp.Error = err
		} else {
			resp.Success = true
			resp.Data = data
		}

	case "PUT":
		data, err := handlePut(req.Path, req.Params)
		if err != nil {
			resp.Success = false
			resp.Error = err
		} else {
			resp.Success = true
			resp.Data = data
		}

	case "DELETE":
		err := handleDelete(req.Path)
		if err != nil {
			resp.Success = false
			resp.Error = err
		} else {
			resp.Success = true
			resp.Data = map[string]string{"message": "Deleted successfully"}
		}

	case "SUBSCRIBE":
		subID, err := handleSubscribe(client, req.Path, req.Params)
		if err != nil {
			resp.Success = false
			resp.Error = err
		} else {
			resp.Success = true
			resp.Data = map[string]string{"subscription_id": subID}
		}

	case "UNSUBSCRIBE":
		var subIDData string
		if req.Params != nil {
			json.Unmarshal(req.Params, &subIDData)
		}
		err := handleUnsubscribe(client, subIDData)
		if err != nil {
			resp.Success = false
			resp.Error = err
		} else {
			resp.Success = true
			resp.Data = map[string]string{"message": "Unsubscribed successfully"}
		}

	default:
		resp.Success = false
		resp.Error = &APIError{
			Code:    405,
			Message: "Method not allowed",
			Details: fmt.Sprintf("Unknown method: %s", req.Method),
		}
	}

	return resp
}

// GET handler
func handleGet(path string, params json.RawMessage) (any, *APIError) {
	switch path {
	case "/users":
		store.mu.RLock()
		defer store.mu.RUnlock()
		users := make([]map[string]any, 0, len(store.users))
		for _, user := range store.users {
			users = append(users, user)
		}
		return users, nil

	case "/products":
		store.mu.RLock()
		defer store.mu.RUnlock()
		products := make([]map[string]any, 0, len(store.products))
		for _, product := range store.products {
			products = append(products, product)
		}
		return products, nil

	case "/orders":
		store.mu.RLock()
		defer store.mu.RUnlock()
		orders := make([]map[string]any, 0, len(store.orders))
		for _, order := range store.orders {
			orders = append(orders, order)
		}
		return orders, nil

	default:
		// Try to get specific resource
		if id := extractID(path); id != "" {
			if data := getResourceByID(path, id); data != nil {
				return data, nil
			}
		}
		return nil, &APIError{
			Code:    404,
			Message: "Resource not found",
			Details: fmt.Sprintf("Path: %s", path),
		}
	}
}

// POST handler
func handlePost(path string, params json.RawMessage) (any, *APIError) {
	var data map[string]any
	if err := json.Unmarshal(params, &data); err != nil {
		return nil, &APIError{
			Code:    400,
			Message: "Invalid JSON",
			Details: err.Error(),
		}
	}

	// Generate ID
	data["id"] = fmt.Sprintf("%d", time.Now().UnixNano())
	data["created"] = time.Now()

	store.mu.Lock()
	defer store.mu.Unlock()

	switch path {
	case "/users":
		store.users[data["id"].(string)] = data
		notifySubscribers("users", "create", data)
		return data, nil

	case "/products":
		store.products[data["id"].(string)] = data
		notifySubscribers("products", "create", data)
		return data, nil

	case "/orders":
		store.orders[data["id"].(string)] = data
		notifySubscribers("orders", "create", data)
		return data, nil

	default:
		return nil, &APIError{
			Code:    404,
			Message: "Endpoint not found",
			Details: fmt.Sprintf("Path: %s", path),
		}
	}
}

// PUT handler
func handlePut(path string, params json.RawMessage) (any, *APIError) {
	id := extractID(path)
	if id == "" {
		return nil, &APIError{
			Code:    400,
			Message: "ID required for update",
		}
	}

	var updates map[string]any
	if err := json.Unmarshal(params, &updates); err != nil {
		return nil, &APIError{
			Code:    400,
			Message: "Invalid JSON",
			Details: err.Error(),
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	resource := getResourceType(path)
	var collection map[string]map[string]any

	switch resource {
	case "users":
		collection = store.users
	case "products":
		collection = store.products
	case "orders":
		collection = store.orders
	default:
		return nil, &APIError{
			Code:    404,
			Message: "Resource type not found",
		}
	}

	if existing, ok := collection[id]; ok {
		// Merge updates
		for k, v := range updates {
			existing[k] = v
		}
		existing["updated"] = time.Now()
		notifySubscribers(resource, "update", existing)
		return existing, nil
	}

	return nil, &APIError{
		Code:    404,
		Message: "Resource not found",
		Details: fmt.Sprintf("ID: %s", id),
	}
}

// DELETE handler
func handleDelete(path string) *APIError {
	id := extractID(path)
	if id == "" {
		return &APIError{
			Code:    400,
			Message: "ID required for delete",
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	resource := getResourceType(path)
	switch resource {
	case "users":
		if _, ok := store.users[id]; ok {
			delete(store.users, id)
			notifySubscribers(resource, "delete", map[string]string{"id": id})
			return nil
		}
	case "products":
		if _, ok := store.products[id]; ok {
			delete(store.products, id)
			notifySubscribers(resource, "delete", map[string]string{"id": id})
			return nil
		}
	case "orders":
		if _, ok := store.orders[id]; ok {
			delete(store.orders, id)
			notifySubscribers(resource, "delete", map[string]string{"id": id})
			return nil
		}
	}

	return &APIError{
		Code:    404,
		Message: "Resource not found",
		Details: fmt.Sprintf("ID: %s", id),
	}
}

// Subscribe handler
func handleSubscribe(client *APIClient, resource string, params json.RawMessage) (string, *APIError) {
	sub := &Subscription{
		ID:       fmt.Sprintf("sub_%d", time.Now().UnixNano()),
		Resource: resource,
		Client:   client,
	}

	if params != nil {
		json.Unmarshal(params, &sub.Filters)
	}

	// Add to client subscriptions
	client.mu.Lock()
	client.Subscriptions[sub.ID] = sub
	client.mu.Unlock()

	// Add to global subscriptions
	subMu.Lock()
	subscriptions[resource] = append(subscriptions[resource], sub)
	subMu.Unlock()

	log.Printf("[SUBSCRIBE] Client %s subscribed to '%s' (ID: %s). Total subscriptions for '%s': %d", client.ID[:8], resource, sub.ID, resource, len(subscriptions[resource]))
	return sub.ID, nil
}

// Unsubscribe handler
func handleUnsubscribe(client *APIClient, subID string) *APIError {
	client.mu.Lock()
	sub, ok := client.Subscriptions[subID]
	if !ok {
		client.mu.Unlock()
		return &APIError{
			Code:    404,
			Message: "Subscription not found",
		}
	}
	delete(client.Subscriptions, subID)
	client.mu.Unlock()

	removeSubscription(sub)
	return nil
}

// Helper functions
func extractID(path string) string {
	// Simple ID extraction from path like /users/123
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func getResourceType(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func getResourceByID(path, id string) map[string]any {
	store.mu.RLock()
	defer store.mu.RUnlock()

	resource := getResourceType(path)
	switch resource {
	case "users":
		return store.users[id]
	case "products":
		return store.products[id]
	case "orders":
		return store.orders[id]
	}
	return nil
}

func removeSubscription(sub *Subscription) {
	subMu.Lock()
	defer subMu.Unlock()

	subs := subscriptions[sub.Resource]
	for i, s := range subs {
		if s.ID == sub.ID {
			subscriptions[sub.Resource] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func notifySubscribers(resource, action string, data any) {
	subMu.RLock()
	subs := subscriptions[resource]
	subMu.RUnlock()

	log.Printf("[NOTIFY] Sending '%s' notification for resource '%s' to %d subscribers", action, resource, len(subs))

	if len(subs) == 0 {
		log.Printf("[NOTIFY] No subscribers found for resource '%s'", resource)
		return
	}

	notification := APIResponse{
		ID:      fmt.Sprintf("notify_%d", time.Now().UnixNano()),
		Success: true,
		Data: map[string]any{
			"type":     "notification",
			"resource": resource,
			"action":   action,
			"data":     data,
		},
		Time: time.Now(),
	}

	for _, sub := range subs {
		// Send notification to subscriber
		go func(s *Subscription) {
			if err := s.Client.Conn.WriteJSON(notification); err != nil {
				log.Printf("[NOTIFY] Failed to send notification to client %s: %v", s.Client.ID[:8], err)
			}
		}(sub)
	}
}

// API test page
func apiPageHandler(ctx router.Context) error {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>WebSocket JSON API</title>
    <style>
        body {
            font-family: 'Monaco', 'Courier New', monospace;
            margin: 0;
            padding: 20px;
            background: #1e1e1e;
            color: #d4d4d4;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 20px;
        }
        .panel {
            background: #252526;
            border: 1px solid #3c3c3c;
            border-radius: 5px;
            padding: 20px;
        }
        h2 {
            color: #4ec9b0;
            margin-top: 0;
        }
        button {
            background: #0e639c;
            color: white;
            border: none;
            padding: 8px 15px;
            margin: 5px;
            border-radius: 3px;
            cursor: pointer;
        }
        button:hover {
            background: #1177bb;
        }
        textarea {
            width: 100%;
            height: 150px;
            background: #1e1e1e;
            color: #d4d4d4;
            border: 1px solid #3c3c3c;
            padding: 10px;
            font-family: inherit;
            font-size: 14px;
        }
        #responses {
            height: 400px;
            overflow-y: auto;
            background: #1e1e1e;
            border: 1px solid #3c3c3c;
            padding: 10px;
            margin-top: 10px;
        }
        .response {
            margin: 10px 0;
            padding: 10px;
            background: #2d2d30;
            border-left: 3px solid #4ec9b0;
        }
        .error {
            border-left-color: #f48771;
        }
        .notification {
            border-left-color: #dcdcaa;
        }
        pre {
            margin: 0;
            white-space: pre-wrap;
        }
        .status {
            padding: 5px 10px;
            background: #f48771;
            display: inline-block;
            border-radius: 3px;
            margin-bottom: 10px;
        }
        .status.connected {
            background: #4ec9b0;
        }
        .examples {
            margin-top: 20px;
        }
        .example {
            background: #1e1e1e;
            padding: 10px;
            margin: 5px 0;
            border: 1px solid #3c3c3c;
            cursor: pointer;
        }
        .example:hover {
            background: #2d2d30;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="panel">
            <h2>JSON API WebSocket Client</h2>
            <div class="status" id="status">Disconnected</div>

            <div>
                <button onclick="connect()">Connect</button>
                <button onclick="disconnect()">Disconnect</button>
                <button onclick="clearResponses()">Clear</button>
            </div>

            <h3>Request</h3>
            <textarea id="requestInput" placeholder='{"id":"1","method":"GET","path":"/products"}'></textarea>

            <div>
                <button onclick="sendRequest()">Send Request</button>
            </div>

            <div class="examples">
                <h3>Examples (click to use)</h3>
                <div class="example" onclick='useExample({"id":"1","method":"GET","path":"/products"})'>
                    GET /products
                </div>
                <div class="example" onclick='useExample({"id":"2","method":"POST","path":"/products","params":{"name":"New Product","price":29.99}})'>
                    POST /products
                </div>
                <div class="example" onclick='useExample({"id":"3","method":"PUT","path":"/products/1","params":{"price":39.99}})'>
                    PUT /products/1
                </div>
                <div class="example" onclick='useExample({"id":"4","method":"DELETE","path":"/products/2"})'>
                    DELETE /products/2
                </div>
                <div class="example" onclick='useExample({"id":"5","method":"SUBSCRIBE","path":"products"})'>
                    SUBSCRIBE products
                </div>
            </div>
        </div>

        <div class="panel">
            <h2>Responses</h2>
            <div id="responses"></div>
        </div>
    </div>

    <script>
        let ws = null;
        let requestId = 1;

        function connect() {
            if (ws) return;

            ws = new WebSocket('ws://localhost:8083/ws/api');

            ws.onopen = function() {
                document.getElementById('status').textContent = 'Connected';
                document.getElementById('status').className = 'status connected';
            };

            ws.onmessage = function(event) {
                const response = JSON.parse(event.data);
                displayResponse(response);
            };

            ws.onerror = function(error) {
                console.error('WebSocket error:', error);
            };

            ws.onclose = function() {
                document.getElementById('status').textContent = 'Disconnected';
                document.getElementById('status').className = 'status';
                ws = null;
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
            }
        }

        function sendRequest() {
            if (!ws || ws.readyState !== WebSocket.OPEN) {
                alert('Not connected');
                return;
            }

            const input = document.getElementById('requestInput');
            try {
                const request = JSON.parse(input.value);
                if (!request.id) {
                    request.id = String(requestId++);
                }
                ws.send(JSON.stringify(request));
                displayResponse({ type: 'request', ...request });
            } catch (e) {
                alert('Invalid JSON: ' + e.message);
            }
        }

        function displayResponse(response) {
            const responses = document.getElementById('responses');
            const div = document.createElement('div');

            let className = 'response';
            if (response.error) className += ' error';
            if (response.data && response.data.type === 'notification') className += ' notification';
            if (response.type === 'request') className = 'response';

            div.className = className;

            const pre = document.createElement('pre');
            pre.textContent = JSON.stringify(response, null, 2);
            div.appendChild(pre);

            responses.insertBefore(div, responses.firstChild);

            // Keep only last 20 responses
            while (responses.children.length > 20) {
                responses.removeChild(responses.lastChild);
            }
        }

        function useExample(example) {
            document.getElementById('requestInput').value = JSON.stringify(example, null, 2);
        }

        function clearResponses() {
            document.getElementById('responses').innerHTML = '';
        }

        // Auto-connect
        window.onload = function() {
            setTimeout(connect, 500);
        };
    </script>
</body>
</html>`

	ctx.SetHeader("Content-Type", "text/html; charset=utf-8")
	return ctx.SendString(html)
}

// Note: convertToHTTPHandler function removed - using unified app.Serve() instead
