package router

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

type initializationSocket struct {
	WebSocketContext
	reads    chan struct{}
	messages chan []byte
	closed   chan struct{}
	once     sync.Once
}

func newInitializationSocket() *initializationSocket {
	return &initializationSocket{reads: make(chan struct{}, 64), messages: make(chan []byte, 64), closed: make(chan struct{})}
}

func (*initializationSocket) ConnectionID() string { return "initialization" }
func (s *initializationSocket) ReadMessage() (int, []byte, error) {
	s.reads <- struct{}{}
	select {
	case data := <-s.messages:
		return TextMessage, data, nil
	case <-s.closed:
		return 0, nil, io.EOF
	}
}
func (*initializationSocket) WriteMessage(int, []byte) error { return nil }
func (s *initializationSocket) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func initializationReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for client initialization or cleanup")
		var zero T
		return zero
	}
}

func TestWSHubInitializesBeforeReading(t *testing.T) {
	hub := NewWSHub()
	defer hub.Close()
	socket := newInitializationSocket()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	received := make(chan string, 2)
	if err := hub.OnConnect(func(ctx context.Context, client WSClient, _ any) error {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		client.Set("ready", "initialized")
		return client.OnMessage(func(context.Context, []byte) error {
			received <- client.GetString("ready")
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- hub.Handler()(socket) }()
	defer func() {
		_ = hub.Close()
		initializationReceive(t, done)
	}()
	initializationReceive(t, entered)
	socket.messages <- []byte("early")
	select {
	case <-socket.reads:
		t.Fatal("read pump started before connection initialization completed")
	case <-time.After(50 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	if got := initializationReceive(t, received); got != "initialized" {
		t.Fatalf("first message observed incomplete state: %q", got)
	}
}

func TestWSClientConcurrentHandlerRegistration(t *testing.T) {
	socket := newInitializationSocket()
	client := NewWSClient(socket, nil)
	defer func() {
		_ = client.Close(CloseNormalClosure, "test cleanup")
		initializationReceive(t, client.(*wsClient).writeDone)
	}()
	processed := make(chan struct{}, 32)
	if err := client.OnMessage(func(context.Context, []byte) error {
		// Registration from a callback must not deadlock dispatch.
		return client.OnMessage(func(context.Context, []byte) error { return nil })
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.OnJSON("message", func(context.Context, json.RawMessage) error {
		if err := client.OnJSON("message", func(context.Context, json.RawMessage) error { return nil }); err != nil {
			return err
		}
		processed <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for i := range 100 {
				client.SetContext(context.WithValue(context.Background(), initializationContextKey{}, i))
				_ = client.OnMessage(func(context.Context, []byte) error { return nil })
				_ = client.OnJSON(fmt.Sprint(i), func(context.Context, json.RawMessage) error { return nil })
				_ = client.OnJSON("message", func(context.Context, json.RawMessage) error { return nil })
			}
		})
	}
	for range 32 {
		socket.messages <- []byte(`{"type":"message","data":"payload"}`)
		initializationReceive(t, processed)
	}
	workers.Wait()
}

type initializationContextKey struct{}

func TestWSHubInitializationClosure(t *testing.T) {
	for _, mode := range []string{"client", "hub", "already-closed", "cancel-initializer"} {
		t.Run(mode, func(t *testing.T) {
			hub := NewWSHub()
			defer hub.Close()
			socket := newInitializationSocket()
			entered := make(chan struct{}, 1)
			if err := hub.OnConnect(func(ctx context.Context, client WSClient, _ any) error {
				entered <- struct{}{}
				switch mode {
				case "client":
					return client.Close(CloseNormalClosure, "initializer closed")
				case "hub":
					return hub.Close()
				case "cancel-initializer":
					<-ctx.Done()
					return ctx.Err()
				default:
					t.Error("closed hub admitted a connection")
					return nil
				}
			}); err != nil {
				t.Fatal(err)
			}
			_ = hub.OnConnect(func(context.Context, WSClient, any) error {
				t.Error("initializer ran after closure")
				return nil
			})
			if mode == "already-closed" {
				_ = hub.Close()
			}
			done := make(chan error, 1)
			go func() { done <- hub.Handler()(socket) }()
			if mode == "cancel-initializer" {
				initializationReceive(t, entered)
				_ = hub.Close()
			}
			if err := initializationReceive(t, done); err != nil {
				t.Fatal(err)
			}
			select {
			case <-socket.reads:
				t.Fatal("closed client started reading")
			default:
			}
		})
	}
}
