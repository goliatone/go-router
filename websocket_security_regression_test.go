package router_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func TestHTTPRouterWebSocketRejectsCrossOriginByDefault(t *testing.T) {
	app := router.NewHTTPServer()
	app.Router().WebSocket("/ws", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		return nil
	})

	server := httptest.NewServer(app.WrappedRouter())
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http", "ws", 1) + "/ws"
	headers := http.Header{}
	headers.Set("Origin", "https://evil.example.com")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-origin websocket dial to be rejected")
	}
}

func TestFiberWebSocketRejectsCrossOriginByDefault(t *testing.T) {
	app := router.NewFiberAdapter().(*router.FiberAdapter)
	app.Router().WebSocket("/ws", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		return nil
	})

	address, shutdown := startFiberServer(t, app)
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

func TestHTTPRouterOnMessageCloseDoesNotDeadlock(t *testing.T) {
	config := router.DefaultWebSocketConfig()
	closed := make(chan error, 1)
	config.OnMessage = func(ws router.WebSocketContext, _ int, _ []byte) error {
		select {
		case closed <- ws.Close():
		default:
		}
		return nil
	}

	app := router.NewHTTPServer()
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		_, _, _ = ws.ReadMessage()
		return nil
	})

	server := httptest.NewServer(app.WrappedRouter())
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http", "ws", 1) + "/ws"
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

func TestFiberOnMessageCloseDoesNotDeadlock(t *testing.T) {
	config := router.DefaultWebSocketConfig()
	closed := make(chan error, 1)
	config.OnMessage = func(ws router.WebSocketContext, _ int, _ []byte) error {
		select {
		case closed <- ws.Close():
		default:
		}
		return nil
	}

	app := router.NewFiberAdapter().(*router.FiberAdapter)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		_, _, _ = ws.ReadMessage()
		return nil
	})

	address, shutdown := startFiberServer(t, app)
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

func startFiberServer(t *testing.T, app *router.FiberAdapter) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate listener: %v", err)
	}

	fiberApp := app.WrappedRouter()
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- fiberApp.Listener(listener)
	}()

	time.Sleep(50 * time.Millisecond)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		select {
		case <-serverErr:
		case <-time.After(500 * time.Millisecond):
		}
	}

	return listener.Addr().String(), shutdown
}
