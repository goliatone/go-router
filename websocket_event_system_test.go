package router_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

func TestEventRouter(t *testing.T) {
	t.Run("Create router with default config", func(t *testing.T) {
		config := router.EventRouterConfig{}
		r := router.NewEventRouter(config)
		
		if r == nil {
			t.Fatal("Expected router to be created")
		}
	})
	
	t.Run("Register and trigger global handler", func(t *testing.T) {
		r := router.NewEventRouter(router.EventRouterConfig{})
		
		called := false
		handler := &router.GenericEventHandler{
			Type: "test",
			Handler: func(ctx context.Context, client router.WSClient, event *router.EventMessage) error {
				called = true
				if event.Type != "test" {
					t.Errorf("Expected event type 'test', got %s", event.Type)
				}
				return nil
			},
		}
		
		err := r.On("test", handler)
		if err != nil {
			t.Fatalf("Failed to register handler: %v", err)
		}
		
		mockClient := &mockWSClient{id: "test-client"}
		event := &router.EventMessage{
			Type: "test",
			Data: "test data",
		}
		
		err = r.RouteEvent(context.Background(), mockClient, event)
		if err != nil {
			t.Fatalf("Failed to route event: %v", err)
		}
		
		if !called {
			t.Error("Handler was not called")
		}
	})
	
	t.Run("Namespace isolation", func(t *testing.T) {
		r := router.NewEventRouter(router.EventRouterConfig{})
		
		ns1 := r.Namespace("ns1")
		ns2 := r.Namespace("ns2")
		
		ns1Called := false
		ns2Called := false
		
		ns1.On("test", &router.GenericEventHandler{
			Type: "test",
			Handler: func(ctx context.Context, client router.WSClient, event *router.EventMessage) error {
				ns1Called = true
				return nil
			},
		})
		
		ns2.On("test", &router.GenericEventHandler{
			Type: "test",
			Handler: func(ctx context.Context, client router.WSClient, event *router.EventMessage) error {
				ns2Called = true
				return nil
			},
		})
		
		mockClient := &mockWSClient{id: "test-client"}
		
		// Route to ns1
		event1 := &router.EventMessage{
			Type:      "test",
			Namespace: "ns1",
		}
		r.RouteEvent(context.Background(), mockClient, event1)
		
		if !ns1Called {
			t.Error("NS1 handler should have been called")
		}
		if ns2Called {
			t.Error("NS2 handler should not have been called")
		}
		
		// Reset and route to ns2
		ns1Called = false
		ns2Called = false
		
		event2 := &router.EventMessage{
			Type:      "test",
			Namespace: "ns2",
		}
		r.RouteEvent(context.Background(), mockClient, event2)
		
		if ns1Called {
			t.Error("NS1 handler should not have been called")
		}
		if !ns2Called {
			t.Error("NS2 handler should have been called")
		}
	})
	
	t.Run("Middleware chain", func(t *testing.T) {
		r := router.NewEventRouter(router.EventRouterConfig{})
		
		order := []string{}
		mu := sync.Mutex{}
		
		// Add middleware in order
		r.Use(func(ctx context.Context, client router.WSClient, event *router.EventMessage, next router.EventMiddlewareNext) error {
			mu.Lock()
			order = append(order, "middleware1")
			mu.Unlock()
			return next(ctx, client, event)
		})
		
		r.Use(func(ctx context.Context, client router.WSClient, event *router.EventMessage, next router.EventMiddlewareNext) error {
			mu.Lock()
			order = append(order, "middleware2")
			mu.Unlock()
			return next(ctx, client, event)
		})
		
		r.On("test", &router.GenericEventHandler{
			Type: "test",
			Handler: func(ctx context.Context, client router.WSClient, event *router.EventMessage) error {
				mu.Lock()
				order = append(order, "handler")
				mu.Unlock()
				return nil
			},
		})
		
		mockClient := &mockWSClient{id: "test-client"}
		event := &router.EventMessage{Type: "test"}
		
		r.RouteEvent(context.Background(), mockClient, event)
		
		expected := []string{"middleware1", "middleware2", "handler"}
		if len(order) != len(expected) {
			t.Fatalf("Expected %d calls, got %d", len(expected), len(order))
		}
		
		for i, v := range expected {
			if order[i] != v {
				t.Errorf("Expected order[%d] = %s, got %s", i, v, order[i])
			}
		}
	})
	
	t.Run("Type-safe handler", func(t *testing.T) {
		r := router.NewEventRouter(router.EventRouterConfig{})
		
		type TestData struct {
			Message string `json:"message"`
			Count   int    `json:"count"`
		}
		
		received := false
		handler := router.TypedHandler("test", func(ctx context.Context, client router.WSClient, data TestData) error {
			received = true
			if data.Message != "hello" {
				t.Errorf("Expected message 'hello', got %s", data.Message)
			}
			if data.Count != 42 {
				t.Errorf("Expected count 42, got %d", data.Count)
			}
			return nil
		})
		
		r.On("test", handler)
		
		mockClient := &mockWSClient{id: "test-client"}
		event := &router.EventMessage{
			Type: "test",
			Data: TestData{Message: "hello", Count: 42},
		}
		
		r.RouteEvent(context.Background(), mockClient, event)
		
		if !received {
			t.Error("Handler was not called")
		}
	})
	
	t.Run("Namespace authorization", func(t *testing.T) {
		r := router.NewEventRouter(router.EventRouterConfig{})
		ns := r.Namespace("private")
		
		// Set authorization function
		ns.SetAuth(func(ctx context.Context, client router.WSClient) error {
			// Only allow clients with "admin" role
			if client.GetString("role") != "admin" {
				return errors.New("unauthorized")
			}
			return nil
		})
		
		// Try to join with non-admin client
		nonAdminClient := &mockWSClient{
			id:    "user1",
			state: map[string]interface{}{"role": "user"},
		}
		
		err := ns.Join(context.Background(), nonAdminClient)
		if err == nil {
			t.Error("Expected authorization to fail for non-admin")
		}
		
		// Try to join with admin client
		adminClient := &mockWSClient{
			id:    "admin1",
			state: map[string]interface{}{"role": "admin"},
		}
		
		err = ns.Join(context.Background(), adminClient)
		if err != nil {
			t.Errorf("Expected authorization to succeed for admin: %v", err)
		}
	})
}

func TestEventHistory(t *testing.T) {
	t.Run("Add and retrieve events", func(t *testing.T) {
		h := router.NewEventHistory(100, 1*time.Hour)
		
		// Add events
		for i := 0; i < 5; i++ {
			event := &router.EventMessage{
				ID:        generateTestID(),
				Type:      "test",
				Data:      i,
				Timestamp: time.Now(),
			}
			h.Add(event)
		}
		
		// Retrieve all
		events := h.Get(router.EventHistoryFilter{})
		if len(events) != 5 {
			t.Errorf("Expected 5 events, got %d", len(events))
		}
	})
	
	t.Run("Filter by type", func(t *testing.T) {
		h := router.NewEventHistory(100, 1*time.Hour)
		
		// Add different types
		h.Add(&router.EventMessage{Type: "type1", Timestamp: time.Now()})
		h.Add(&router.EventMessage{Type: "type2", Timestamp: time.Now()})
		h.Add(&router.EventMessage{Type: "type1", Timestamp: time.Now()})
		
		// Filter by type1
		events := h.Get(router.EventHistoryFilter{Type: "type1"})
		if len(events) != 2 {
			t.Errorf("Expected 2 events of type1, got %d", len(events))
		}
		
		for _, e := range events {
			if e.Type != "type1" {
				t.Errorf("Expected type1, got %s", e.Type)
			}
		}
	})
	
	t.Run("Max size enforcement", func(t *testing.T) {
		h := router.NewEventHistory(3, 1*time.Hour)
		
		// Add more than max size
		for i := 0; i < 5; i++ {
			h.Add(&router.EventMessage{
				Type:      "test",
				Data:      i,
				Timestamp: time.Now(),
			})
		}
		
		// Should only have 3 most recent
		events := h.Get(router.EventHistoryFilter{})
		if len(events) != 3 {
			t.Errorf("Expected 3 events (max size), got %d", len(events))
		}
		
		// Check that we have the most recent ones (2, 3, 4)
		for i, e := range events {
			expectedData := i + 2
			if e.Data.(int) != expectedData {
				t.Errorf("Expected data %d, got %v", expectedData, e.Data)
			}
		}
	})
	
	t.Run("Time-based filtering", func(t *testing.T) {
		h := router.NewEventHistory(100, 1*time.Hour)
		
		now := time.Now()
		past := now.Add(-1 * time.Hour)
		future := now.Add(1 * time.Hour)
		
		// Add events at different times
		h.Add(&router.EventMessage{Type: "old", Timestamp: past})
		h.Add(&router.EventMessage{Type: "current", Timestamp: now})
		h.Add(&router.EventMessage{Type: "future", Timestamp: future})
		
		// Filter for events after 'now'
		afterNow := now.Add(-1 * time.Second)
		events := h.Get(router.EventHistoryFilter{Since: &afterNow})
		
		// Should get current and future
		if len(events) != 2 {
			t.Errorf("Expected 2 events after now, got %d", len(events))
		}
	})
}

func TestAckManager(t *testing.T) {
	t.Run("Send with acknowledgment", func(t *testing.T) {
		mgr := router.NewAckManager(1 * time.Second)
		mockClient := &mockWSClient{id: "test-client"}
		
		// Simulate acknowledgment in background
		go func() {
			time.Sleep(100 * time.Millisecond)
			ack := &router.EventAck{
				ID:      "test-ack",
				Success: true,
				Data:    "response",
			}
			mgr.HandleAck(ack)
		}()
		
		event := &router.EventMessage{
			Type:  "test",
			AckID: "test-ack",
		}
		
		ctx := context.Background()
		ack, err := mgr.SendWithAck(ctx, mockClient, event, 500*time.Millisecond)
		
		if err != nil {
			t.Fatalf("Failed to get acknowledgment: %v", err)
		}
		
		if !ack.Success {
			t.Error("Expected successful acknowledgment")
		}
		
		if ack.Data != "response" {
			t.Errorf("Expected response data, got %v", ack.Data)
		}
	})
	
	t.Run("Acknowledgment timeout", func(t *testing.T) {
		mgr := router.NewAckManager(100 * time.Millisecond)
		mockClient := &mockWSClient{id: "test-client"}
		
		event := &router.EventMessage{
			Type:  "test",
			AckID: "timeout-test",
		}
		
		ctx := context.Background()
		ack, err := mgr.SendWithAck(ctx, mockClient, event, 100*time.Millisecond)
		
		// The function returns a timeout ack rather than an error
		// Check if we got a timeout acknowledgment (Success=false, Error="acknowledgment timeout")
		if err == nil && ack != nil {
			if ack.Success || ack.Error != "acknowledgment timeout" {
				t.Errorf("Expected timeout acknowledgment, got: Success=%v, Error=%s", ack.Success, ack.Error)
			}
		} else if err != nil {
			// This is also acceptable - context timeout
			if err.Error() != "acknowledgment timeout" {
				t.Errorf("Expected timeout error, got: %v", err)
			}
		} else {
			t.Error("Expected either timeout error or timeout acknowledgment")
		}
	})
	
	t.Run("Callback-based acknowledgment", func(t *testing.T) {
		mgr := router.NewAckManager(1 * time.Second)
		mockClient := &mockWSClient{id: "test-client"}
		
		callbackCalled := false
		var receivedAck *router.EventAck
		
		callback := func(ctx context.Context, ack *router.EventAck) error {
			callbackCalled = true
			receivedAck = ack
			return nil
		}
		
		event := &router.EventMessage{
			Type:  "test",
			AckID: "callback-test",
		}
		
		err := mgr.SendWithCallback(context.Background(), mockClient, event, callback, 500*time.Millisecond)
		if err != nil {
			t.Fatalf("Failed to send with callback: %v", err)
		}
		
		// Simulate acknowledgment
		ack := &router.EventAck{
			ID:      "callback-test",
			Success: true,
		}
		mgr.HandleAck(ack)
		
		// Wait for callback
		time.Sleep(50 * time.Millisecond)
		
		if !callbackCalled {
			t.Error("Callback was not called")
		}
		
		if receivedAck == nil || !receivedAck.Success {
			t.Error("Expected successful acknowledgment in callback")
		}
	})
}

func TestEventBatcher(t *testing.T) {
	t.Run("Batch by size", func(t *testing.T) {
		var batches [][]*router.EventMessage
		mu := sync.Mutex{}
		
		processor := func(batch []*router.EventMessage) {
			mu.Lock()
			batches = append(batches, batch)
			mu.Unlock()
		}
		
		batcher := router.NewEventBatcher(3, 1*time.Second, processor)
		
		// Add 5 events
		for i := 0; i < 5; i++ {
			batcher.Add(&router.EventMessage{
				Type: "test",
				Data: i,
			})
		}
		
		// Wait for processing
		time.Sleep(100 * time.Millisecond)
		
		mu.Lock()
		// Should have 1 batch of 3 (triggered by size)
		// and remaining will be in buffer
		if len(batches) != 1 {
			t.Errorf("Expected 1 batch, got %d", len(batches))
		}
		
		if len(batches[0]) != 3 {
			t.Errorf("Expected batch size 3, got %d", len(batches[0]))
		}
		mu.Unlock()
		
		// Close to flush remaining
		batcher.Close()
		time.Sleep(100 * time.Millisecond)
		
		// Should now have 2 batches total
		mu.Lock()
		if len(batches) != 2 {
			t.Errorf("Expected 2 batches after close, got %d", len(batches))
		}
		mu.Unlock()
	})
	
	t.Run("Batch by time", func(t *testing.T) {
		var batches [][]*router.EventMessage
		mu := sync.Mutex{}
		
		processor := func(batch []*router.EventMessage) {
			mu.Lock()
			batches = append(batches, batch)
			mu.Unlock()
		}
		
		batcher := router.NewEventBatcher(100, 200*time.Millisecond, processor)
		
		// Add 2 events (less than batch size)
		for i := 0; i < 2; i++ {
			batcher.Add(&router.EventMessage{
				Type: "test",
				Data: i,
			})
		}
		
		// Wait for timer to trigger
		time.Sleep(300 * time.Millisecond)
		
		mu.Lock()
		defer mu.Unlock()
		
		// Should have 1 batch triggered by timer
		if len(batches) != 1 {
			t.Errorf("Expected 1 batch, got %d", len(batches))
		}
		
		if len(batches[0]) != 2 {
			t.Errorf("Expected batch size 2, got %d", len(batches[0]))
		}
		
		batcher.Close()
	})
}

func TestEventThrottler(t *testing.T) {
	t.Run("Basic throttling", func(t *testing.T) {
		throttler := router.NewEventThrottler(3, 100*time.Millisecond)
		
		// First 3 should be allowed
		for i := 0; i < 3; i++ {
			if !throttler.Allow("test") {
				t.Errorf("Event %d should be allowed", i+1)
			}
		}
		
		// 4th should be throttled
		if throttler.Allow("test") {
			t.Error("4th event should be throttled")
		}
		
		// Wait for window to reset
		time.Sleep(150 * time.Millisecond)
		
		// Should be allowed again
		if !throttler.Allow("test") {
			t.Error("Event should be allowed after window reset")
		}
	})
	
	t.Run("Different event types", func(t *testing.T) {
		throttler := router.NewEventThrottler(2, 100*time.Millisecond)
		
		// Each type has its own limit
		for i := 0; i < 2; i++ {
			if !throttler.Allow("type1") {
				t.Error("type1 should be allowed")
			}
			if !throttler.Allow("type2") {
				t.Error("type2 should be allowed")
			}
		}
		
		// Both should be throttled now
		if throttler.Allow("type1") {
			t.Error("type1 should be throttled")
		}
		if throttler.Allow("type2") {
			t.Error("type2 should be throttled")
		}
	})
}

// Helper function
func generateTestID() string {
	return time.Now().Format("20060102150405.999999999")
}

// Mock implementations
type mockWSClient struct {
	id    string
	state map[string]interface{}
	mu    sync.RWMutex
}

func (m *mockWSClient) ID() string                        { return m.id }
func (m *mockWSClient) ConnectionID() string              { return m.id }
func (m *mockWSClient) Context() context.Context          { return context.Background() }
func (m *mockWSClient) SetContext(ctx context.Context)    {}
func (m *mockWSClient) OnMessage(handler router.MessageHandler) error { return nil }
func (m *mockWSClient) OnJSON(event string, handler router.JSONHandler) error { return nil }
func (m *mockWSClient) Send(data []byte) error            { return nil }
func (m *mockWSClient) SendJSON(v interface{}) error {
	// For testing ack manager
	_, err := json.Marshal(v)
	return err
}
func (m *mockWSClient) SendWithContext(ctx context.Context, data []byte) error { return nil }
func (m *mockWSClient) SendJSONWithContext(ctx context.Context, v interface{}) error { 
	return m.SendJSON(v)
}
func (m *mockWSClient) Broadcast(data []byte) error       { return nil }
func (m *mockWSClient) BroadcastJSON(v interface{}) error { return nil }
func (m *mockWSClient) BroadcastWithContext(ctx context.Context, data []byte) error { return nil }
func (m *mockWSClient) BroadcastJSONWithContext(ctx context.Context, v interface{}) error { return nil }
func (m *mockWSClient) Join(room string) error            { return nil }
func (m *mockWSClient) JoinWithContext(ctx context.Context, room string) error { return nil }
func (m *mockWSClient) Leave(room string) error           { return nil }
func (m *mockWSClient) LeaveWithContext(ctx context.Context, room string) error { return nil }
func (m *mockWSClient) Room(name string) router.RoomBroadcaster { return nil }
func (m *mockWSClient) Rooms() []string                   { return nil }
func (m *mockWSClient) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = make(map[string]interface{})
	}
	m.state[key] = value
}
func (m *mockWSClient) SetWithContext(ctx context.Context, key string, value interface{}) {
	m.Set(key, value)
}
func (m *mockWSClient) Get(key string) interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state == nil {
		return nil
	}
	return m.state[key]
}
func (m *mockWSClient) GetString(key string) string {
	v := m.Get(key)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func (m *mockWSClient) GetInt(key string) int {
	v := m.Get(key)
	if i, ok := v.(int); ok {
		return i
	}
	return 0
}
func (m *mockWSClient) GetBool(key string) bool {
	v := m.Get(key)
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
func (m *mockWSClient) Close(code int, reason string) error { return nil }
func (m *mockWSClient) CloseWithContext(ctx context.Context, code int, reason string) error { return nil }
func (m *mockWSClient) IsConnected() bool                 { return true }
func (m *mockWSClient) Query(key string, defaultValue ...string) string {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}
func (m *mockWSClient) Emit(event string, data interface{}) error { return nil }
func (m *mockWSClient) EmitWithContext(ctx context.Context, event string, data interface{}) error { return nil }
func (m *mockWSClient) Conn() router.WebSocketContext     { return nil }