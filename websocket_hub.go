package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// WSHub manages WebSocket connections with event-driven architecture
type WSHub struct {
	// Client management
	clients   map[string]WSClient
	clientsMu sync.RWMutex

	// Advanced room management
	roomManager *RoomManager

	// Event handlers
	connectHandlers    []EventHandler
	disconnectHandlers []EventHandler
	eventHandlers      map[string][]EventHandler
	errorHandlers      []WSErrorHandler
	handlersMu         sync.RWMutex

	// Channel management
	register     chan WSClient
	unregisterCh chan WSClient
	broadcast    chan broadcastMessage

	// Context for hub lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Logging
	logger Logger

	// Configuration
	config WSHubConfig
}

// WSHubConfig contains configuration for the WebSocket hub
type WSHubConfig struct {
	// Maximum message size in bytes
	MaxMessageSize int64

	// Connection timeouts
	HandshakeTimeout time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration

	// Ping/Pong configuration
	PingPeriod time.Duration
	PongWait   time.Duration

	// Buffer sizes
	ReadBufferSize  int
	WriteBufferSize int

	// Enable compression
	EnableCompression bool
}

// WSErrorHandler handles errors with context
type WSErrorHandler func(ctx context.Context, client WSClient, err error)

// broadcastMessage represents a message to broadcast
type broadcastMessage struct {
	ctx    context.Context
	data   []byte
	room   string
	except map[string]bool
}

// DefaultWSHubConfig returns default configuration
func DefaultWSHubConfig() WSHubConfig {
	return WSHubConfig{
		MaxMessageSize:    1024 * 1024, // 1MB
		HandshakeTimeout:  10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingPeriod:        54 * time.Second,
		PongWait:          60 * time.Second,
		ReadBufferSize:    1024,
		WriteBufferSize:   1024,
		EnableCompression: false,
	}
}

// NewWSHub creates a new WebSocket hub
func NewWSHub(opts ...func(*WSHubConfig)) *WSHub {
	config := DefaultWSHubConfig()
	for _, opt := range opts {
		opt(&config)
	}

	ctx, cancel := context.WithCancel(context.Background())

	hub := &WSHub{
		clients:       make(map[string]WSClient),
		eventHandlers: make(map[string][]EventHandler),
		register:      make(chan WSClient),
		unregisterCh:  make(chan WSClient),
		broadcast:     make(chan broadcastMessage),
		ctx:           ctx,
		cancel:        cancel,
		logger:        &defaultLogger{},
		config:        config,
	}

	// Initialize room manager
	hub.roomManager = NewRoomManager(hub, RoomManagerConfig{
		AllowDynamicRooms: true,
		DefaultRoomConfig: RoomConfig{
			MaxClients:       100,
			DestroyWhenEmpty: true,
			TrackPresence:    true,
		},
	})

	// Start the hub's event loop
	go hub.run()

	return hub
}

// run processes hub events
func (h *WSHub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client.ID()] = client

			// Call connect handlers
			h.handlersMu.RLock()
			handlers := h.connectHandlers
			h.handlersMu.RUnlock()

			for _, handler := range handlers {
				go func(handler EventHandler) {
					if err := handler(h.ctx, client, nil); err != nil {
						h.handleError(h.ctx, client, err)
					}
				}(handler)
			}

		case client := <-h.unregisterCh:
			if _, ok := h.clients[client.ID()]; ok {
				delete(h.clients, client.ID())

				// Remove from all rooms using RoomManager
				if h.roomManager != nil {
					h.roomManager.LeaveAllRooms(h.ctx, client)
				}

				// Call disconnect handlers
				h.handlersMu.RLock()
				handlers := h.disconnectHandlers
				h.handlersMu.RUnlock()

				for _, handler := range handlers {
					go func(handler EventHandler) {
						if err := handler(h.ctx, client, nil); err != nil {
							h.handleError(h.ctx, client, err)
						}
					}(handler)
				}
			}

		case msg := <-h.broadcast:
			if msg.room != "" {
				// Broadcast to specific room using RoomManager
				if h.roomManager != nil {
					if room, err := h.roomManager.GetRoom(msg.room); err == nil {
						// Convert except map to slice
						var except []string
						if msg.except != nil {
							except = make([]string, 0, len(msg.except))
							for clientID := range msg.except {
								except = append(except, clientID)
							}
						}
						// Use broadcast methods from Room
						if except != nil {
							room.BroadcastExcept(msg.ctx, msg.data, except)
						} else {
							room.Broadcast(msg.ctx, msg.data)
						}
					}
				}
			} else {
				// Broadcast to all clients
				h.broadcastToAll(msg)
			}

		case <-h.ctx.Done():
			return
		}
	}
}

// OnConnect registers a handler for client connections
func (h *WSHub) OnConnect(handler EventHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	h.handlersMu.Lock()
	h.connectHandlers = append(h.connectHandlers, handler)
	h.handlersMu.Unlock()

	return nil
}

// OnDisconnect registers a handler for client disconnections
func (h *WSHub) OnDisconnect(handler EventHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	h.handlersMu.Lock()
	h.disconnectHandlers = append(h.disconnectHandlers, handler)
	h.handlersMu.Unlock()

	return nil
}

// On registers an event handler
func (h *WSHub) On(event string, handler EventHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	h.handlersMu.Lock()
	h.eventHandlers[event] = append(h.eventHandlers[event], handler)
	h.handlersMu.Unlock()

	return nil
}

// OnError registers an error handler
func (h *WSHub) OnError(handler WSErrorHandler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	h.handlersMu.Lock()
	h.errorHandlers = append(h.errorHandlers, handler)
	h.handlersMu.Unlock()

	return nil
}

// Emit triggers an event with data
func (h *WSHub) Emit(event string, data any) error {
	return h.EmitWithContext(h.ctx, event, data)
}

// EmitWithContext triggers an event with context
func (h *WSHub) EmitWithContext(ctx context.Context, event string, data any) error {
	h.handlersMu.RLock()
	handlers, ok := h.eventHandlers[event]
	h.handlersMu.RUnlock()

	if !ok {
		return nil // No handlers for this event
	}

	// Execute handlers for all clients
	h.clientsMu.RLock()
	clients := make([]WSClient, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.clientsMu.RUnlock()

	var lastErr error
	for _, client := range clients {
		for _, handler := range handlers {
			if err := handler(ctx, client, data); err != nil {
				h.handleError(ctx, client, err)
				lastErr = err
			}
		}
	}

	return lastErr
}

// Broadcast sends data to all connected clients
func (h *WSHub) Broadcast(data []byte) error {
	return h.BroadcastWithContext(h.ctx, data)
}

// BroadcastWithContext sends data to all connected clients with context
func (h *WSHub) BroadcastWithContext(ctx context.Context, data []byte) error {
	select {
	case h.broadcast <- broadcastMessage{ctx: ctx, data: data}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BroadcastJSON sends JSON data to all connected clients
func (h *WSHub) BroadcastJSON(v any) error {
	return h.BroadcastJSONWithContext(h.ctx, v)
}

// BroadcastJSONWithContext sends JSON data to all connected clients with context
func (h *WSHub) BroadcastJSONWithContext(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return h.BroadcastWithContext(ctx, data)
}

// Room returns a room broadcaster
func (h *WSHub) Room(name string) RoomBroadcaster {
	// Use RoomManager exclusively
	if h.roomManager != nil {
		if room, err := h.roomManager.GetRoom(name); err == nil {
			return &advancedRoomBroadcaster{room: room}
		}
		// If room doesn't exist, try to create it (RoomManager handles dynamic room creation)
		if room, err := h.roomManager.GetOrCreateRoom(h.ctx, name, name, RoomConfig{
			MaxClients:       100,
			DestroyWhenEmpty: true,
			TrackPresence:    true,
		}); err == nil {
			return &advancedRoomBroadcaster{room: room}
		}
	}

	// Return nil if RoomManager is not available or room creation fails
	return nil
}

// Handler returns an HTTP handler for WebSocket connections
func (h *WSHub) Handler() func(Context) error {
	return func(ctx Context) error {
		// Check if this is a WebSocket context
		wsCtx, ok := ctx.(WebSocketContext)
		if !ok {
			return ctx.Status(400).SendString("WebSocket upgrade required")
		}

		// Create a new client
		client := NewWSClient(wsCtx, h)

		// Register the client
		h.register <- client

		// Wait for client to disconnect
		<-client.(*wsClient).done

		return nil
	}
}

// ClientCount returns the number of connected clients
func (h *WSHub) ClientCount() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}

// Clients returns all connected clients
func (h *WSHub) Clients() []WSClient {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	clients := make([]WSClient, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	return clients
}

// Close shuts down the hub
func (h *WSHub) Close() error {
	h.cancel()

	// Close all client connections
	h.clientsMu.Lock()
	for _, client := range h.clients {
		client.Close(CloseGoingAway, "server shutdown")
	}
	h.clientsMu.Unlock()

	return nil
}

// Internal methods

func (h *WSHub) handleError(ctx context.Context, client WSClient, err error) {
	h.handlersMu.RLock()
	handlers := h.errorHandlers
	h.handlersMu.RUnlock()

	for _, handler := range handlers {
		handler(ctx, client, err)
	}
}

func (h *WSHub) broadcastToAll(msg broadcastMessage) {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	for id, client := range h.clients {
		if msg.except != nil && msg.except[id] {
			continue
		}

		// Non-blocking send
		go func(c WSClient) {
			if err := c.SendWithContext(msg.ctx, msg.data); err != nil {
				h.logger.Error("Failed to send message to client",
					"client_id", c.ID(),
					"error", err)
			}
		}(client)
	}
}

func (h *WSHub) unregister(client WSClient) {
	select {
	case h.unregisterCh <- client:
	default:
		// Hub might be shutting down
	}
}

// advancedRoomBroadcaster implements RoomBroadcaster for advanced rooms
type advancedRoomBroadcaster struct {
	room   *Room
	except []string
}

func (r *advancedRoomBroadcaster) Emit(event string, data any) error {
	return r.EmitWithContext(context.Background(), event, data)
}

func (r *advancedRoomBroadcaster) EmitWithContext(ctx context.Context, event string, data any) error {
	if r.except != nil {
		return r.room.EmitExcept(ctx, event, data, r.except)
	}
	return r.room.Emit(ctx, event, data)
}

func (r *advancedRoomBroadcaster) Except(clients ...WSClient) RoomBroadcaster {
	except := make([]string, len(clients))
	for i, client := range clients {
		except[i] = client.ID()
	}
	return &advancedRoomBroadcaster{
		room:   r.room,
		except: except,
	}
}

func (r *advancedRoomBroadcaster) Clients() []WSClient {
	return r.room.Clients()
}
