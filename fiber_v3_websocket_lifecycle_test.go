package router_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func TestFiberV3WebSocketMiddlewareSnapshotAndOwnedShutdown(t *testing.T) {
	s := requireTestType[*router.FiberV3Adapter](t, router.NewFiberV3Adapter())
	cfg := router.DefaultWebSocketConfig()
	cfg.DisableReadDeadline = true
	cfg.Subprotocols = []string{"t03"}
	cfg.OnPreUpgrade = func(c router.Context) (router.UpgradeData, error) { return router.UpgradeData{"actor": "t03"}, nil }
	connected := make(chan router.WebSocketContext, 1)
	stopped := make(chan error, 1)
	s.Router().Get("/socket/:id", func(c router.Context) error {
		ws, ok := c.(router.WebSocketContext)
		if !ok {
			return fmt.Errorf("missing WebSocketContext")
		}
		if ws.Param("id") != "42" || ws.Header("X-Snapshot") != "kept" || ws.RouteName() != "socket" || ws.Subprotocol() != "t03" {
			stopped <- fmt.Errorf("snapshot mismatch")
			return nil
		}
		if value, _ := ws.UpgradeData("actor"); value != "t03" {
			stopped <- errors.New("upgrade data missing")
			return nil
		}
		if requireTestType[interface{ Request() *http.Request }](t, ws).Request() != nil {
			stopped <- errors.New("pooled request retained")
			return nil
		}
		connected <- ws
		_, _, err := ws.ReadMessage()
		stopped <- err
		return nil
	}, router.WebSocketUpgrade(cfg)).SetName("socket")
	addr, shutdown := startFiberV3Server(t, s)
	defer shutdown()
	headers := http.Header{"X-Snapshot": []string{"kept"}}
	dialer := websocket.Dialer{Subprotocols: []string{"t03"}, HandshakeTimeout: time.Second}
	conn, resp, err := dialer.Dial("ws://"+addr+"/socket/42", headers)
	closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWebSocketConn(t, conn)
	var ws router.WebSocketContext
	select {
	case ws = <-connected:
	case err := <-stopped:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("upgrade timeout")
	}
	if err := requireTestType[router.WebSocketReadInterrupter](t, ws).InterruptRead(); err != nil {
		t.Fatal(err)
	}
	if err := lifecycleReceive(t, stopped); !errors.Is(err, router.ErrWebSocketReadInterrupted) {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("owned connection did not close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
