//go:build ignore

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func receive[T any](t *testing.T, ch <-chan T) T {
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
func address(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	l.Close()
	return a
}
func httpClient(t *testing.T) *http.Client {
	t.Helper()
	c := &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(c.CloseIdleConnections)
	return c
}
func ready(t *testing.T, addr string) {
	t.Helper()
	c := httpClient(t)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := c.Get("http://" + addr + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("host not ready")
}
func serve(t *testing.T, h *advancedHost) (string, <-chan error) {
	t.Helper()
	addr := address(t)
	done := make(chan error, 1)
	go func() { done <- h.Serve(addr) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	ready(t, addr)
	return addr, done
}
func connect(t *testing.T, addr, path string) *websocket.Conn {
	t.Helper()
	c, res, err := websocket.DefaultDialer.Dial("ws://"+addr+path, nil)
	if res != nil {
		res.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func readType(t *testing.T, c *websocket.Conn, want string) map[string]any {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for range 200 {
		var msg map[string]any
		if err := c.ReadJSON(&msg); err != nil {
			t.Fatal(err)
		}
		if msg["type"] == want {
			return msg
		}
	}
	t.Fatalf("missing %s", want)
	return nil
}
func send(t *testing.T, c *websocket.Conn, msg map[string]any) {
	t.Helper()
	if err := c.WriteJSON(msg); err != nil {
		t.Fatal(err)
	}
}
func cleanClose(t *testing.T, c *websocket.Conn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		var closed *websocket.CloseError
		if !errors.As(err, &closed) || (closed.Code != 1000 && closed.Code != 1001) {
			t.Fatalf("unclean socket close: %v", err)
		}
		return
	}
}
func assertIdle(t *testing.T, h *advancedHost) {
	t.Helper()
	receive(t, h.stopped)
	h.mu.Lock()
	n := len(h.active)
	h.mu.Unlock()
	if n != 0 || getTotalPlayers() != 0 || getActiveSessions() != 0 {
		t.Fatalf("retained resources: sockets=%d players=%d sessions=%d", n, getTotalPlayers(), getActiveSessions())
	}
	clientsMutex.RLock()
	n = len(documentClients)
	clientsMutex.RUnlock()
	if n != 0 {
		t.Fatalf("retained collaboration clients: %d", n)
	}
	for protocol, count := range getProtocolStats() {
		if count != 0 {
			t.Fatalf("retained protocol %s: %d", protocol, count)
		}
	}
}

func TestAdvancedFeaturesAndShutdown(t *testing.T) {
	h := newAdvancedHost()
	addr, done := serve(t, h)
	for _, path := range []string{"/", "/health"} {
		res, err := httpClient(t).Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("HTTP %s: %v", path, err)
		}
		if path == "/" && (!strings.Contains(string(body), "location.host") || strings.Contains(string(body), "ws://localhost:8080")) {
			t.Fatal("dashboard socket origin mismatch")
		}
	}
	files := connect(t, addr, "/ws/files")
	readType(t, files, "welcome")
	content := "roundtrip data π\n"
	send(t, files, map[string]any{"type": "upload", "name": "test.txt", "content": content})
	uploaded := readType(t, files, "upload_complete")["file"].(map[string]any)
	id := uploaded["id"]
	send(t, files, map[string]any{"type": "download", "file_id": id})
	download := readType(t, files, "download_ready")["file"].(map[string]any)
	if download["content"] != content || download["size"] != float64(len(content)) {
		t.Fatalf("download integrity: %v", download)
	}
	send(t, files, map[string]any{"type": "delete", "file_id": id})
	readType(t, files, "file_deleted")
	collab := connect(t, addr, "/ws/collab")
	readType(t, collab, "welcome")
	doc := generateID()
	send(t, collab, map[string]any{"type": "join_document", "document_id": doc})
	readType(t, collab, "document_joined")
	send(t, collab, map[string]any{"type": "document_operation", "document_id": doc, "operation": "replace", "content": "updated"})
	updated := readType(t, collab, "document_updated")["document"].(map[string]any)
	if updated["content"] != "updated" {
		t.Fatal(updated)
	}
	game := connect(t, addr, "/ws/game")
	readType(t, game, "welcome")
	for _, room := range []string{"one-" + doc, "two-" + doc} {
		send(t, game, map[string]any{"type": "join_game", "game_id": room, "player_name": "test"})
		readType(t, game, "game_joined")
	}
	reconnect := connect(t, addr, "/ws/reconnect")
	session := readType(t, reconnect, "session_created")["session_id"].(string)
	send(t, reconnect, map[string]any{"type": "send_message", "target_session": session, "data": "queued"})
	readType(t, reconnect, "message_queued")
	reconnect.Close()
	deadline := time.Now().Add(3 * time.Second)
	for getActiveSessions() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if getActiveSessions() != 0 {
		t.Fatal("session disconnect not applied")
	}
	reconnect = connect(t, addr, "/ws/reconnect?session_id="+session)
	if queued := readType(t, reconnect, "queued_message"); queued["data"] != "queued" {
		t.Fatal(queued)
	}
	readType(t, reconnect, "reconnected")
	multi := connect(t, addr, "/ws/multiproto?protocol=json")
	readType(t, multi, "welcome")
	send(t, multi, map[string]any{"type": "echo", "data": "echoed"})
	if msg := readType(t, multi, "echo_response"); msg["original_data"] != "echoed" || msg["protocol"] != "json" {
		t.Fatal(msg)
	}
	res, err := httpClient(t).Get("http://" + addr + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), `"json":1`) {
		t.Fatalf("protocol stats: %s", body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*websocket.Conn{files, collab, game, reconnect, multi} {
		cleanClose(t, c)
	}
	if err := receive(t, done); !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
	assertIdle(t, h)
}

func TestAdvancedConcurrentClients(t *testing.T) {
	h := newAdvancedHost()
	addr, done := serve(t, h)
	doc, room := generateID(), generateID()
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c := connect(t, addr, "/ws/collab")
			defer c.Close()
			readType(t, c, "welcome")
			send(t, c, map[string]any{"type": "join_document", "document_id": doc})
			readType(t, c, "document_joined")
			for j := range 20 {
				send(t, c, map[string]any{"type": "document_operation", "document_id": doc, "operation": "replace", "content": fmt.Sprintf("%d-%d", i, j)})
				readType(t, c, "document_updated")
				send(t, c, map[string]any{"type": "cursor_position", "document_id": doc, "line": j, "column": i})
				readType(t, c, "cursor_updated")
			}
		}()
		go func() {
			defer wg.Done()
			c := connect(t, addr, "/ws/game")
			defer c.Close()
			readType(t, c, "welcome")
			for j := range 20 {
				send(t, c, map[string]any{"type": "join_game", "game_id": room, "player_name": fmt.Sprint(i)})
				readType(t, c, "game_joined")
				send(t, c, map[string]any{"type": "player_move", "game_id": room, "x": j, "y": i})
				readType(t, c, "player_moved")
			}
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	receive(t, done)
	assertIdle(t, h)
}

func TestAdvancedHTTPDrain(t *testing.T) {
	for _, mode := range []string{"graceful", "deadline-retry", "parent-cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newAdvancedHost()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			h.app.Router().Get("/held", func(c router.Context) error { close(entered); <-release; return c.SendString("complete") })
			addr := address(t)
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			done := make(chan error, 1)
			if mode == "parent-cancel" {
				go func() { done <- h.Run(parent, addr, 3*time.Second) }()
			} else {
				go func() { done <- h.Serve(addr) }()
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = h.Shutdown(ctx)
			})
			ready(t, addr)
			response := make(chan error, 1)
			c := httpClient(t)
			go func() {
				res, err := c.Get("http://" + addr + "/held")
				if err == nil {
					body, e := io.ReadAll(res.Body)
					res.Body.Close()
					err = e
					if err == nil && (res.StatusCode != 200 || string(body) != "complete") {
						err = fmt.Errorf("response %d %q", res.StatusCode, body)
					}
				}
				response <- err
			}()
			receive(t, entered)
			budget := 3 * time.Second
			if mode == "deadline-retry" {
				budget = 40 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			stopped := make(chan error, 1)
			if mode == "parent-cancel" {
				cancelParent()
				go func() { stopped <- receive(t, done) }()
			} else {
				go func() { stopped <- h.Shutdown(ctx) }()
			}
			if mode == "deadline-retry" {
				if err := receive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			} else {
				select {
				case err := <-stopped:
					t.Fatalf("returned before drain: %v", err)
				case <-time.After(40 * time.Millisecond):
				}
			}
			unblock()
			if err := receive(t, response); err != nil {
				t.Fatal(err)
			}
			if mode != "deadline-retry" {
				if err := receive(t, stopped); err != nil {
					t.Fatal(err)
				}
			}
			retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
			defer cancelRetry()
			if err := h.Shutdown(retry); err != nil {
				t.Fatal(err)
			}
			if mode != "parent-cancel" {
				if err := receive(t, done); !errors.Is(err, http.ErrServerClosed) {
					t.Fatal(err)
				}
			}
			assertIdle(t, h)
		})
	}
}

func TestAdvancedOwnedWorkerDeadline(t *testing.T) {
	h := newAdvancedHost()
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.app.Router().WebSocket("/held", router.DefaultWebSocketConfig(), h.track(func(router.WebSocketContext) error { close(entered); <-release; return nil }))
	addr, done := serve(t, h)
	c := connect(t, addr, "/held")
	receive(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-h.stopped:
		t.Fatal("joined held worker")
	default:
	}
	unblock()
	retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := h.Shutdown(retry); err != nil {
		t.Fatal(err)
	}
	cleanClose(t, c)
	receive(t, done)
	assertIdle(t, h)
}
func TestAdvancedPeerCloseDuringStop(t *testing.T) {
	h := newAdvancedHost()
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	config := router.DefaultWebSocketConfig()
	config.OnDisconnect = func(router.WebSocketContext, error) { close(entered); <-release }
	h.app.Router().WebSocket("/closing", config, h.track(func(ws router.WebSocketContext) error { _, _, err := ws.ReadMessage(); return err }))
	addr, done := serve(t, h)
	c := connect(t, addr, "/closing")
	c.Close()
	receive(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-h.stopped:
		t.Fatal("joined held close callback")
	default:
	}
	unblock()
	retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := h.Shutdown(retry); err != nil {
		t.Fatal(err)
	}
	receive(t, done)
	assertIdle(t, h)
}
func TestAdvancedStartupBoundaries(t *testing.T) {
	t.Run("occupied", func(t *testing.T) {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		h := newAdvancedHost()
		if err := h.Run(context.Background(), l.Addr().String(), time.Second); err == nil {
			t.Fatal("startup error lost")
		}
		assertIdle(t, h)
	})
	t.Run("pre-canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h := newAdvancedHost()
		if err := h.Run(ctx, address(t), time.Second); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		assertIdle(t, h)
	})
	t.Run("shutdown-before-serve", func(t *testing.T) {
		h := newAdvancedHost()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		if err := h.Serve(address(t)); !errors.Is(err, http.ErrServerClosed) {
			t.Fatal(err)
		}
		assertIdle(t, h)
	})
}
func TestAdvancedProcessHelper(t *testing.T) {
	if addr := os.Getenv("H036_CHILD"); addr != "" {
		flag.CommandLine = flag.NewFlagSet("advanced", flag.ExitOnError)
		os.Args = []string{os.Args[0], "-address", addr}
		main()
		os.Exit(0)
	}
}
func TestAdvancedSignals(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			addr := address(t)
			log, err := os.CreateTemp(t.TempDir(), "host")
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestAdvancedProcessHelper$")
			cmd.Env = append(os.Environ(), "H036_CHILD="+addr)
			cmd.Stdout = log
			cmd.Stderr = log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done, finished := make(chan error, 1), make(chan struct{})
			go func() { done <- cmd.Wait(); close(finished) }()
			t.Cleanup(func() {
				select {
				case <-finished:
				default:
					cmd.Process.Kill()
					<-finished
				}
			})
			ready(t, addr)
			var clients []*websocket.Conn
			for _, path := range []string{"/ws/files", "/ws/collab", "/ws/game", "/ws/reconnect", "/ws/multiproto"} {
				c := connect(t, addr, path)
				kind := "welcome"
				if path == "/ws/reconnect" {
					kind = "session_created"
				}
				readType(t, c, kind)
				clients = append(clients, c)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			for _, c := range clients {
				cleanClose(t, c)
			}
			if err := receive(t, done); err != nil {
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("process: %v %s", err, data)
			}
		})
	}
}

func TestAdvancedUpgradeDuringStop(t *testing.T) {
	h := newAdvancedHost()
	upgrading, releaseUpgrade := make(chan struct{}), make(chan struct{})
	closing, releaseClose := make(chan struct{}), make(chan struct{})
	unblockUpgrade := sync.OnceFunc(func() { close(releaseUpgrade) })
	defer unblockUpgrade()
	unblockClose := sync.OnceFunc(func() { close(releaseClose) })
	defer unblockClose()
	cfg := router.DefaultWebSocketConfig()
	cfg.OnPreUpgrade = func(router.Context) (router.UpgradeData, error) { close(upgrading); <-releaseUpgrade; return nil, nil }
	cfg.OnDisconnect = func(router.WebSocketContext, error) { close(closing); <-releaseClose }
	h.app.Router().WebSocket("/late-upgrade", cfg, h.track(func(router.WebSocketContext) error { t.Error("admitted socket after stop"); return nil }))
	addr, done := serve(t, h)
	dialed := make(chan *websocket.Conn, 1)
	go func() { dialed <- connect(t, addr, "/late-upgrade") }()
	receive(t, upgrading)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- h.Shutdown(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		stopping := h.stopping
		h.mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop did not latch")
		}
		time.Sleep(time.Millisecond)
	}
	unblockUpgrade()
	ws := receive(t, dialed)
	receive(t, closing)
	if err := receive(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("returned with upgrade close callback held: %v", err)
	}
	select {
	case <-h.stopped:
		t.Fatal("owned completion published before upgrade close")
	default:
	}
	unblockClose()
	retry, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err := h.Shutdown(retry); err != nil {
		t.Fatal(err)
	}
	cleanClose(t, ws)
	receive(t, done)
	assertIdle(t, h)
}
