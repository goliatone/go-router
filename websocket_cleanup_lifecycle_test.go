package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestCleanupOwnersClose(t *testing.T) {
	owners := map[string]func() any{
		"history":          func() any { return NewEventHistory(10, time.Hour) },
		"history-zero-ttl": func() any { return NewEventHistory(10, 0) },
		"reconnect":        func() any { return NewReconnectManager(DefaultReconnectConfig()) },
		"router":           func() any { return NewEventRouter(EventRouterConfig{EnableHistory: true}) },
		"router-disabled":  func() any { return NewEventRouter(EventRouterConfig{}) },
		"zero-history":     func() any { return &EventHistory{} },
		"zero-reconnect":   func() any { return &ReconnectManager{} },
		"zero-router":      func() any { return &EventRouter{} },
	}
	for name, newOwner := range owners {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				for range 20 {
					owner, ok := newOwner().(interface{ Close() })
					if !ok {
						t.Fatal("cleanup owner has no Close method")
					}
					var callers sync.WaitGroup
					for range 4 {
						callers.Go(owner.Close)
					}
					callers.Wait()
					owner.Close()
				}
				if elapsed := time.Since(start); elapsed != 0 {
					t.Fatalf("idle close waited for a cleanup tick: %v", elapsed)
				}
				// The bubble also requires every constructed worker to exit.
			})
		})
	}
}

func TestPeriodicCleanupCloseJoinsSweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var sweeps atomic.Int32
		worker := newPeriodicCleanup(time.Second, func() {
			sweeps.Add(1)
			close(entered)
			<-release
		})
		<-entered
		returned := make(chan struct{}, 8)
		var callers sync.WaitGroup
		for range 8 {
			callers.Go(func() { worker.close(); returned <- struct{}{} })
		}
		<-worker.stop
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("close returned while the cleanup sweep was blocked")
		default:
		}
		// Queue another tick while the sweep is blocked. Cancellation must win
		// when the sweep exits, without starting another cleanup callback.
		time.Sleep(2 * time.Second)
		unblock()
		callers.Wait()
		requireCleanupStopped(t, worker)
		time.Sleep(10 * time.Second)
		if sweeps.Load() != 1 {
			t.Fatalf("cleanup ran %d times, want one in-flight sweep", sweeps.Load())
		}
	})
}

func requireCleanupStopped(t *testing.T, worker *periodicCleanup) {
	t.Helper()
	if worker == nil {
		return
	}
	select {
	case <-worker.done:
	default:
		t.Fatal("close returned before the cleanup worker exited")
	}
}

func TestEventHistoryCleanupLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(10, 90*time.Second)
		defer history.Close()
		old := &EventMessage{Type: "old", Timestamp: time.Now().Add(-2 * time.Minute)}
		fresh := &EventMessage{Type: "fresh", Timestamp: time.Now()}
		history.Add(old)
		history.Add(fresh)
		synctest.Wait() // register the first tick before advancing virtual time
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := history.Get(EventHistoryFilter{}); len(got) != 1 || got[0] != fresh {
			t.Fatalf("periodic expiry did not retain only the fresh event: %v", got)
		}
		if got := history.Get(EventHistoryFilter{Type: "old"}); len(got) != 0 {
			t.Fatal("expired event retained in index")
		}
		history.Close()
		requireCleanupStopped(t, history.cleanup)
		history.Add(&EventMessage{Type: "later", Timestamp: time.Now()})
		time.Sleep(2 * time.Hour)
		if history.Size() != 2 || len(history.Get(EventHistoryFilter{Type: "fresh"})) != 1 {
			t.Fatal("closed history still expired events or discarded stored data")
		}
		history.Clear()
		if history.Size() != 0 {
			t.Fatal("manual clear stopped working after close")
		}
		withoutTTL := NewEventHistory(10, 0)
		defer withoutTTL.Close()
		if withoutTTL.cleanup != nil {
			t.Fatal("zero-TTL history started a cleanup worker")
		}
	})
}

type lifecycleClient struct {
	WSClient
	sendJSON func(any) error
}

func (c lifecycleClient) ID() string { return "lifecycle" }
func (c lifecycleClient) SendJSON(v any) error {
	if c.sendJSON != nil {
		return c.sendJSON(v)
	}
	return nil
}

func TestReconnectManagerCleanupLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		config := DefaultReconnectConfig()
		config.SessionTimeout = 30 * time.Second
		manager := NewReconnectManager(config)
		defer manager.Close()
		client := lifecycleClient{}
		expired, err := manager.CreateSession(client)
		if err != nil {
			t.Fatal(err)
		}
		active, err := manager.CreateSession(client)
		if err != nil {
			t.Fatal(err)
		}
		manager.HandleDisconnect(expired.ID)
		synctest.Wait()
		time.Sleep(time.Minute)
		synctest.Wait()
		manager.sessionsMu.RLock()
		_, expiredExists := manager.sessions[expired.ID]
		_, activeExists := manager.sessions[active.ID]
		manager.sessionsMu.RUnlock()
		if expiredExists || !activeExists {
			t.Fatal("periodic cleanup failed to remove only the expired session")
		}
		manager.Close()
		requireCleanupStopped(t, manager.cleanup)
		manager.HandleDisconnect(active.ID)
		time.Sleep(2 * time.Minute)
		if queueErr := manager.QueueMessage(active.ID, &QueuedMessage{Type: "retained"}); queueErr != nil {
			t.Fatalf("closed manager lost its retained session: %v", queueErr)
		}
		if _, reconnectErr := manager.HandleReconnect(t.Context(), client, active.ID, active.ReconnectToken); reconnectErr == nil {
			t.Fatal("explicit reconnect stopped enforcing session expiry after close")
		}
		created, err := manager.CreateSession(client)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.HandleReconnect(t.Context(), client, created.ID, created.ReconnectToken); err != nil {
			t.Fatal(err)
		}
		manager.RemoveSession(created.ID)
	})
}

func TestCleanupCloseFromCallbacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(10, time.Hour)
		defer history.Close()
		history.Add(&EventMessage{Type: "replay", Timestamp: time.Now()})
		if err := history.Replay(lifecycleClient{sendJSON: func(any) error {
			history.Close()
			return nil
		}}, EventHistoryFilter{}); err != nil {
			t.Fatal(err)
		}
		manager := NewReconnectManager(DefaultReconnectConfig())
		defer manager.Close()
		client := lifecycleClient{sendJSON: func(any) error { manager.Close(); return nil }}
		session, err := manager.CreateSession(client)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.HandleReconnect(t.Context(), client, session.ID, session.ReconnectToken); err != nil {
			t.Fatal(err)
		}
		events := NewEventRouter(EventRouterConfig{EnableHistory: true})
		defer events.Close()
		if err := events.On("stop", &GenericEventHandler{Handler: func(context.Context, WSClient, *EventMessage) error {
			events.Close()
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := events.RouteEvent(t.Context(), client, &EventMessage{Type: "stop"}); err != nil {
				t.Fatal(err)
			}
		}
		requireCleanupStopped(t, history.cleanup)
		requireCleanupStopped(t, manager.cleanup)
		requireCleanupStopped(t, events.history.cleanup)
		time.Sleep(2 * time.Hour)
		if len(events.GetHistory(EventHistoryFilter{})) != 2 {
			t.Fatal("router close discarded history or left expiry running")
		}
	})
}

func TestCleanupCloseConcurrentOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		history := NewEventHistory(1000, time.Hour)
		manager := NewReconnectManager(DefaultReconnectConfig())
		defer history.Close()
		defer manager.Close()
		start := make(chan struct{})
		var workers sync.WaitGroup
		for range 8 {
			workers.Go(func() {
				<-start
				for range 20 {
					history.Add(&EventMessage{Type: "concurrent", Timestamp: time.Now()})
					history.Get(EventHistoryFilter{Type: "concurrent"})
					session, err := manager.CreateSession(lifecycleClient{})
					if err != nil {
						t.Error(err)
						return
					}
					manager.HandleDisconnect(session.ID)
					manager.RemoveSession(session.ID)
				}
			})
		}
		workers.Go(func() { <-start; history.Close(); manager.Close() })
		close(start)
		workers.Wait()
		requireCleanupStopped(t, history.cleanup)
		requireCleanupStopped(t, manager.cleanup)
		if history.Size() != 160 {
			t.Fatal("concurrent close lost stored events")
		}
	})
}
