package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Room represents a WebSocket room with advanced features
type Room struct {
	// Basic properties
	id          string
	name        string
	createdAt   time.Time
	destroyedAt *time.Time

	// Client management
	clients   map[string]WSClient
	clientsMu sync.RWMutex

	// Room state
	state   map[string]any
	stateMu sync.RWMutex

	// Metadata
	metadata   map[string]any
	metadataMu sync.RWMutex

	// Configuration
	config RoomConfig

	// Event hooks
	onJoin    []RoomEventHandler
	onLeave   []RoomEventHandler
	onCreate  []RoomLifecycleHandler
	onDestroy []RoomLifecycleHandler
	hooksMu   sync.RWMutex

	// Access control
	authFunc RoomAuthFunc

	// Parent hub reference
	hub *WSHub

	// Logging
	logger Logger

	// Lifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	destroyed bool
	destroyMu sync.RWMutex
}

// RoomConfig contains configuration for a room
type RoomConfig struct {
	// Maximum number of clients allowed in the room
	MaxClients int

	// Whether the room should be destroyed when empty
	DestroyWhenEmpty bool

	// Time to wait before destroying an empty room
	EmptyDestroyDelay time.Duration

	// Whether to track presence
	TrackPresence bool

	// Whether the room is private (requires authorization)
	Private bool

	// Custom room type for categorization
	Type string

	// Room tags for filtering
	Tags []string
}

// RoomEventHandler handles room join/leave events
type RoomEventHandler func(ctx context.Context, room *Room, client WSClient) error

// RoomLifecycleHandler handles room creation/destruction
type RoomLifecycleHandler func(ctx context.Context, room *Room) error

// RoomAuthFunc determines if a client can join a room
type RoomAuthFunc func(ctx context.Context, room *Room, client WSClient) (bool, error)

// RoomPresence tracks who's in the room
type RoomPresence struct {
	ClientID string         `json:"client_id"`
	Username string         `json:"username,omitempty"`
	JoinedAt time.Time      `json:"joined_at"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// RoomInfo provides public information about a room
type RoomInfo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	ClientCount int            `json:"client_count"`
	MaxClients  int            `json:"max_clients"`
	CreatedAt   time.Time      `json:"created_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Private     bool           `json:"private"`
}

// NewRoom creates a new room with the given configuration
func NewRoom(id, name string, config RoomConfig, hub *WSHub) *Room {
	ctx, cancel := context.WithCancel(context.Background())

	room := &Room{
		id:        id,
		name:      name,
		createdAt: time.Now(),
		clients:   make(map[string]WSClient),
		state:     make(map[string]any),
		metadata:  make(map[string]any),
		config:    config,
		hub:       hub,
		logger:    &defaultLogger{},
		ctx:       ctx,
		cancel:    cancel,
	}

	// Set default values
	if config.MaxClients == 0 {
		room.config.MaxClients = 1000 // Default max clients
	}

	if config.EmptyDestroyDelay == 0 {
		room.config.EmptyDestroyDelay = 5 * time.Minute // Default delay
	}

	// Trigger onCreate hooks
	room.triggerOnCreate()

	// Start empty room destroyer if configured
	if config.DestroyWhenEmpty {
		go room.watchEmpty()
	}

	return room
}

// ID returns the room's unique identifier
func (r *Room) ID() string {
	return r.id
}

// Name returns the room's name
func (r *Room) Name() string {
	return r.name
}

// Type returns the room's type
func (r *Room) Type() string {
	return r.config.Type
}

// Tags returns the room's tags
func (r *Room) Tags() []string {
	return r.config.Tags
}

// AddClient adds a client to the room
func (r *Room) AddClient(ctx context.Context, client WSClient) error {
	r.destroyMu.RLock()
	if r.destroyed {
		r.destroyMu.RUnlock()
		return errors.New("room is destroyed")
	}
	r.destroyMu.RUnlock()

	// Check authorization
	if r.config.Private && r.authFunc != nil {
		allowed, err := r.authFunc(ctx, r, client)
		if err != nil {
			return fmt.Errorf("authorization check failed: %w", err)
		}
		if !allowed {
			return errors.New("client not authorized to join room")
		}
	}

	// Check capacity
	r.clientsMu.RLock()
	currentCount := len(r.clients)
	r.clientsMu.RUnlock()

	if currentCount >= r.config.MaxClients {
		return fmt.Errorf("room is full (%d/%d)", currentCount, r.config.MaxClients)
	}

	// Add client
	r.clientsMu.Lock()
	r.clients[client.ID()] = client
	r.clientsMu.Unlock()

	// Trigger onJoin hooks
	r.triggerOnJoin(ctx, client)

	// Notify other clients if presence tracking is enabled
	if r.config.TrackPresence {
		r.broadcastPresenceUpdate(ctx, "join", client)
	}

	return nil
}

// RemoveClient removes a client from the room
func (r *Room) RemoveClient(ctx context.Context, client WSClient) error {
	r.clientsMu.Lock()
	delete(r.clients, client.ID())
	isEmpty := len(r.clients) == 0
	r.clientsMu.Unlock()

	// Trigger onLeave hooks
	r.triggerOnLeave(ctx, client)

	// Notify other clients if presence tracking is enabled
	if r.config.TrackPresence {
		r.broadcastPresenceUpdate(ctx, "leave", client)
	}

	// Check if room should be destroyed
	if isEmpty && r.config.DestroyWhenEmpty {
		// Room will be destroyed by watchEmpty goroutine
	}

	return nil
}

// HasClient checks if a client is in the room
func (r *Room) HasClient(clientID string) bool {
	r.clientsMu.RLock()
	defer r.clientsMu.RUnlock()
	_, exists := r.clients[clientID]
	return exists
}

// ClientCount returns the number of clients in the room
func (r *Room) ClientCount() int {
	r.clientsMu.RLock()
	defer r.clientsMu.RUnlock()
	return len(r.clients)
}

// Clients returns all clients in the room
func (r *Room) Clients() []WSClient {
	r.clientsMu.RLock()
	defer r.clientsMu.RUnlock()

	clients := make([]WSClient, 0, len(r.clients))
	for _, client := range r.clients {
		clients = append(clients, client)
	}
	return clients
}

// GetPresence returns presence information for all clients in the room
func (r *Room) GetPresence() []RoomPresence {
	if !r.config.TrackPresence {
		return nil
	}

	r.clientsMu.RLock()
	defer r.clientsMu.RUnlock()

	presence := make([]RoomPresence, 0, len(r.clients))
	for _, client := range r.clients {
		p := RoomPresence{
			ClientID: client.ID(),
			Username: client.GetString("username"),
			JoinedAt: time.Now(), // In production, track actual join time
			Metadata: make(map[string]any),
		}
		presence = append(presence, p)
	}

	return presence
}

// Broadcast sends a message to all clients in the room
func (r *Room) Broadcast(ctx context.Context, data []byte) error {
	return r.BroadcastExcept(ctx, data, nil)
}

// BroadcastExcept sends a message to all clients except specified ones
func (r *Room) BroadcastExcept(ctx context.Context, data []byte, except []string) error {
	exceptMap := make(map[string]bool)
	for _, id := range except {
		exceptMap[id] = true
	}

	r.clientsMu.RLock()
	clients := make([]WSClient, 0, len(r.clients))
	for id, client := range r.clients {
		if !exceptMap[id] {
			clients = append(clients, client)
		}
	}
	r.clientsMu.RUnlock()

	// Send to each client
	var lastErr error
	for _, client := range clients {
		if err := client.SendWithContext(ctx, data); err != nil {
			r.logger.Error("Failed to broadcast message to client in room",
				"room_id", r.id,
				"room_name", r.name,
				"client_id", client.ID(),
				"error", err)
			lastErr = err
		}
	}

	return lastErr
}

// BroadcastJSON sends JSON data to all clients in the room
func (r *Room) BroadcastJSON(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return r.Broadcast(ctx, data)
}

// Emit sends an event to all clients in the room
func (r *Room) Emit(ctx context.Context, event string, data any) error {
	payload := map[string]any{
		"type": event,
		"room": r.name,
		"data": data,
	}
	return r.BroadcastJSON(ctx, payload)
}

// EmitExcept sends an event to all clients except specified ones
func (r *Room) EmitExcept(ctx context.Context, event string, data any, except []string) error {
	payload := map[string]any{
		"type": event,
		"room": r.name,
		"data": data,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	return r.BroadcastExcept(ctx, jsonData, except)
}

// State management

// SetState sets a value in the room's state
func (r *Room) SetState(key string, value any) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state[key] = value
}

// GetState gets a value from the room's state
func (r *Room) GetState(key string) any {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	return r.state[key]
}

// GetStateString gets a string value from the room's state
func (r *Room) GetStateString(key string) string {
	v := r.GetState(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetStateInt gets an int value from the room's state
func (r *Room) GetStateInt(key string) int {
	v := r.GetState(key)
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

// GetStateBool gets a bool value from the room's state
func (r *Room) GetStateBool(key string) bool {
	v := r.GetState(key)
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// ClearState clears all state
func (r *Room) ClearState() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.state = make(map[string]any)
}

// Metadata management

// SetMetadata sets metadata for the room
func (r *Room) SetMetadata(key string, value any) {
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata[key] = value
}

// GetMetadata gets metadata from the room
func (r *Room) GetMetadata(key string) any {
	r.metadataMu.RLock()
	defer r.metadataMu.RUnlock()
	return r.metadata[key]
}

// GetAllMetadata returns all metadata
func (r *Room) GetAllMetadata() map[string]any {
	r.metadataMu.RLock()
	defer r.metadataMu.RUnlock()

	meta := make(map[string]any)
	for k, v := range r.metadata {
		meta[k] = v
	}
	return meta
}

// Event hooks

// OnJoin registers a handler for when clients join the room
func (r *Room) OnJoin(handler RoomEventHandler) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	r.onJoin = append(r.onJoin, handler)
}

// OnLeave registers a handler for when clients leave the room
func (r *Room) OnLeave(handler RoomEventHandler) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	r.onLeave = append(r.onLeave, handler)
}

// OnCreate registers a handler for when the room is created
func (r *Room) OnCreate(handler RoomLifecycleHandler) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	r.onCreate = append(r.onCreate, handler)
}

// OnDestroy registers a handler for when the room is destroyed
func (r *Room) OnDestroy(handler RoomLifecycleHandler) {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()
	r.onDestroy = append(r.onDestroy, handler)
}

// SetAuthFunc sets the authorization function for the room
func (r *Room) SetAuthFunc(authFunc RoomAuthFunc) {
	r.authFunc = authFunc
}

// GetInfo returns public information about the room
func (r *Room) GetInfo() RoomInfo {
	r.clientsMu.RLock()
	clientCount := len(r.clients)
	r.clientsMu.RUnlock()

	return RoomInfo{
		ID:          r.id,
		Name:        r.name,
		Type:        r.config.Type,
		Tags:        r.config.Tags,
		ClientCount: clientCount,
		MaxClients:  r.config.MaxClients,
		CreatedAt:   r.createdAt,
		Metadata:    r.GetAllMetadata(),
		Private:     r.config.Private,
	}
}

// Destroy destroys the room
func (r *Room) Destroy() error {
	r.destroyMu.Lock()
	defer r.destroyMu.Unlock()

	if r.destroyed {
		return errors.New("room already destroyed")
	}

	r.destroyed = true
	now := time.Now()
	r.destroyedAt = &now

	// Trigger onDestroy hooks
	r.triggerOnDestroy()

	// Remove all clients
	r.clientsMu.Lock()
	for _, client := range r.clients {
		// Notify client they're being removed
		client.SendJSON(map[string]any{
			"type":   "room:destroyed",
			"room":   r.name,
			"reason": "room destroyed",
		})
	}
	r.clients = make(map[string]WSClient)
	r.clientsMu.Unlock()

	// Cancel context
	r.cancel()

	// Remove from hub if it exists
	if r.hub != nil {
		r.hub.removeRoom(r.id)
	}

	return nil
}

// IsDestroyed returns whether the room is destroyed
func (r *Room) IsDestroyed() bool {
	r.destroyMu.RLock()
	defer r.destroyMu.RUnlock()
	return r.destroyed
}

// Internal methods

func (r *Room) triggerOnJoin(ctx context.Context, client WSClient) {
	r.hooksMu.RLock()
	handlers := r.onJoin
	r.hooksMu.RUnlock()

	for _, handler := range handlers {
		go func(h RoomEventHandler) {
			if err := h(ctx, r, client); err != nil {
				r.logger.Error("Room onJoin handler failed",
					"room_id", r.id,
					"room_name", r.name,
					"client_id", client.ID(),
					"error", err)
			}
		}(handler)
	}
}

func (r *Room) triggerOnLeave(ctx context.Context, client WSClient) {
	r.hooksMu.RLock()
	handlers := r.onLeave
	r.hooksMu.RUnlock()

	for _, handler := range handlers {
		go func(h RoomEventHandler) {
			if err := h(ctx, r, client); err != nil {
				r.logger.Error("Room onLeave handler failed",
					"room_id", r.id,
					"room_name", r.name,
					"client_id", client.ID(),
					"error", err)
			}
		}(handler)
	}
}

func (r *Room) triggerOnCreate() {
	r.hooksMu.RLock()
	handlers := r.onCreate
	r.hooksMu.RUnlock()

	for _, handler := range handlers {
		go func(h RoomLifecycleHandler) {
			if err := h(r.ctx, r); err != nil {
				r.logger.Error("Room onCreate handler failed",
					"room_id", r.id,
					"room_name", r.name,
					"error", err)
			}
		}(handler)
	}
}

func (r *Room) triggerOnDestroy() {
	r.hooksMu.RLock()
	handlers := r.onDestroy
	r.hooksMu.RUnlock()

	for _, handler := range handlers {
		go func(h RoomLifecycleHandler) {
			if err := h(r.ctx, r); err != nil {
				r.logger.Error("Room onDestroy handler failed",
					"room_id", r.id,
					"room_name", r.name,
					"error", err)
			}
		}(handler)
	}
}

func (r *Room) broadcastPresenceUpdate(ctx context.Context, action string, client WSClient) {
	presence := RoomPresence{
		ClientID: client.ID(),
		Username: client.GetString("username"),
		JoinedAt: time.Now(),
	}

	if err := r.EmitExcept(ctx, "presence:"+action, presence, []string{client.ID()}); err != nil {
		r.logger.Error("Failed to broadcast presence update",
			"room_id", r.id,
			"room_name", r.name,
			"action", action,
			"client_id", client.ID(),
			"error", err)
	}
}

func (r *Room) watchEmpty() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var emptyStartTime *time.Time

	for {
		select {
		case <-ticker.C:
			r.clientsMu.RLock()
			isEmpty := len(r.clients) == 0
			r.clientsMu.RUnlock()

			if isEmpty {
				if emptyStartTime == nil {
					now := time.Now()
					emptyStartTime = &now
				} else if time.Since(*emptyStartTime) > r.config.EmptyDestroyDelay {
					// Room has been empty for too long, destroy it
					r.Destroy()
					return
				}
			} else {
				// Room is not empty, reset timer
				emptyStartTime = nil
			}

		case <-r.ctx.Done():
			return
		}
	}
}
