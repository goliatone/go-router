//go:build skip
// +build skip

package router_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

func TestRoomCreation(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	// Create a room
	room, err := hub.CreateRoom(ctx, "test-room-1", "Test Room 1", router.RoomConfig{
		MaxClients:       10,
		DestroyWhenEmpty: false,
		TrackPresence:    true,
		Private:          false,
		Type:             "chat",
		Tags:             []string{"public", "general"},
	})

	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	if room.ID() != "test-room-1" {
		t.Errorf("Expected room ID 'test-room-1', got '%s'", room.ID())
	}

	if room.Name() != "Test Room 1" {
		t.Errorf("Expected room name 'Test Room 1', got '%s'", room.Name())
	}

	if room.Type() != "chat" {
		t.Errorf("Expected room type 'chat', got '%s'", room.Type())
	}

	// Try to create duplicate room
	_, err = hub.CreateRoom(ctx, "test-room-1", "Duplicate", router.RoomConfig{})
	if err == nil {
		t.Error("Expected error when creating duplicate room")
	}
}

func TestRoomPresenceTracking(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	// Create room with presence tracking
	room, err := hub.CreateRoom(ctx, "presence-room", "Presence Room", router.RoomConfig{
		TrackPresence: true,
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Create mock clients
	client1 := createMockWSClient("client-1", hub)
	client1.Set("username", "Alice")

	client2 := createMockWSClient("client-2", hub)
	client2.Set("username", "Bob")

	// Add clients to room
	err = room.AddClient(ctx, client1)
	if err != nil {
		t.Errorf("Failed to add client1: %v", err)
	}

	err = room.AddClient(ctx, client2)
	if err != nil {
		t.Errorf("Failed to add client2: %v", err)
	}

	// Check presence
	presence := room.GetPresence()
	if len(presence) != 2 {
		t.Errorf("Expected 2 clients in presence, got %d", len(presence))
	}

	// Check client count
	if room.ClientCount() != 2 {
		t.Errorf("Expected 2 clients, got %d", room.ClientCount())
	}

	// Remove a client
	err = room.RemoveClient(ctx, client1)
	if err != nil {
		t.Errorf("Failed to remove client1: %v", err)
	}

	// Check updated presence
	presence = room.GetPresence()
	if len(presence) != 1 {
		t.Errorf("Expected 1 client in presence after removal, got %d", len(presence))
	}
}

func TestRoomCapacityLimits(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	// Create room with small capacity
	room, err := hub.CreateRoom(ctx, "small-room", "Small Room", router.RoomConfig{
		MaxClients: 2,
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Add clients up to capacity
	client1 := createMockWSClient("client-1", hub)
	client2 := createMockWSClient("client-2", hub)
	client3 := createMockWSClient("client-3", hub)

	err = room.AddClient(ctx, client1)
	if err != nil {
		t.Errorf("Failed to add client1: %v", err)
	}

	err = room.AddClient(ctx, client2)
	if err != nil {
		t.Errorf("Failed to add client2: %v", err)
	}

	// Try to exceed capacity
	err = room.AddClient(ctx, client3)
	if err == nil {
		t.Error("Expected error when exceeding room capacity")
	}
}

func TestRoomStateManagement(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	room, err := hub.CreateRoom(ctx, "state-room", "State Room", router.RoomConfig{})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Set various state values
	room.SetState("topic", "General Discussion")
	room.SetState("messageCount", 42)
	room.SetState("locked", true)

	// Retrieve state values
	if room.GetStateString("topic") != "General Discussion" {
		t.Error("Failed to retrieve string state")
	}

	if room.GetStateInt("messageCount") != 42 {
		t.Error("Failed to retrieve int state")
	}

	if !room.GetStateBool("locked") {
		t.Error("Failed to retrieve bool state")
	}

	// Clear state
	room.ClearState()

	if room.GetState("topic") != nil {
		t.Error("State should be cleared")
	}
}

func TestRoomMetadata(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	room, err := hub.CreateRoom(ctx, "meta-room", "Meta Room", router.RoomConfig{})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Set metadata
	room.SetMetadata("creator", "admin")
	room.SetMetadata("created_at", time.Now())
	room.SetMetadata("description", "Test room for metadata")

	// Get metadata
	if room.GetMetadata("creator") != "admin" {
		t.Error("Failed to retrieve metadata")
	}

	// Get all metadata
	allMeta := room.GetAllMetadata()
	if len(allMeta) != 3 {
		t.Errorf("Expected 3 metadata entries, got %d", len(allMeta))
	}
}

func TestRoomEventHooks(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	var (
		onJoinCalled    bool
		onLeaveCalled   bool
		onCreateCalled  bool
		onDestroyCalled bool
	)

	room, err := hub.CreateRoom(ctx, "hook-room", "Hook Room", router.RoomConfig{})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Register hooks
	room.OnJoin(func(ctx context.Context, r *router.Room, client router.WSClient) error {
		onJoinCalled = true
		return nil
	})

	room.OnLeave(func(ctx context.Context, r *router.Room, client router.WSClient) error {
		onLeaveCalled = true
		return nil
	})

	room.OnCreate(func(ctx context.Context, r *router.Room) error {
		onCreateCalled = true
		return nil
	})

	room.OnDestroy(func(ctx context.Context, r *router.Room) error {
		onDestroyCalled = true
		return nil
	})

	// Add and remove client to trigger hooks
	client := createMockWSClient("client-1", hub)

	err = room.AddClient(ctx, client)
	if err != nil {
		t.Errorf("Failed to add client: %v", err)
	}

	// Give time for async hook execution
	time.Sleep(100 * time.Millisecond)

	if !onJoinCalled {
		t.Error("OnJoin hook was not called")
	}

	err = room.RemoveClient(ctx, client)
	if err != nil {
		t.Errorf("Failed to remove client: %v", err)
	}

	// Give time for async hook execution
	time.Sleep(100 * time.Millisecond)

	if !onLeaveCalled {
		t.Error("OnLeave hook was not called")
	}

	// Destroy room
	err = room.Destroy()
	if err != nil {
		t.Errorf("Failed to destroy room: %v", err)
	}

	// Give time for async hook execution
	time.Sleep(100 * time.Millisecond)

	if !onDestroyCalled {
		t.Error("OnDestroy hook was not called")
	}
}

func TestRoomAuthorization(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	// Create private room with auth function
	room, err := hub.CreateRoom(ctx, "private-room", "Private Room", router.RoomConfig{
		Private: true,
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Set authorization function
	room.SetAuthFunc(func(ctx context.Context, r *router.Room, client router.WSClient) (bool, error) {
		// Only allow clients with "authorized" flag
		return client.GetBool("authorized"), nil
	})

	// Create authorized and unauthorized clients
	authorizedClient := createMockWSClient("auth-client", hub)
	authorizedClient.Set("authorized", true)

	unauthorizedClient := createMockWSClient("unauth-client", hub)
	unauthorizedClient.Set("authorized", false)

	// Try to add authorized client
	err = room.AddClient(ctx, authorizedClient)
	if err != nil {
		t.Errorf("Authorized client should be allowed: %v", err)
	}

	// Try to add unauthorized client
	err = room.AddClient(ctx, unauthorizedClient)
	if err == nil {
		t.Error("Unauthorized client should not be allowed")
	}
}

func TestRoomBroadcasting(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	room, err := hub.CreateRoom(ctx, "broadcast-room", "Broadcast Room", router.RoomConfig{})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Add multiple clients
	clients := make([]router.WSClient, 3)
	for i := range clients {
		clients[i] = createMockWSClient(string(rune('a'+i)), hub)
		err = room.AddClient(ctx, clients[i])
		if err != nil {
			t.Errorf("Failed to add client %d: %v", i, err)
		}
	}

	// Broadcast to all
	err = room.Broadcast(ctx, []byte("hello all"))
	if err != nil {
		t.Errorf("Failed to broadcast: %v", err)
	}

	// Broadcast except one
	err = room.BroadcastExcept(ctx, []byte("hello others"), []string{clients[0].ID()})
	if err != nil {
		t.Errorf("Failed to broadcast except: %v", err)
	}

	// Emit event
	err = room.Emit(ctx, "test-event", map[string]string{"msg": "test"})
	if err != nil {
		t.Errorf("Failed to emit event: %v", err)
	}
}

func TestRoomFiltering(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	// Create rooms with different properties
	_, err := hub.CreateRoom(ctx, "chat-public", "Public Chat", router.RoomConfig{
		Type:    "chat",
		Private: false,
		Tags:    []string{"public", "general"},
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	_, err = hub.CreateRoom(ctx, "chat-private", "Private Chat", router.RoomConfig{
		Type:    "chat",
		Private: true,
		Tags:    []string{"private", "vip"},
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	_, err = hub.CreateRoom(ctx, "game-public", "Public Game", router.RoomConfig{
		Type:    "game",
		Private: false,
		Tags:    []string{"public", "gaming"},
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// List all rooms
	rooms := hub.ListRooms()
	if len(rooms) != 3 {
		t.Errorf("Expected 3 rooms, got %d", len(rooms))
	}

	// Get room stats
	stats := hub.RoomStats()
	if stats.TotalRooms != 3 {
		t.Errorf("Expected 3 total rooms, got %d", stats.TotalRooms)
	}
}

func TestRoomDestroyWhenEmpty(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	// Create room that destroys when empty with short delay
	room, err := hub.CreateRoom(ctx, "auto-destroy", "Auto Destroy", router.RoomConfig{
		DestroyWhenEmpty:  true,
		EmptyDestroyDelay: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Add and remove a client
	client := createMockWSClient("client-1", hub)

	err = room.AddClient(ctx, client)
	if err != nil {
		t.Errorf("Failed to add client: %v", err)
	}

	err = room.RemoveClient(ctx, client)
	if err != nil {
		t.Errorf("Failed to remove client: %v", err)
	}

	// Room should still exist immediately
	if room.IsDestroyed() {
		t.Error("Room should not be destroyed immediately")
	}

	// Wait for auto-destroy
	time.Sleep(200 * time.Millisecond)

	// Room should be destroyed now
	if !room.IsDestroyed() {
		t.Error("Room should be destroyed after delay")
	}
}

func TestConcurrentRoomOperations(t *testing.T) {
	hub := router.NewWSHub()
	defer hub.Close()

	ctx := context.Background()

	room, err := hub.CreateRoom(ctx, "concurrent-room", "Concurrent Room", router.RoomConfig{
		MaxClients: 100,
	})
	if err != nil {
		t.Fatalf("Failed to create room: %v", err)
	}

	// Concurrent client additions
	var wg sync.WaitGroup
	numClients := 50

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			client := createMockWSClient(string(rune('a'+id)), hub)
			err := room.AddClient(ctx, client)
			if err != nil {
				t.Errorf("Failed to add client %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	// Check final count
	if room.ClientCount() != numClients {
		t.Errorf("Expected %d clients, got %d", numClients, room.ClientCount())
	}

	// Concurrent broadcasts
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			room.Broadcast(ctx, []byte(string(rune('0'+n))))
		}(i)
	}

	wg.Wait()
}

// Helper to create mock WebSocket client
func createMockWSClient(id string, hub *router.WSHub) router.WSClient {
	// This would use the mock from websocket_highlevel_test.go
	// For now, return a basic implementation
	return &mockWSClientImpl{
		id:    id,
		hub:   hub,
		state: make(map[string]interface{}),
	}
}

type mockWSClientImpl struct {
	id    string
	hub   *router.WSHub
	state map[string]interface{}
	mu    sync.RWMutex
}

func (m *mockWSClientImpl) ID() string                                             { return m.id }
func (m *mockWSClientImpl) ConnectionID() string                                   { return m.id }
func (m *mockWSClientImpl) Context() context.Context                               { return context.Background() }
func (m *mockWSClientImpl) SetContext(ctx context.Context)                         {}
func (m *mockWSClientImpl) OnMessage(handler router.MessageHandler) error          { return nil }
func (m *mockWSClientImpl) OnJSON(event string, handler router.JSONHandler) error  { return nil }
func (m *mockWSClientImpl) Send(data []byte) error                                 { return nil }
func (m *mockWSClientImpl) SendJSON(v interface{}) error                           { return nil }
func (m *mockWSClientImpl) SendWithContext(ctx context.Context, data []byte) error { return nil }
func (m *mockWSClientImpl) SendJSONWithContext(ctx context.Context, v interface{}) error {
	return nil
}
func (m *mockWSClientImpl) Broadcast(data []byte) error                                 { return nil }
func (m *mockWSClientImpl) BroadcastJSON(v interface{}) error                           { return nil }
func (m *mockWSClientImpl) BroadcastWithContext(ctx context.Context, data []byte) error { return nil }
func (m *mockWSClientImpl) BroadcastJSONWithContext(ctx context.Context, v interface{}) error {
	return nil
}
func (m *mockWSClientImpl) Join(room string) error                                  { return nil }
func (m *mockWSClientImpl) JoinWithContext(ctx context.Context, room string) error  { return nil }
func (m *mockWSClientImpl) Leave(room string) error                                 { return nil }
func (m *mockWSClientImpl) LeaveWithContext(ctx context.Context, room string) error { return nil }
func (m *mockWSClientImpl) Room(name string) router.RoomBroadcaster                 { return nil }
func (m *mockWSClientImpl) Rooms() []string                                         { return nil }

func (m *mockWSClientImpl) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state[key] = value
}

func (m *mockWSClientImpl) SetWithContext(ctx context.Context, key string, value interface{}) {
	m.Set(key, value)
}

func (m *mockWSClientImpl) Get(key string) interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state[key]
}

func (m *mockWSClientImpl) GetString(key string) string {
	v := m.Get(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (m *mockWSClientImpl) GetInt(key string) int {
	v := m.Get(key)
	if i, ok := v.(int); ok {
		return i
	}
	return 0
}

func (m *mockWSClientImpl) GetBool(key string) bool {
	v := m.Get(key)
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func (m *mockWSClientImpl) Close(code int, reason string) error { return nil }
func (m *mockWSClientImpl) CloseWithContext(ctx context.Context, code int, reason string) error {
	return nil
}
func (m *mockWSClientImpl) IsConnected() bool { return true }
func (m *mockWSClientImpl) Query(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}
func (m *mockWSClientImpl) Conn() router.WebSocketContext { return nil }
