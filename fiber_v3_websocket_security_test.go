package router_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func TestFiberV3WebSocketRejectsCrossOriginByDefault(t *testing.T) {
	app := router.NewFiberV3Adapter().(*router.FiberV3Adapter)
	app.Router().WebSocket("/ws", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		return nil
	})

	address, shutdown := startFiberV3Server(t, app)
	defer shutdown()

	wsURL := "ws://" + address + "/ws"
	headers := http.Header{}
	headers.Set("Origin", "https://evil.example.com")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-origin websocket dial to be rejected")
	}
}

func TestFiberV3WebSocketAllowsSameOriginWithExplicitOriginOnNonDefaultPort(t *testing.T) {
	app := router.NewFiberV3Adapter().(*router.FiberV3Adapter)
	app.Router().WebSocket("/ws", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		return nil
	})

	address, shutdown := startFiberV3Server(t, app)
	defer shutdown()

	wsURL := "ws://" + address + "/ws"
	headers := http.Header{}
	headers.Set("Origin", "http://"+address)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("expected same-origin websocket dial to succeed, got %v", err)
	}
	_ = conn.Close()
}

func TestFiberV3WebSocketRejectsCrossOriginWithForbiddenStatus(t *testing.T) {
	app := router.NewFiberV3Adapter().(*router.FiberV3Adapter)
	app.Router().WebSocket("/ws", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		return nil
	})

	address, shutdown := startFiberV3Server(t, app)
	defer shutdown()

	req, err := http.NewRequest(http.MethodGet, "http://"+address+"/ws", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("handshake request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected cross-origin handshake to return 403, got %d", resp.StatusCode)
	}
}

func TestFiberV3OnMessageCloseDoesNotDeadlock(t *testing.T) {
	config := router.DefaultWebSocketConfig()
	closed := make(chan error, 1)
	config.OnMessage = func(ws router.WebSocketContext, _ int, _ []byte) error {
		select {
		case closed <- ws.Close():
		default:
		}
		return nil
	}

	app := router.NewFiberV3Adapter().(*router.FiberV3Adapter)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		_, _, _ = ws.ReadMessage()
		return nil
	})

	address, shutdown := startFiberV3Server(t, app)
	defer shutdown()

	wsURL := "ws://" + address + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("close")); err != nil {
		t.Fatalf("failed to write websocket message: %v", err)
	}

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("unexpected close error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnMessage callback deadlocked while closing websocket")
	}
}

func startFiberV3Server(t *testing.T, app *router.FiberV3Adapter) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	done := make(chan error, 1)
	native := app.WrappedRouter()
	go func() {
		done <- native.Listener(listener, fiber3.ListenConfig{DisableStartupMessage: true, BeforeServeFunc: func(*fiber3.App) error { close(ready); return nil }})
	}()
	lifecycleReceive(t, ready)
	return listener.Addr().String(), func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := lifecycleReceive(t, done); err != nil {
			t.Error(err)
		}
	}
}
