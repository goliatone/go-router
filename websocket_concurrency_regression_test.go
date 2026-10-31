package router

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type concurrencyClient struct {
	WSClient
	sendJSON func(any) error
	close    func() error
}

func (c concurrencyClient) ID() string           { return "concurrency" }
func (c concurrencyClient) SendJSON(v any) error { return c.sendJSON(v) }
func (c concurrencyClient) Close(int, string) error {
	return c.close()
}

func TestReconnectConcurrentDisconnect(t *testing.T) {
	session := &ClientSession{ID: "session", ReconnectToken: "token", DisconnectTime: time.Now()}
	m := &ReconnectManager{
		sessions: map[string]*ClientSession{session.ID: session},
		config:   ReconnectConfig{SessionTimeout: time.Hour},
	}
	client := concurrencyClient{sendJSON: func(any) error { return nil }}
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			m.HandleDisconnect(session.ID)
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			if _, err := m.HandleReconnect(t.Context(), client, session.ID, "token"); err != nil {
				t.Errorf("reconnect removed a live session: %v", err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for range 1000 {
			m.cleanupExpiredSessions(time.Now())
		}
	}()
	close(start)
	workers.Wait()
}

func TestReconnectCleanupDuringSend(t *testing.T) {
	session := &ClientSession{ID: "session", ReconnectToken: "token", DisconnectTime: time.Now()}
	m := &ReconnectManager{
		sessions: map[string]*ClientSession{session.ID: session},
		config:   ReconnectConfig{SessionTimeout: time.Hour},
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	client := concurrencyClient{sendJSON: func(any) error {
		close(entered)
		<-release
		return nil
	}}
	result := make(chan error, 1)
	go func() {
		_, err := m.HandleReconnect(t.Context(), client, session.ID, "token")
		result <- err
	}()
	initializationReceive(t, entered)
	// The reconnect transition has finished, but its network callback is still
	// blocked. Cleanup must see active state and must not wait for network I/O.
	cleaned := make(chan struct{})
	go func() {
		m.cleanupExpiredSessions(time.Now().Add(2 * time.Hour))
		close(cleaned)
	}()
	initializationReceive(t, cleaned)
	retry := concurrencyClient{sendJSON: func(any) error {
		// Reentry must also be safe on the reconnect caller's own goroutine.
		m.HandleDisconnect(session.ID)
		return nil
	}}
	retried := make(chan error, 1)
	go func() {
		_, err := m.HandleReconnect(t.Context(), retry, session.ID, "token")
		retried <- err
	}()
	if err := initializationReceive(t, retried); err != nil {
		t.Fatalf("cleanup removed a restored session: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := initializationReceive(t, result); err != nil {
		t.Fatal(err)
	}
	// Once disconnected, the same sweep must actually remove an expired entry.
	m.cleanupExpiredSessions(time.Now().Add(2 * time.Hour))
	if _, err := m.HandleReconnect(t.Context(), retry, session.ID, "token"); err == nil {
		t.Fatal("expired session survived cleanup")
	}
}

func TestAckRetrySurvivesOldSendFailure(t *testing.T) {
	for _, synchronous := range []bool{false, true} {
		name := "callback"
		if synchronous {
			name = "synchronous"
		}
		t.Run(name, func(t *testing.T) {
			m := NewAckManager(time.Hour)
			defer m.CancelAck("retry")
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			failed := errors.New("old send failed")
			oldClient := concurrencyClient{sendJSON: func(any) error {
				close(entered)
				<-release
				return failed
			}}
			oldResult := make(chan error, 1)
			go func() {
				event := &EventMessage{AckID: "retry"}
				if synchronous {
					_, err := m.SendWithAck(t.Context(), oldClient, event, time.Hour)
					oldResult <- err
				} else {
					oldResult <- m.SendWithCallback(t.Context(), oldClient, event, nil, time.Hour)
				}
			}()
			initializationReceive(t, entered)
			m.pendingMu.RLock()
			old := m.pending["retry"]
			m.pendingMu.RUnlock()
			// Also clean up on a regression that loses the old attempt's resources.
			defer old.timer.Stop()
			defer old.cancel()
			received := make(chan *EventAck, 1)
			newClient := concurrencyClient{sendJSON: func(any) error { return nil }}
			if err := m.SendWithCallback(t.Context(), newClient, &EventMessage{AckID: "retry"}, func(_ context.Context, ack *EventAck) error {
				received <- ack
				return nil
			}, time.Hour); err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			if err := initializationReceive(t, oldResult); !errors.Is(err, failed) {
				t.Fatalf("old send: %v", err)
			}
			if m.PendingCount() != 1 {
				t.Fatal("old send failure removed the replacement ACK")
			}
			if old.ctx.Err() == nil || old.timer.Stop() {
				t.Error("replaced attempt retained its context or timer")
			}
			ack := &EventAck{ID: "retry", Success: true}
			if err := m.HandleAck(ack); err != nil {
				t.Fatal(err)
			}
			if got := initializationReceive(t, received); got != ack {
				t.Fatalf("replacement received %v", got)
			}
		})
	}
}

func TestWSHubCloseAllowsReentryAndRejectsAdmission(t *testing.T) {
	hub := NewWSHub()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var calls atomic.Int32
	hub.clients["blocked"] = concurrencyClient{close: func() error {
		calls.Add(1)
		// These are synchronous callbacks on the Close caller's goroutine.
		if got := hub.ClientCount(); got != 0 {
			t.Errorf("shutdown retained %d clients", got)
		}
		if len(hub.Clients()) != 0 {
			t.Error("shutdown client snapshot is not empty")
		}
		if err := hub.Close(); err != nil {
			t.Errorf("reentrant close: %v", err)
		}
		close(entered)
		<-release
		return errors.New("close failure must not stop the remaining clients")
	}}
	for _, id := range []string{"second", "third"} {
		hub.clients[id] = concurrencyClient{close: func() error {
			calls.Add(1)
			return nil
		}}
	}
	closed := make(chan error, 1)
	go func() { closed <- hub.Close() }()
	initializationReceive(t, entered)
	// A concurrent Close must not duplicate calls or wait on a callback that
	// could itself be waiting on this caller.
	repeated := make(chan error, 1)
	go func() { repeated <- hub.Close() }()
	if err := initializationReceive(t, repeated); err != nil {
		t.Fatal(err)
	}
	socket := newInitializationSocket()
	admitted := make(chan error, 1)
	go func() { admitted <- hub.Handler()(socket) }()
	if err := initializationReceive(t, admitted); err != nil {
		t.Fatal(err)
	}
	initializationReceive(t, socket.closed)
	if hub.ClientCount() != 0 {
		t.Error("admitted client after shutdown began")
	}
	releaseOnce.Do(func() { close(release) })
	if err := initializationReceive(t, closed); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("closed %d clients, want exactly 3", got)
	}
}

func TestAckRetryIgnoresStaleTimerAndCancellation(t *testing.T) {
	m := NewAckManager(time.Hour)
	defer m.CancelAck("retry")
	client := concurrencyClient{sendJSON: func(any) error { return nil }}
	if err := m.SendWithCallback(t.Context(), client, &EventMessage{AckID: "retry"}, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	m.pendingMu.RLock()
	old := m.pending["retry"]
	m.pendingMu.RUnlock()
	// Model a timer callback already dispatched when Stop runs. Hold its
	// attempt-local work until the replacement has been published.
	release, finished := make(chan struct{}), make(chan struct{})
	go func() {
		<-release
		m.timeoutAck("retry", old)
		m.cleanupAck("retry", old)
		close(finished)
	}()
	received := make(chan *EventAck, 1)
	if err := m.SendWithCallback(t.Context(), client, &EventMessage{AckID: "retry"}, func(_ context.Context, ack *EventAck) error {
		received <- ack
		return nil
	}, time.Hour); err != nil {
		close(release)
		initializationReceive(t, finished)
		t.Fatal(err)
	}
	close(release)
	initializationReceive(t, finished)
	if m.PendingCount() != 1 {
		t.Fatal("stale timeout/cancellation removed the retry")
	}
	ack := &EventAck{ID: "retry", Success: true}
	if err := m.HandleAck(ack); err != nil {
		t.Fatal(err)
	}
	if got := initializationReceive(t, received); got != ack {
		t.Fatalf("replacement received stale result: %v", got)
	}
}

func TestAckReplacementReleasesSynchronousWaiter(t *testing.T) {
	m := NewAckManager(time.Hour)
	defer m.CancelAck("retry")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	client := concurrencyClient{sendJSON: func(any) error {
		close(entered)
		<-release
		return nil
	}}
	result := make(chan error, 1)
	go func() {
		_, err := m.SendWithAck(t.Context(), client, &EventMessage{AckID: "retry"}, time.Hour)
		result <- err
	}()
	initializationReceive(t, entered)
	newClient := concurrencyClient{sendJSON: func(any) error { return nil }}
	if err := m.SendWithCallback(t.Context(), newClient, &EventMessage{AckID: "retry"}, nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := initializationReceive(t, result); err == nil {
		t.Fatal("superseded waiter returned a nil successful acknowledgment")
	}
	if m.PendingCount() != 1 {
		t.Fatal("superseded waiter's cleanup removed the retry")
	}
	m.CancelAck("retry")
	if m.PendingCount() != 0 {
		t.Fatal("explicit CancelAck did not cancel the current attempt")
	}
}

func TestAckImmediateTimeoutCompletesPublishedAttempt(t *testing.T) {
	m := NewAckManager(time.Hour)
	received := make(chan *EventAck, 1)
	client := concurrencyClient{sendJSON: func(any) error { return nil }}
	if err := m.SendWithCallback(t.Context(), client, &EventMessage{AckID: "immediate"}, func(_ context.Context, ack *EventAck) error {
		received <- ack
		return nil
	}, -time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	ack := initializationReceive(t, received)
	if ack.ID != "immediate" || ack.Success || ack.Error != "acknowledgment timeout" {
		t.Fatalf("unexpected timeout: %+v", ack)
	}
	if m.PendingCount() != 0 {
		t.Fatal("timeout ran before its attempt was published")
	}
}
