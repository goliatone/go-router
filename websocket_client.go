package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// WSClient represents a high-level WebSocket client with automatic pump management
type WSClient interface {
	// Core identification
	ID() string
	ConnectionID() string

	// Context support
	Context() context.Context
	SetContext(ctx context.Context)

	// Message handling
	OnMessage(handler MessageHandler) error
	OnJSON(event string, handler JSONHandler) error
	Send(data []byte) error
	SendJSON(v any) error
	SendWithContext(ctx context.Context, data []byte) error
	SendJSONWithContext(ctx context.Context, v any) error

	// Broadcasting
	Broadcast(data []byte) error
	BroadcastJSON(v any) error
	BroadcastWithContext(ctx context.Context, data []byte) error
	BroadcastJSONWithContext(ctx context.Context, v any) error

	// Room management (for Phase 2, basic interface now)
	Join(room string) error
	JoinWithContext(ctx context.Context, room string) error
	Leave(room string) error
	LeaveWithContext(ctx context.Context, room string) error
	Room(name string) RoomBroadcaster
	Rooms() []string

	// Client state management
	Set(key string, value any)
	SetWithContext(ctx context.Context, key string, value any)
	Get(key string) any
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool

	// Connection management
	Close(code int, reason string) error
	CloseWithContext(ctx context.Context, code int, reason string) error
	IsConnected() bool

	// Query parameters
	Query(key string, defaultValue ...string) string

	// Event emission
	Emit(event string, data any) error
	EmitWithContext(ctx context.Context, event string, data any) error

	// Low-level access when needed
	Conn() WebSocketContext
}

// MessageHandler handles raw messages with context and error support
type MessageHandler func(ctx context.Context, data []byte) error

// JSONHandler handles JSON messages with context and error support
type JSONHandler func(ctx context.Context, data json.RawMessage) error

// EventHandler handles typed events with context and error support
type EventHandler func(ctx context.Context, client WSClient, data any) error

// RoomBroadcaster allows broadcasting to a specific room
type RoomBroadcaster interface {
	Emit(event string, data any) error
	EmitWithContext(ctx context.Context, event string, data any) error
	Except(clients ...WSClient) RoomBroadcaster
	Clients() []WSClient
}

// wsClient is the default implementation of WSClient
type wsClient struct {
	id     string
	conn   WebSocketContext
	hub    *WSHub
	ctx    context.Context
	cancel context.CancelFunc

	// Message handling
	messageHandlers []MessageHandler
	jsonHandlers    map[string][]JSONHandler

	// State management
	state   map[string]any
	stateMu sync.RWMutex

	// Room management
	rooms   map[string]bool
	roomsMu sync.RWMutex

	// Channel management
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once

	// Error handling
	errHandler func(error)
	logger     Logger
}

// NewWSClient creates a new WebSocket client with automatic pump management
func NewWSClient(conn WebSocketContext, hub *WSHub) WSClient {
	ctx, cancel := context.WithCancel(context.Background())

	c := &wsClient{
		id:           conn.ConnectionID(),
		conn:         conn,
		hub:          hub,
		ctx:          ctx,
		cancel:       cancel,
		jsonHandlers: make(map[string][]JSONHandler),
		state:        make(map[string]any),
		rooms:        make(map[string]bool),
		send:         make(chan []byte, 256),
		done:         make(chan struct{}),
		logger:       &defaultLogger{},
	}

	// Start automatic pump management
	go c.writePump()
	go c.readPump()

	return c
}

// ID returns the client's unique identifier
func (c *wsClient) ID() string {
	return c.id
}

// ConnectionID returns the underlying connection ID
func (c *wsClient) ConnectionID() string {
	return c.conn.ConnectionID()
}

// Context returns the client's context
func (c *wsClient) Context() context.Context {
	return c.ctx
}

// SetContext updates the client's context
func (c *wsClient) SetContext(ctx context.Context) {
	c.ctx = ctx
}

// OnMessage registers a raw message handler
func (c *wsClient) OnMessage(handler MessageHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}
	c.messageHandlers = append(c.messageHandlers, handler)
	return nil
}

// OnJSON registers a JSON event handler
func (c *wsClient) OnJSON(event string, handler JSONHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}
	c.jsonHandlers[event] = append(c.jsonHandlers[event], handler)
	return nil
}

// Send sends raw data to the client
func (c *wsClient) Send(data []byte) error {
	return c.SendWithContext(c.ctx, data)
}

// SendWithContext sends raw data with context
func (c *wsClient) SendWithContext(ctx context.Context, data []byte) error {
	select {
	case c.send <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("connection closed")
	}
}

// SendJSON sends JSON data to the client
func (c *wsClient) SendJSON(v any) error {
	return c.SendJSONWithContext(c.ctx, v)
}

// SendJSONWithContext sends JSON data with context
func (c *wsClient) SendJSONWithContext(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return c.SendWithContext(ctx, data)
}

// Broadcast sends data to all connected clients
func (c *wsClient) Broadcast(data []byte) error {
	return c.BroadcastWithContext(c.ctx, data)
}

// BroadcastWithContext sends data to all connected clients with context
func (c *wsClient) BroadcastWithContext(ctx context.Context, data []byte) error {
	if c.hub == nil {
		return errors.New("client not connected to hub")
	}
	return c.hub.BroadcastWithContext(ctx, data)
}

// BroadcastJSON sends JSON data to all connected clients
func (c *wsClient) BroadcastJSON(v any) error {
	return c.BroadcastJSONWithContext(c.ctx, v)
}

// BroadcastJSONWithContext sends JSON data to all connected clients with context
func (c *wsClient) BroadcastJSONWithContext(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return c.BroadcastWithContext(ctx, data)
}

// Join adds the client to a room
func (c *wsClient) Join(room string) error {
	return c.JoinWithContext(c.ctx, room)
}

// JoinWithContext adds the client to a room with context
func (c *wsClient) JoinWithContext(ctx context.Context, room string) error {
	c.roomsMu.Lock()
	defer c.roomsMu.Unlock()

	c.rooms[room] = true
	if c.hub != nil && c.hub.roomManager != nil {
		return c.hub.roomManager.JoinRoom(ctx, room, c)
	}
	return nil
}

// Leave removes the client from a room
func (c *wsClient) Leave(room string) error {
	return c.LeaveWithContext(c.ctx, room)
}

// LeaveWithContext removes the client from a room with context
func (c *wsClient) LeaveWithContext(ctx context.Context, room string) error {
	c.roomsMu.Lock()
	defer c.roomsMu.Unlock()

	delete(c.rooms, room)
	if c.hub != nil && c.hub.roomManager != nil {
		return c.hub.roomManager.LeaveRoom(ctx, room, c)
	}
	return nil
}

// Room returns a broadcaster for a specific room
func (c *wsClient) Room(name string) RoomBroadcaster {
	if c.hub == nil {
		return &nullRoomBroadcaster{}
	}
	return c.hub.Room(name)
}

// Rooms returns the list of rooms the client is in
func (c *wsClient) Rooms() []string {
	c.roomsMu.RLock()
	defer c.roomsMu.RUnlock()

	rooms := make([]string, 0, len(c.rooms))
	for room := range c.rooms {
		rooms = append(rooms, room)
	}
	return rooms
}

// Set stores a value in the client's state
func (c *wsClient) Set(key string, value any) {
	c.SetWithContext(c.ctx, key, value)
}

// SetWithContext stores a value in the client's state with context
func (c *wsClient) SetWithContext(ctx context.Context, key string, value any) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.state[key] = value
}

// Get retrieves a value from the client's state
func (c *wsClient) Get(key string) any {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state[key]
}

// GetString retrieves a string value from the client's state
func (c *wsClient) GetString(key string) string {
	v := c.Get(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetInt retrieves an int value from the client's state
func (c *wsClient) GetInt(key string) int {
	v := c.Get(key)
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// GetBool retrieves a bool value from the client's state
func (c *wsClient) GetBool(key string) bool {
	v := c.Get(key)
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// Close closes the connection with a status code and reason
func (c *wsClient) Close(code int, reason string) error {
	return c.CloseWithContext(c.ctx, code, reason)
}

// CloseWithContext closes the connection with context
func (c *wsClient) CloseWithContext(ctx context.Context, code int, reason string) error {
	var err error
	c.closeOnce.Do(func() {
		// Send close message
		closeMsg := make([]byte, 2+len(reason))
		closeMsg[0] = byte(code >> 8)
		closeMsg[1] = byte(code)
		copy(closeMsg[2:], reason)

		c.conn.WriteMessage(CloseMessage, closeMsg)
		c.conn.Close()

		// Clean up resources
		c.cancel()
		close(c.done)

		// Remove from hub
		if c.hub != nil {
			c.hub.unregister(c)
		}
	})
	return err
}

// IsConnected returns true if the client is still connected
func (c *wsClient) IsConnected() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Query retrieves a query parameter value from the WebSocket upgrade request
func (c *wsClient) Query(key string, defaultValue ...string) string {
	if c.conn == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return c.conn.Query(key, defaultValue...)
}

// Emit sends an event with data
func (c *wsClient) Emit(event string, data any) error {
	return c.EmitWithContext(c.ctx, event, data)
}

// EmitWithContext sends an event with data and context
func (c *wsClient) EmitWithContext(ctx context.Context, event string, data any) error {
	payload := map[string]any{
		"type": event,
		"data": data,
	}
	return c.SendJSONWithContext(ctx, payload)
}

// Conn returns the underlying WebSocket connection
func (c *wsClient) Conn() WebSocketContext {
	return c.conn
}

// readPump handles incoming messages automatically
func (c *wsClient) readPump() {
	defer func() {
		c.Close(CloseNormalClosure, "")
	}()

	for {
		messageType, data, err := c.conn.ReadMessage()
		if err != nil {
			c.logger.Error("WebSocket read error",
				"client_id", c.ID(),
				"error", err)
			if c.errHandler != nil {
				c.errHandler(err)
			}
			return
		}

		// Handle different message types
		switch messageType {
		case TextMessage, BinaryMessage:
			// Process through handlers
			for _, handler := range c.messageHandlers {
				if err := handler(c.ctx, data); err != nil {
					c.logger.Error("Message handler failed",
						"client_id", c.ID(),
						"error", err)
					if c.errHandler != nil {
						c.errHandler(err)
					}
				}
			}

			// Try to parse as JSON event
			var event struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(data, &event); err == nil && event.Type != "" {
				if handlers, ok := c.jsonHandlers[event.Type]; ok {
					for _, handler := range handlers {
						if err := handler(c.ctx, event.Data); err != nil {
							c.logger.Error("JSON handler failed",
								"client_id", c.ID(),
								"event_type", event.Type,
								"error", err)
							if c.errHandler != nil {
								c.errHandler(err)
							}
						}
					}
				}
			}

		case PingMessage:
			// Respond with pong
			c.conn.WritePong(data)

		case CloseMessage:
			return
		}
	}
}

// writePump handles outgoing messages automatically
func (c *wsClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(TextMessage, message); err != nil {
				c.logger.Error("WebSocket write error",
					"client_id", c.ID(),
					"error", err)
				return
			}

		case <-ticker.C:
			if err := c.conn.WritePing([]byte{}); err != nil {
				c.logger.Error("WebSocket ping error",
					"client_id", c.ID(),
					"error", err)
				return
			}

		case <-c.done:
			return
		}
	}
}

// nullRoomBroadcaster is a no-op room broadcaster
type nullRoomBroadcaster struct{}

func (n *nullRoomBroadcaster) Emit(event string, data any) error { return nil }
func (n *nullRoomBroadcaster) EmitWithContext(ctx context.Context, event string, data any) error {
	return nil
}
func (n *nullRoomBroadcaster) Except(clients ...WSClient) RoomBroadcaster { return n }
func (n *nullRoomBroadcaster) Clients() []WSClient                        { return nil }
