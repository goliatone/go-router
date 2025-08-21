package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// RoomManager manages rooms within a hub
type RoomManager struct {
	// Room storage
	rooms   map[string]*Room
	roomsMu sync.RWMutex

	// Room type registry
	roomTypes   map[string]RoomConfig
	roomTypesMu sync.RWMutex

	// Global room hooks
	globalOnCreate  []RoomLifecycleHandler
	globalOnDestroy []RoomLifecycleHandler
	globalHooksMu   sync.RWMutex

	// Hub reference
	hub *WSHub

	// Configuration
	config RoomManagerConfig
}

// RoomManagerConfig contains configuration for the room manager
type RoomManagerConfig struct {
	// Maximum number of rooms allowed
	MaxRooms int

	// Default room configuration
	DefaultRoomConfig RoomConfig

	// Whether to allow dynamic room creation
	AllowDynamicRooms bool

	// Room naming pattern validation
	RoomNamePattern string
}

// NewRoomManager creates a new room manager
func NewRoomManager(hub *WSHub, config RoomManagerConfig) *RoomManager {
	if config.MaxRooms == 0 {
		config.MaxRooms = 10000 // Default max rooms
	}

	return &RoomManager{
		rooms:     make(map[string]*Room),
		roomTypes: make(map[string]RoomConfig),
		hub:       hub,
		config:    config,
	}
}

// CreateRoom creates a new room with the given configuration
func (rm *RoomManager) CreateRoom(ctx context.Context, id, name string, config RoomConfig) (*Room, error) {
	rm.roomsMu.Lock()
	defer rm.roomsMu.Unlock()

	// Check if room already exists
	if _, exists := rm.rooms[id]; exists {
		return nil, fmt.Errorf("room with id %s already exists", id)
	}

	// Check room limit
	if len(rm.rooms) >= rm.config.MaxRooms {
		return nil, errors.New("maximum number of rooms reached")
	}

	// Create the room
	room := NewRoom(id, name, config, rm.hub)

	// Add global hooks
	rm.globalHooksMu.RLock()
	for _, handler := range rm.globalOnCreate {
		room.OnCreate(handler)
	}
	for _, handler := range rm.globalOnDestroy {
		room.OnDestroy(handler)
	}
	rm.globalHooksMu.RUnlock()

	// Store the room
	rm.rooms[id] = room

	return room, nil
}

// CreateRoomFromType creates a room using a predefined type
func (rm *RoomManager) CreateRoomFromType(ctx context.Context, id, name, roomType string) (*Room, error) {
	rm.roomTypesMu.RLock()
	config, exists := rm.roomTypes[roomType]
	rm.roomTypesMu.RUnlock()

	if !exists {
		// Use default config if type doesn't exist
		config = rm.config.DefaultRoomConfig
	}

	config.Type = roomType
	return rm.CreateRoom(ctx, id, name, config)
}

// GetRoom retrieves a room by ID
func (rm *RoomManager) GetRoom(id string) (*Room, error) {
	rm.roomsMu.RLock()
	defer rm.roomsMu.RUnlock()

	room, exists := rm.rooms[id]
	if !exists {
		return nil, fmt.Errorf("room %s not found", id)
	}

	if room.IsDestroyed() {
		return nil, fmt.Errorf("room %s is destroyed", id)
	}

	return room, nil
}

// GetOrCreateRoom gets a room or creates it if it doesn't exist
func (rm *RoomManager) GetOrCreateRoom(ctx context.Context, id, name string, config RoomConfig) (*Room, error) {
	// Try to get existing room
	room, err := rm.GetRoom(id)
	if err == nil {
		return room, nil
	}

	// Create new room if allowed
	if !rm.config.AllowDynamicRooms {
		return nil, errors.New("dynamic room creation is not allowed")
	}

	return rm.CreateRoom(ctx, id, name, config)
}

// RemoveRoom removes a room from the manager
func (rm *RoomManager) RemoveRoom(id string) error {
	rm.roomsMu.Lock()
	defer rm.roomsMu.Unlock()

	room, exists := rm.rooms[id]
	if !exists {
		return fmt.Errorf("room %s not found", id)
	}

	// Destroy the room if not already destroyed
	if !room.IsDestroyed() {
		if err := room.Destroy(); err != nil {
			return err
		}
	}

	delete(rm.rooms, id)
	return nil
}

// ListRooms returns a list of all rooms
func (rm *RoomManager) ListRooms() []*Room {
	rm.roomsMu.RLock()
	defer rm.roomsMu.RUnlock()

	rooms := make([]*Room, 0, len(rm.rooms))
	for _, room := range rm.rooms {
		if !room.IsDestroyed() {
			rooms = append(rooms, room)
		}
	}
	return rooms
}

// ListRoomInfo returns information about all rooms
func (rm *RoomManager) ListRoomInfo() []RoomInfo {
	rooms := rm.ListRooms()
	info := make([]RoomInfo, 0, len(rooms))

	for _, room := range rooms {
		info = append(info, room.GetInfo())
	}

	return info
}

// FindRooms finds rooms matching the given criteria
func (rm *RoomManager) FindRooms(filter RoomFilter) []*Room {
	rm.roomsMu.RLock()
	defer rm.roomsMu.RUnlock()

	var matches []*Room
	for _, room := range rm.rooms {
		if room.IsDestroyed() {
			continue
		}

		if filter.Matches(room) {
			matches = append(matches, room)
		}
	}

	return matches
}

// RegisterRoomType registers a predefined room type
func (rm *RoomManager) RegisterRoomType(typeName string, config RoomConfig) {
	rm.roomTypesMu.Lock()
	defer rm.roomTypesMu.Unlock()
	rm.roomTypes[typeName] = config
}

// JoinRoom adds a client to a room
func (rm *RoomManager) JoinRoom(ctx context.Context, roomID string, client WSClient) error {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	return room.AddClient(ctx, client)
}

// LeaveRoom removes a client from a room
func (rm *RoomManager) LeaveRoom(ctx context.Context, roomID string, client WSClient) error {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	return room.RemoveClient(ctx, client)
}

// LeaveAllRooms removes a client from all rooms
func (rm *RoomManager) LeaveAllRooms(ctx context.Context, client WSClient) {
	rm.roomsMu.RLock()
	rooms := make([]*Room, 0, len(rm.rooms))
	for _, room := range rm.rooms {
		rooms = append(rooms, room)
	}
	rm.roomsMu.RUnlock()

	for _, room := range rooms {
		if room.HasClient(client.ID()) {
			room.RemoveClient(ctx, client)
		}
	}
}

// BroadcastToRoom broadcasts a message to all clients in a room
func (rm *RoomManager) BroadcastToRoom(ctx context.Context, roomID string, data []byte) error {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	return room.Broadcast(ctx, data)
}

// EmitToRoom emits an event to all clients in a room
func (rm *RoomManager) EmitToRoom(ctx context.Context, roomID, event string, data interface{}) error {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	return room.Emit(ctx, event, data)
}

// GetRoomPresence returns presence information for a room
func (rm *RoomManager) GetRoomPresence(roomID string) ([]RoomPresence, error) {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return nil, err
	}

	return room.GetPresence(), nil
}

// OnRoomCreate registers a global handler for room creation
func (rm *RoomManager) OnRoomCreate(handler RoomLifecycleHandler) {
	rm.globalHooksMu.Lock()
	defer rm.globalHooksMu.Unlock()
	rm.globalOnCreate = append(rm.globalOnCreate, handler)
}

// OnRoomDestroy registers a global handler for room destruction
func (rm *RoomManager) OnRoomDestroy(handler RoomLifecycleHandler) {
	rm.globalHooksMu.Lock()
	defer rm.globalHooksMu.Unlock()
	rm.globalOnDestroy = append(rm.globalOnDestroy, handler)
}

// Stats returns statistics about the room manager
func (rm *RoomManager) Stats() RoomManagerStats {
	rm.roomsMu.RLock()
	defer rm.roomsMu.RUnlock()

	stats := RoomManagerStats{
		TotalRooms:   len(rm.rooms),
		MaxRooms:     rm.config.MaxRooms,
		RoomTypes:    make(map[string]int),
		TotalClients: 0,
	}

	for _, room := range rm.rooms {
		if !room.IsDestroyed() {
			stats.ActiveRooms++
			stats.TotalClients += room.ClientCount()

			if room.Type() != "" {
				stats.RoomTypes[room.Type()]++
			}
		}
	}

	return stats
}

// RoomManagerStats contains statistics about the room manager
type RoomManagerStats struct {
	TotalRooms   int            `json:"total_rooms"`
	ActiveRooms  int            `json:"active_rooms"`
	MaxRooms     int            `json:"max_rooms"`
	TotalClients int            `json:"total_clients"`
	RoomTypes    map[string]int `json:"room_types"`
}

// RoomFilter defines criteria for filtering rooms
type RoomFilter struct {
	// Filter by room type
	Type string

	// Filter by tags (any match)
	Tags []string

	// Filter by whether room is private
	Private *bool

	// Filter by minimum/maximum client count
	MinClients *int
	MaxClients *int

	// Filter by metadata key/value
	MetadataKey   string
	MetadataValue interface{}

	// Custom filter function
	CustomFilter func(*Room) bool
}

// Matches checks if a room matches the filter criteria
func (f RoomFilter) Matches(room *Room) bool {
	// Check type
	if f.Type != "" && room.Type() != f.Type {
		return false
	}

	// Check tags
	if len(f.Tags) > 0 {
		roomTags := room.Tags()
		hasMatch := false
		for _, filterTag := range f.Tags {
			for _, roomTag := range roomTags {
				if filterTag == roomTag {
					hasMatch = true
					break
				}
			}
			if hasMatch {
				break
			}
		}
		if !hasMatch {
			return false
		}
	}

	// Check private
	if f.Private != nil && room.config.Private != *f.Private {
		return false
	}

	// Check client count
	clientCount := room.ClientCount()
	if f.MinClients != nil && clientCount < *f.MinClients {
		return false
	}
	if f.MaxClients != nil && clientCount > *f.MaxClients {
		return false
	}

	// Check metadata
	if f.MetadataKey != "" {
		metaValue := room.GetMetadata(f.MetadataKey)
		if metaValue != f.MetadataValue {
			return false
		}
	}

	// Check custom filter
	if f.CustomFilter != nil && !f.CustomFilter(room) {
		return false
	}

	return true
}

// Integrate with WSHub

// EnhanceWSHub adds room management capabilities to an existing hub
func EnhanceWSHub(hub *WSHub) {
	if hub.roomManager == nil {
		hub.roomManager = NewRoomManager(hub, RoomManagerConfig{
			AllowDynamicRooms: true,
			DefaultRoomConfig: RoomConfig{
				MaxClients:       100,
				DestroyWhenEmpty: true,
				TrackPresence:    true,
			},
		})
	}
}

// Add these methods to WSHub (would be added via edit)

func (h *WSHub) removeRoom(roomID string) error {
	if h.roomManager != nil {
		return h.roomManager.RemoveRoom(roomID)
	}
	return nil
}

func (h *WSHub) CreateRoom(ctx context.Context, id, name string, config RoomConfig) (*Room, error) {
	if h.roomManager == nil {
		return nil, errors.New("room manager not initialized")
	}
	return h.roomManager.CreateRoom(ctx, id, name, config)
}

func (h *WSHub) GetRoom(id string) (*Room, error) {
	if h.roomManager == nil {
		return nil, errors.New("room manager not initialized")
	}
	return h.roomManager.GetRoom(id)
}

func (h *WSHub) ListRooms() []RoomInfo {
	if h.roomManager == nil {
		return nil
	}
	return h.roomManager.ListRoomInfo()
}

func (h *WSHub) RoomStats() RoomManagerStats {
	if h.roomManager == nil {
		return RoomManagerStats{}
	}
	return h.roomManager.Stats()
}
