package router_test

import (
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func TestFiberWebSocketIdleConnectionSurvivesPastPongWait(t *testing.T) {
	app := router.NewFiberAdapter().(*router.FiberAdapter)
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 40 * time.Millisecond
	config.PongWait = 120 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond

	handlerErrs := make(chan error, 1)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			handlerErrs <- err
			return err
		}
		return ws.WriteMessage(messageType, data)
	})

	address, shutdown := startFiberServer(t, app)
	defer shutdown()

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	var pings atomic.Int32
	var writeMu sync.Mutex
	conn.SetPingHandler(func(data string) error {
		pings.Add(1)
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})

	messages := make(chan []byte, 1)
	readErrs := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				readErrs <- err
				return
			}
			messages <- data
		}
	}()

	deadline := time.After(500 * time.Millisecond)
	for pings.Load() == 0 {
		select {
		case err := <-handlerErrs:
			t.Fatalf("server handler returned before keepalive ping: %v", err)
		case err := <-readErrs:
			t.Fatalf("client read failed before keepalive ping: %v", err)
		case <-deadline:
			t.Fatal("expected server to send at least one ping")
		case <-time.After(10 * time.Millisecond):
		}
	}

	time.Sleep(2 * config.PongWait)

	writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, []byte("after-idle"))
	writeMu.Unlock()
	if err != nil {
		t.Fatalf("failed to write after idle period: %v", err)
	}

	select {
	case got := <-messages:
		if string(got) != "after-idle" {
			t.Fatalf("expected echo after idle period, got %q", string(got))
		}
	case err := <-handlerErrs:
		t.Fatalf("server handler returned during idle period: %v", err)
	case err := <-readErrs:
		t.Fatalf("client read failed during idle period: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for echo after idle period")
	}
}

func TestFiberWebSocketSilentClientStillTimesOut(t *testing.T) {
	app := router.NewFiberAdapter().(*router.FiberAdapter)
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 40 * time.Millisecond
	config.PongWait = 120 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond

	handlerErrs := make(chan error, 1)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		_, _, err := ws.ReadMessage()
		handlerErrs <- err
		return nil
	})

	address, shutdown := startFiberServer(t, app)
	defer shutdown()

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-handlerErrs:
		if err == nil {
			t.Fatal("expected read timeout for silent client")
		}
	case <-time.After(time.Second):
		t.Fatal("expected silent client to time out")
	}
}

func TestFiberWebSocketDisableKeepAliveDisablesAutomaticPings(t *testing.T) {
	app := router.NewFiberAdapter().(*router.FiberAdapter)
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 30 * time.Millisecond
	config.PongWait = 90 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond
	config.DisableKeepAlive = true

	done := make(chan struct{})
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		<-done
		return nil
	})

	address, shutdown := startFiberServer(t, app)
	defer shutdown()
	defer close(done)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	var pings atomic.Int32
	conn.SetPingHandler(func(data string) error {
		pings.Add(1)
		return nil
	})

	_ = conn.SetReadDeadline(time.Now().Add(3 * config.PingPeriod))
	_, _, _ = conn.ReadMessage()

	if got := pings.Load(); got != 0 {
		t.Fatalf("expected DisableKeepAlive to suppress automatic pings, got %d", got)
	}
}

func TestFiberWebSocketDisableReadDeadlineKeepsPingingWithoutPongTimeout(t *testing.T) {
	app := router.NewFiberAdapter().(*router.FiberAdapter)
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 30 * time.Millisecond
	config.PongWait = 90 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond
	config.DisableReadDeadline = true

	handlerErrs := make(chan error, 1)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		_, _, err := ws.ReadMessage()
		handlerErrs <- err
		return nil
	})

	address, shutdown := startFiberServer(t, app)
	defer shutdown()

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	var pings atomic.Int32
	conn.SetPingHandler(func(data string) error {
		pings.Add(1)
		return nil
	})

	readErrs := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				readErrs <- err
				return
			}
		}
	}()

	time.Sleep(3 * config.PongWait)

	if got := pings.Load(); got == 0 {
		t.Fatal("expected automatic pings to continue when only read deadlines are disabled")
	}

	select {
	case err := <-handlerErrs:
		t.Fatalf("server handler returned despite disabled read deadline: %v", err)
	case err := <-readErrs:
		t.Fatalf("client read failed while read deadline was disabled: %v", err)
	default:
	}
}

func TestHTTPRouterWebSocketIdleConnectionSurvivesPastPongWait(t *testing.T) {
	app := router.NewHTTPServer()
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 40 * time.Millisecond
	config.PongWait = 120 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond

	handlerErrs := make(chan error, 1)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			handlerErrs <- err
			return err
		}
		return ws.WriteMessage(messageType, data)
	})

	server := httptest.NewServer(app.WrappedRouter())
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/ws", nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer conn.Close()

	var pings atomic.Int32
	var writeMu sync.Mutex
	conn.SetPingHandler(func(data string) error {
		pings.Add(1)
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})

	messages := make(chan []byte, 1)
	readErrs := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				readErrs <- err
				return
			}
			messages <- data
		}
	}()

	time.Sleep(2 * config.PongWait)
	if got := pings.Load(); got == 0 {
		t.Fatal("expected HTTPRouter server to send keepalive pings")
	}

	writeMu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, []byte("after-idle"))
	writeMu.Unlock()
	if err != nil {
		t.Fatalf("failed to write after idle period: %v", err)
	}

	select {
	case got := <-messages:
		if string(got) != "after-idle" {
			t.Fatalf("expected echo after idle period, got %q", string(got))
		}
	case err := <-handlerErrs:
		t.Fatalf("server handler returned during idle period: %v", err)
	case err := <-readErrs:
		t.Fatalf("client read failed during idle period: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for echo after idle period")
	}
}
