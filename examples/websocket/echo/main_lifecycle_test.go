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
func TestEchoProcessHelper(t *testing.T) {
	if os.Getenv("H040_CHILD") == "1" {
		main()
		os.Exit(0)
	}
}

func echoAddress(t *testing.T) string {
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

func echoReady(t *testing.T, address string) {
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

func echoSocket(t *testing.T, address, path string) *websocket.Conn {
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

func echoClosed(t *testing.T, c *websocket.Conn) {
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

func echoReceive[T any](t *testing.T, ch <-chan T) T {
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

func serveEcho(t *testing.T, h *echoHost) (string, <-chan error) {
	t.Helper()
	address := echoAddress(t)
	served := make(chan error, 1)
	go func() { served <- h.Serve(address) }()
	echoReady(t, address)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := echoReceive(t, served); !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
	})
	return address, served
}
func echoWelcome(t *testing.T, c *websocket.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg map[string]string
	if err := c.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	if msg["type"] != "welcome" || msg["message"] != "Welcome to Echo Server!" || msg["id"] == "" {
		t.Fatalf("welcome: %v", msg)
	}
}
func echoRoundtrip(t *testing.T, c *websocket.Conn, kind int, data string) {
	t.Helper()
	if err := c.WriteMessage(kind, []byte(data)); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg struct {
		Type, Original, Server string
		MessageType            int
		Timestamp              int64
	}
	if err := c.ReadJSON(&msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "echo" || msg.Original != data || msg.MessageType != kind || msg.Server != "echo-server" || msg.Timestamp <= 0 {
		t.Fatalf("echo: %+v", msg)
	}
}
func assertEchoStopped(t *testing.T, h *echoHost) {
	t.Helper()
	echoReceive(t, h.stopped)
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.stopping || len(h.active) != 0 {
		t.Fatalf("stopping=%t active=%d", h.stopping, len(h.active))
	}
}
func TestEchoProtocol(t *testing.T) {
	h := newEchoHost()
	addr, _ := serveEcho(t, h)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	res, err := client.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	res.Body.Close()
	if readErr != nil || res.StatusCode != 200 || !strings.Contains(string(body), "location.host") || !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("page: %d %v", res.StatusCode, readErr)
	}
	res, err = client.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatal(err)
	}
	var health map[string]string
	err = json.NewDecoder(res.Body).Decode(&health)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || health["status"] != "healthy" {
		t.Fatalf("health: %v %v", health, err)
	}
	c := echoSocket(t, addr, "/ws/echo")
	echoWelcome(t, c)
	echoRoundtrip(t, c, websocket.TextMessage, "hello λ")
	echoRoundtrip(t, c, websocket.BinaryMessage, "binary\x00payload")
	ping := make(chan string, 1)
	old := c.PingHandler()
	c.SetPingHandler(func(data string) error { ping <- data; return old(data) })
	if err := c.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	echoRoundtrip(t, c, websocket.TextMessage, "after-ping")
	if data := echoReceive(t, ping); data != "pong" {
		t.Fatalf("ping payload %q", data)
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte("close")); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != 1000 || closeErr.Text != "Goodbye!" {
		t.Fatalf("close command: %v", err)
	}
}
func TestEchoSignals(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			addr := echoAddress(t)
			log, err := os.CreateTemp(t.TempDir(), "host")
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestEchoProcessHelper$")
			cmd.Env = append(os.Environ(), "H040_CHILD=1", "ECHO_ADDR="+addr)
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
			echoReady(t, addr)
			c := echoSocket(t, addr, "/ws/echo")
			echoWelcome(t, c)
			echoRoundtrip(t, c, websocket.TextMessage, "before-signal")
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			echoClosed(t, c)
			if err := echoReceive(t, done); err != nil {
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
func TestEchoConcurrentClients(t *testing.T) {
	h := newEchoHost()
	addr, _ := serveEcho(t, h)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := echoSocket(t, addr, "/ws/echo")
			defer c.Close()
			echoWelcome(t, c)
			for j := range 10 {
				echoRoundtrip(t, c, websocket.TextMessage, fmt.Sprintf("client-%d-%d", i, j))
			}
			if err := c.WriteMessage(websocket.TextMessage, []byte("close")); err != nil {
				t.Error(err)
			}
			echoClosed(t, c)
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	assertEchoStopped(t, h)
}
func TestEchoHTTPDrainAndRetry(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			h := newEchoHost()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			h.app.Router().Get("/held", func(c router.Context) error { close(entered); <-release; return c.SendString("complete") })
			addr, _ := serveEcho(t, h)
			c := echoSocket(t, addr, "/ws/echo")
			echoWelcome(t, c)
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
			echoReceive(t, entered)
			budget := 3 * time.Second
			if deadline {
				budget = 40 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- h.Shutdown(ctx) }()
			if deadline {
				if err := echoReceive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline: %v", err)
				}
			} else {
				select {
				case err := <-stopped:
					t.Fatalf("returned before drain: %v", err)
				case <-time.After(40 * time.Millisecond):
				}
			}
			echoClosed(t, c)
			unblock()
			if err := echoReceive(t, response); err != nil {
				t.Fatal(err)
			}
			if !deadline {
				if err := echoReceive(t, stopped); err != nil {
					t.Fatal(err)
				}
			}
			retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
			defer cancelRetry()
			if err := h.Shutdown(retry); err != nil {
				t.Fatal(err)
			}
			assertEchoStopped(t, h)
		})
	}
}
func TestEchoHeldWorker(t *testing.T) {
	h := newEchoHost()
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.app.Router().WebSocket("/held", router.DefaultWebSocketConfig(), h.track(func(router.WebSocketContext) error { close(entered); <-release; return nil }))
	addr, _ := serveEcho(t, h)
	c := echoSocket(t, addr, "/held")
	echoReceive(t, entered)
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
	echoClosed(t, c)
	assertEchoStopped(t, h)
}
func TestEchoCloseDuringStop(t *testing.T) {
	for _, peer := range []bool{false, true} {
		t.Run(fmt.Sprintf("peer=%t", peer), func(t *testing.T) {
			h := newEchoHost()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			cfg := router.DefaultWebSocketConfig()
			cfg.OnDisconnect = func(router.WebSocketContext, error) { close(entered); <-release }
			h.app.Router().WebSocket("/closing", cfg, h.track(func(ws router.WebSocketContext) error {
				_, _, _ = ws.ReadMessage()
				return ws.CloseWithStatus(router.CloseNormalClosure, "done")
			}))
			addr, _ := serveEcho(t, h)
			c := echoSocket(t, addr, "/closing")
			if peer {
				c.Close()
			} else {
				if err := c.WriteMessage(websocket.TextMessage, []byte("close")); err != nil {
					t.Fatal(err)
				}
			}
			echoReceive(t, entered)
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
				echoClosed(t, c)
			}
			assertEchoStopped(t, h)
		})
	}
}
func TestEchoUpgradeDuringStop(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(fmt.Sprintf("connected=%t", connected), func(t *testing.T) {
			h := newEchoHost()
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
			addr, _ := serveEcho(t, h)
			dialed := make(chan *websocket.Conn, 1)
			go func() { dialed <- echoSocket(t, addr, "/late") }()
			echoReceive(t, entered)
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
			c := echoReceive(t, dialed)
			echoReceive(t, closing)
			if err := echoReceive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
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
			echoClosed(t, c)
			assertEchoStopped(t, h)
		})
	}
}
func TestEchoStartupBoundaries(t *testing.T) {
	t.Run("occupied", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		h := newEchoHost()
		if err := h.Run(context.Background(), l.Addr().String(), time.Second); err == nil {
			t.Fatal("lost startup error")
		}
		assertEchoStopped(t, h)
	})
	t.Run("pre-canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h := newEchoHost()
		if err := h.Run(ctx, echoAddress(t), time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		assertEchoStopped(t, h)
	})
	t.Run("shutdown-before-serve", func(t *testing.T) {
		h := newEchoHost()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		addr := echoAddress(t)
		if err := h.Serve(addr); !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
			t.Fatal("late listener survived")
		}
		assertEchoStopped(t, h)
	})
}
