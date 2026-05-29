package router_test

import (
	"net/http"
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
	app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
	if !ok {
		t.Fatal("expected FiberAdapter")
	}
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

	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

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
			_, data, readErr := conn.ReadMessage()
			if readErr != nil {
				readErrs <- readErr
				return
			}
			messages <- data
		}
	}()

	deadline := time.After(500 * time.Millisecond)
	for pings.Load() == 0 {
		select {
		case handlerErr := <-handlerErrs:
			t.Fatalf("server handler returned before keepalive ping: %v", handlerErr)
		case readErr := <-readErrs:
			t.Fatalf("client read failed before keepalive ping: %v", readErr)
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
	case handlerErr := <-handlerErrs:
		t.Fatalf("server handler returned during idle period: %v", handlerErr)
	case readErr := <-readErrs:
		t.Fatalf("client read failed during idle period: %v", readErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for echo after idle period")
	}
}

func TestFiberWebSocketSilentClientStillTimesOut(t *testing.T) {
	app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
	if !ok {
		t.Fatal("expected FiberAdapter")
	}
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

	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

	select {
	case handlerErr := <-handlerErrs:
		if handlerErr == nil {
			t.Fatal("expected read timeout for silent client")
		}
	case <-time.After(time.Second):
		t.Fatal("expected silent client to time out")
	}
}

func TestFiberWebSocketDisableKeepAliveDisablesAutomaticPings(t *testing.T) {
	app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
	if !ok {
		t.Fatal("expected FiberAdapter")
	}
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

	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

	var pings atomic.Int32
	conn.SetPingHandler(func(data string) error {
		pings.Add(1)
		return nil
	})

	if err := conn.SetReadDeadline(time.Now().Add(3 * config.PingPeriod)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	if _, _, readErr := conn.ReadMessage(); readErr != nil {
		t.Logf("read ended while checking for suppressed pings: %v", readErr)
	}

	if got := pings.Load(); got != 0 {
		t.Fatalf("expected DisableKeepAlive to suppress automatic pings, got %d", got)
	}
}

func TestFiberWebSocketDisableReadDeadlineKeepsPingingWithoutPongTimeout(t *testing.T) {
	app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
	if !ok {
		t.Fatal("expected FiberAdapter")
	}
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

	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

	var pings atomic.Int32
	conn.SetPingHandler(func(data string) error {
		pings.Add(1)
		return nil
	})

	readErrs := make(chan error, 1)
	go func() {
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				readErrs <- readErr
				return
			}
		}
	}()

	time.Sleep(3 * config.PongWait)

	if got := pings.Load(); got == 0 {
		t.Fatal("expected automatic pings to continue when only read deadlines are disabled")
	}

	select {
	case handlerErr := <-handlerErrs:
		t.Fatalf("server handler returned despite disabled read deadline: %v", handlerErr)
	case readErr := <-readErrs:
		t.Fatalf("client read failed while read deadline was disabled: %v", readErr)
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

	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

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
			_, data, readErr := conn.ReadMessage()
			if readErr != nil {
				readErrs <- readErr
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
	case handlerErr := <-handlerErrs:
		t.Fatalf("server handler returned during idle period: %v", handlerErr)
	case readErr := <-readErrs:
		t.Fatalf("client read failed during idle period: %v", readErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for echo after idle period")
	}
}

func TestHTTPRouterWebSocketMiddlewareIdleConnectionSurvivesPastPongWait(t *testing.T) {
	app := router.NewHTTPServer()
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 40 * time.Millisecond
	config.PongWait = 120 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond

	handlerErrs := make(chan error, 1)
	app.Router().Get("/ws", func(ctx router.Context) error {
		ws, ok := ctx.(router.WebSocketContext)
		if !ok {
			return nil
		}
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			handlerErrs <- err
			return err
		}
		return ws.WriteMessage(messageType, data)
	}, router.WebSocketUpgrade(config))

	server := httptest.NewServer(app.WrappedRouter())
	defer server.Close()

	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

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
			_, data, readErr := conn.ReadMessage()
			if readErr != nil {
				readErrs <- readErr
				return
			}
			messages <- data
		}
	}()

	time.Sleep(2 * config.PongWait)
	if got := pings.Load(); got == 0 {
		t.Fatal("expected middleware WebSocket server to send keepalive pings")
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
	case handlerErr := <-handlerErrs:
		t.Fatalf("server handler returned during idle period: %v", handlerErr)
	case readErr := <-readErrs:
		t.Fatalf("client read failed during idle period: %v", readErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for echo after idle period")
	}
}

func TestFiberWebSocketCustomPongHandlerPreservesDeadlineRefresh(t *testing.T) {
	app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
	if !ok {
		t.Fatal("expected FiberAdapter")
	}
	config := router.DefaultWebSocketConfig()
	config.PingPeriod = 40 * time.Millisecond
	config.PongWait = 120 * time.Millisecond
	config.WriteTimeout = 100 * time.Millisecond

	var serverPongs atomic.Int32
	handlerErrs := make(chan error, 1)
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		ws.SetPongHandler(func(data []byte) error {
			serverPongs.Add(1)
			return nil
		})
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			handlerErrs <- err
			return err
		}
		return ws.WriteMessage(messageType, data)
	})

	address, shutdown := startFiberServer(t, app)
	defer shutdown()

	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

	var writeMu sync.Mutex
	conn.SetPingHandler(func(data string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
	})

	go func() {
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				return
			}
		}
	}()

	deadline := time.After(500 * time.Millisecond)
	for serverPongs.Load() == 0 {
		select {
		case handlerErr := <-handlerErrs:
			t.Fatalf("server handler returned before custom pong callback: %v", handlerErr)
		case <-deadline:
			t.Fatal("expected custom server pong callback to run")
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
	case handlerErr := <-handlerErrs:
		t.Fatalf("server handler returned during idle period: %v", handlerErr)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestFiberWebSocketCustomPingHandlerStillSendsPong(t *testing.T) {
	app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
	if !ok {
		t.Fatal("expected FiberAdapter")
	}
	config := router.DefaultWebSocketConfig()
	config.DisableKeepAlive = true

	var serverPings atomic.Int32
	app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
		ws.SetPingHandler(func(data []byte) error {
			serverPings.Add(1)
			return nil
		})
		_, _, err := ws.ReadMessage()
		return err
	})

	address, shutdown := startFiberServer(t, app)
	defer shutdown()

	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
	defer closeWebSocketResponse(t, resp)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer closeWebSocketConn(t, conn)

	time.Sleep(50 * time.Millisecond)

	pongs := make(chan struct{}, 1)
	conn.SetPongHandler(func(data string) error {
		pongs <- struct{}{}
		return nil
	})

	if err := conn.WriteControl(websocket.PingMessage, []byte("client-ping"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("failed to send client ping: %v", err)
	}

	readErrs := make(chan error, 1)
	go func() {
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				readErrs <- readErr
				return
			}
		}
	}()

	select {
	case <-pongs:
	case readErr := <-readErrs:
		t.Fatalf("client read failed before pong: %v", readErr)
	case <-time.After(time.Second):
		t.Fatal("expected server to send pong despite custom ping handler")
	}

	if got := serverPings.Load(); got == 0 {
		t.Fatal("expected custom server ping callback to run")
	}
}

func TestWebSocketDirectHandlersRejectInvalidConfig(t *testing.T) {
	t.Run("HTTPRouter", func(t *testing.T) {
		app := router.NewHTTPServer()
		config := router.DefaultWebSocketConfig()
		config.PingPeriod = 2 * time.Second
		config.PongWait = time.Second
		app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
			return nil
		})

		server := httptest.NewServer(app.WrappedRouter())
		defer server.Close()

		conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/ws", nil)
		defer closeWebSocketResponse(t, resp)
		if err == nil {
			closeWebSocketConn(t, conn)
			t.Fatal("expected invalid config to reject direct HTTPRouter websocket")
		}
		if resp == nil || resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected HTTP 500 for invalid config, got response %#v and error %v", resp, err)
		}
	})

	t.Run("Fiber", func(t *testing.T) {
		app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
		if !ok {
			t.Fatal("expected FiberAdapter")
		}
		config := router.DefaultWebSocketConfig()
		config.PingPeriod = 2 * time.Second
		config.PongWait = time.Second
		app.Router().WebSocket("/ws", config, func(ws router.WebSocketContext) error {
			return nil
		})

		address, shutdown := startFiberServer(t, app)
		defer shutdown()

		conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws", nil)
		defer closeWebSocketResponse(t, resp)
		if err == nil {
			closeWebSocketConn(t, conn)
			t.Fatal("expected invalid config to reject direct Fiber websocket")
		}
		if resp == nil || resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected HTTP 500 for invalid config, got response %#v and error %v", resp, err)
		}
	})
}
