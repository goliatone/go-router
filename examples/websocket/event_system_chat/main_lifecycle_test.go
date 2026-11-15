//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Run with an explicit file list because this standalone demo is build-ignored.
func TestEventChatProcessHelper(t *testing.T) {
	if os.Getenv("H041_CHILD") == "1" {
		main()
		os.Exit(0)
	}
}

func eventChatAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func eventChatReady(t *testing.T, address string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	t.Cleanup(client.CloseIdleConnections)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get("http://" + address + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("host did not become ready")
}

func eventChatSocket(t *testing.T, address, path string) *websocket.Conn {
	t.Helper()
	c, response, err := websocket.DefaultDialer.Dial("ws://"+address+path, nil)
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func eventChatClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		var closed *websocket.CloseError
		if !errors.As(err, &closed) || (closed.Code != 1000 && closed.Code != 1001) {
			t.Fatalf("unclean websocket closure: %v", err)
		}
		return
	}
}

func eventChatReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("owned work did not finish")
	}
	var zero T
	return zero
}

func serveEventChat(t *testing.T, h *eventChatHost) (string, <-chan error) {
	t.Helper()
	address := eventChatAddress(t)
	served := make(chan error, 1)
	go func() { served <- h.Serve(address) }()
	eventChatReady(t, address)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := eventChatReceive(t, served); !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
	})
	return address, served
}
func eventChatRead(t *testing.T, c *websocket.Conn, kind string) map[string]any {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 1000; i++ {
		var msg map[string]any
		if err := c.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		if msg["type"] == kind {
			return msg
		}
	}
	t.Fatalf("missing event %s", kind)
	return nil
}
func eventChatWelcome(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	msg := eventChatRead(t, c, "welcome")
	id, ok := msg["id"].(string)
	if !ok || len(id) < 8 || msg["message"] != "Welcome to the event-driven chat!" {
		t.Fatalf("welcome: %v", msg)
	}
	ready := eventChatRead(t, c, "handler_ready")
	if ready["connection_id"] != id[:8] {
		t.Fatalf("ready: %v", ready)
	}
	return id
}
func eventChatRoundtrip(t *testing.T, c *websocket.Conn, data string) {
	t.Helper()
	if err := c.WriteJSON(map[string]string{"type": "message", "message": data}); err != nil {
		t.Fatal(err)
	}
	msg := eventChatRead(t, c, "message_sent")
	if msg["message"] != data || msg["user"] == "" || msg["timestamp"] == nil {
		t.Fatalf("confirmation: %v", msg)
	}
}

func assertEventChatStopped(t *testing.T, h *eventChatHost) {
	if n := len(h.clientSnapshot()); n != 0 {
		t.Fatalf("retained chat clients: %d", n)
	}
	t.Helper()
	eventChatReceive(t, h.stopped)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.stopping || len(h.active) != 0 {
		t.Fatalf("stopping=%t active=%d", h.stopping, len(h.active))
	}
}
func TestEventChatProtocol(t *testing.T) {
	h := newEventChatHost()
	addr, _ := serveEventChat(t, h)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/", "/health"} {
		res, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("HTTP %s: %v %d", path, err, res.StatusCode)
		}
		if path == "/" && !strings.Contains(string(body), "location.host") {
			t.Fatal("page origin missing")
		}
		if path == "/health" {
			var health map[string]string
			if err := json.Unmarshal(body, &health); err != nil || health["status"] != "healthy" {
				t.Fatalf("health: %s %v", body, err)
			}
		}
	}
	a := eventChatSocket(t, addr, "/ws/chat")
	id := eventChatWelcome(t, a)
	b := eventChatSocket(t, addr, "/ws/chat")
	eventChatWelcome(t, b)
	if err := a.WriteJSON(map[string]string{"type": "get_users"}); err != nil {
		t.Fatal(err)
	}
	users := eventChatRead(t, a, "users_list")
	if users["count"] != float64(2) || len(users["users"].([]any)) != 2 {
		t.Fatalf("users: %v", users)
	}
	eventChatRoundtrip(t, a, "hello λ")
	msg := eventChatRead(t, b, "message")
	if msg["message"] != "hello λ" || msg["user"] != id[:8] {
		t.Fatalf("broadcast: %v", msg)
	}
	if err := a.WriteJSON(map[string]string{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	pong := eventChatRead(t, a, "pong")
	if pong["timestamp"] == nil {
		t.Fatal(pong)
	}
	if err := b.WriteJSON(map[string]string{"type": "close"}); err != nil {
		t.Fatal(err)
	}
	_ = b.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := b.ReadMessage()
	var closed *websocket.CloseError
	if !errors.As(err, &closed) || closed.Code != 1000 || closed.Text != "Goodbye!" {
		t.Fatalf("close: %v", err)
	}
	// A close frame may precede the handler defer; wait for observable membership.
	eventChatUsers(t, a, 1)
	c := eventChatSocket(t, addr, "/ws/chat")
	eventChatWelcome(t, c)
	c.Close()
	eventChatUsers(t, a, 1)
	invalid := eventChatSocket(t, addr, "/ws/chat")
	eventChatWelcome(t, invalid)
	if err := invalid.WriteMessage(websocket.TextMessage, []byte("{")); err != nil {
		t.Fatal(err)
	}
	eventChatClosed(t, invalid)
	eventChatUsers(t, a, 1)
	if err := a.WriteJSON(map[string]any{"type": 42}); err != nil {
		t.Fatal(err)
	}
	eventChatRoundtrip(t, a, "after-invalid-event")
}
func eventChatUsers(t *testing.T, c *websocket.Conn, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := c.WriteJSON(map[string]string{"type": "get_users"}); err != nil {
			t.Fatal(err)
		}
		msg := eventChatRead(t, c, "users_list")
		if msg["count"] == float64(want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("membership did not reach %d", want)
}

func TestEventChatSignals(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			addr := eventChatAddress(t)
			log, err := os.CreateTemp(t.TempDir(), "host")
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestEventChatProcessHelper$")
			cmd.Env = append(os.Environ(), "H041_CHILD=1", "EVENT_CHAT_ADDR="+addr)
			cmd.Stdout = log
			cmd.Stderr = log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() { done <- cmd.Wait(); close(finished) }()
			t.Cleanup(func() {
				select {
				case <-finished:
				default:
					_ = cmd.Process.Kill()
					<-finished
				}
			})
			eventChatReady(t, addr)
			c := eventChatSocket(t, addr, "/ws/chat")
			eventChatWelcome(t, c)
			eventChatRoundtrip(t, c, "before-signal")
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			eventChatClosed(t, c)
			if err := eventChatReceive(t, done); err != nil {
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("exit: %v\n%s", err, data)
			}
			data, err := os.ReadFile(log.Name())
			if err != nil || strings.Contains(string(data), "DATA RACE") {
				t.Fatalf("host log: %v\n%s", err, data)
			}
		})
	}
}
func TestEventChatConcurrentClients(t *testing.T) {
	h := newEventChatHost()
	addr, _ := serveEventChat(t, h)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := eventChatSocket(t, addr, "/ws/chat")
			defer c.Close()
			eventChatWelcome(t, c)
			for j := range 10 {
				eventChatRoundtrip(t, c, fmt.Sprintf("client-%d-%d", i, j))
			}
			if err := c.WriteJSON(map[string]string{"type": "close"}); err != nil {
				t.Error(err)
			}
			eventChatClosed(t, c)
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	assertEventChatStopped(t, h)
}
func TestEventChatHTTPDrainAndRetry(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			h := newEventChatHost()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			h.app.Router().Get("/held", func(c router.Context) error { close(entered); <-release; return c.SendString("complete") })
			addr, _ := serveEventChat(t, h)
			c := eventChatSocket(t, addr, "/ws/chat")
			eventChatWelcome(t, c)
			response := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: 4 * time.Second}
				defer client.CloseIdleConnections()
				res, err := client.Get("http://" + addr + "/held")
				if err == nil {
					var body []byte
					body, err = io.ReadAll(res.Body)
					res.Body.Close()
					if err == nil && (res.StatusCode != 200 || string(body) != "complete") {
						err = fmt.Errorf("response: %d %q", res.StatusCode, body)
					}
				}
				response <- err
			}()
			eventChatReceive(t, entered)
			budget := 3 * time.Second
			if deadline {
				budget = 40 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- h.Shutdown(ctx) }()
			if deadline {
				if err := eventChatReceive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline: %v", err)
				}
			} else {
				select {
				case err := <-stopped:
					t.Fatalf("returned before drain: %v", err)
				case <-time.After(40 * time.Millisecond):
				}
			}
			eventChatClosed(t, c)
			unblock()
			if err := eventChatReceive(t, response); err != nil {
				t.Fatal(err)
			}
			if !deadline {
				if err := eventChatReceive(t, stopped); err != nil {
					t.Fatal(err)
				}
			}
			retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
			defer cancelRetry()
			if err := h.Shutdown(retry); err != nil {
				t.Fatal(err)
			}
			assertEventChatStopped(t, h)
		})
	}
}
func TestEventChatHeldWorker(t *testing.T) {
	h := newEventChatHost()
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.app.Router().WebSocket("/held", router.DefaultWebSocketConfig(), h.track(func(router.WebSocketContext) error { close(entered); <-release; return nil }))
	addr, _ := serveEventChat(t, h)
	c := eventChatSocket(t, addr, "/held")
	eventChatReceive(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	select {
	case <-h.stopped:
		t.Fatal("completed with held worker")
	default:
	}
	unblock()
	retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := h.Shutdown(retry); err != nil {
		t.Fatal(err)
	}
	eventChatClosed(t, c)
	assertEventChatStopped(t, h)
}
func TestEventChatCloseDuringStop(t *testing.T) {
	for _, peer := range []bool{false, true} {
		t.Run(fmt.Sprintf("peer=%t", peer), func(t *testing.T) {
			h := newEventChatHost()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			cfg := router.DefaultWebSocketConfig()
			cfg.OnDisconnect = func(router.WebSocketContext, error) { close(entered); <-release }
			h.app.Router().WebSocket("/closing", cfg, h.track(func(ws router.WebSocketContext) error {
				_, _, _ = ws.ReadMessage()
				return ws.CloseWithStatus(router.CloseNormalClosure, "done")
			}))
			addr, _ := serveEventChat(t, h)
			c := eventChatSocket(t, addr, "/closing")
			if peer {
				c.Close()
			} else {
				if err := c.WriteJSON(map[string]string{"type": "close"}); err != nil {
					t.Fatal(err)
				}
			}
			eventChatReceive(t, entered)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("held close: %v", err)
			}
			select {
			case <-h.stopped:
				t.Fatal("completed before close callback")
			default:
			}
			unblock()
			retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
			defer cancelRetry()
			if err := h.Shutdown(retry); err != nil {
				t.Fatal(err)
			}
			if !peer {
				eventChatClosed(t, c)
			}
			assertEventChatStopped(t, h)
		})
	}
}
func TestEventChatUpgradeDuringStop(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(fmt.Sprintf("connected=%t", connected), func(t *testing.T) {
			h := newEventChatHost()
			entered, release := make(chan struct{}), make(chan struct{})
			closing, releaseClose := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			unblockClose := sync.OnceFunc(func() { close(releaseClose) })
			defer unblockClose()
			cfg := router.DefaultWebSocketConfig()
			if connected {
				cfg.OnConnect = func(router.WebSocketContext) error { close(entered); <-release; return nil }
			} else {
				cfg.OnPreUpgrade = func(router.Context) (router.UpgradeData, error) { close(entered); <-release; return nil, nil }
			}
			cfg.OnDisconnect = func(router.WebSocketContext, error) { close(closing); <-releaseClose }
			h.app.Router().WebSocket("/late", cfg, h.track(func(router.WebSocketContext) error { t.Error("admitted after seal"); return nil }))
			addr, _ := serveEventChat(t, h)
			dialed := make(chan *websocket.Conn, 1)
			go func() { dialed <- eventChatSocket(t, addr, "/late") }()
			eventChatReceive(t, entered)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- h.Shutdown(ctx) }()
			end := time.Now().Add(time.Second)
			for {
				h.mu.Lock()
				stopping := h.stopping
				h.mu.Unlock()
				if stopping {
					break
				}
				if time.Now().After(end) {
					t.Fatal("stop not latched")
				}
				time.Sleep(time.Millisecond)
			}
			unblock()
			c := eventChatReceive(t, dialed)
			eventChatReceive(t, closing)
			if err := eventChatReceive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("held upgrade close: %v", err)
			}
			select {
			case <-h.stopped:
				t.Fatal("completed before upgrade close")
			default:
			}
			unblockClose()
			retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
			defer cancelRetry()
			if err := h.Shutdown(retry); err != nil {
				t.Fatal(err)
			}
			eventChatClosed(t, c)
			assertEventChatStopped(t, h)
		})
	}
}
func TestEventChatStartupBoundaries(t *testing.T) {
	t.Run("occupied", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		h := newEventChatHost()
		if err := h.Run(context.Background(), l.Addr().String(), time.Second); err == nil {
			t.Fatal("lost startup error")
		}
		assertEventChatStopped(t, h)
	})
	t.Run("pre-canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h := newEventChatHost()
		if err := h.Run(ctx, eventChatAddress(t), time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		assertEventChatStopped(t, h)
	})
	t.Run("shutdown-before-serve", func(t *testing.T) {
		h := newEventChatHost()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		addr := eventChatAddress(t)
		if err := h.Serve(addr); !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
			t.Fatal("late listener survived")
		}
		assertEventChatStopped(t, h)
	})
}

// Hold one recipient's write while other live clients disconnect and query the
// membership. Socket I/O must not retain the shared membership lock.
type heldRecipient struct {
	router.WebSocketContext
	entered chan struct{}
	release chan struct{}
}

func (r *heldRecipient) WriteJSON(v any) error {
	close(r.entered)
	<-r.release
	return r.WebSocketContext.WriteJSON(v)
}
func TestEventChatBlockedBroadcast(t *testing.T) {
	h := newEventChatHost()
	addr, _ := serveEventChat(t, h)
	sender := eventChatSocket(t, addr, "/ws/chat")
	eventChatWelcome(t, sender)
	recipient := eventChatSocket(t, addr, "/ws/chat")
	id := eventChatWelcome(t, recipient)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.clientsMu.Lock()
	h.clients[id] = &heldRecipient{WebSocketContext: h.clients[id], entered: entered, release: release}
	h.clientsMu.Unlock()
	if err := sender.WriteJSON(map[string]string{"type": "message", "message": "held-broadcast"}); err != nil {
		t.Fatal(err)
	}
	eventChatReceive(t, entered)
	recipient.Close()
	// A third connection must enter and observe the departed recipient even while
	// the original sender remains blocked in its owned broadcast.
	observer := eventChatSocket(t, addr, "/ws/chat")
	eventChatWelcome(t, observer)
	eventChatUsers(t, observer, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("held broadcast: %v", err)
	}
	select {
	case <-h.stopped:
		t.Fatal("completed before broadcast writer")
	default:
	}
	unblock()
	retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := h.Shutdown(retry); err != nil {
		t.Fatal(err)
	}
	eventChatClosed(t, sender)
	eventChatClosed(t, observer)
	assertEventChatStopped(t, h)
}
