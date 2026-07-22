package router_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func TestWebSocketInterruptReadUnblocksActiveRead(t *testing.T) {
	tests := []struct {
		name  string
		serve func(t *testing.T, handler func(router.WebSocketContext) error) (string, func())
	}{
		{
			name: "HTTPRouter",
			serve: func(t *testing.T, handler func(router.WebSocketContext) error) (string, func()) {
				app := router.NewHTTPServer()
				config := router.DefaultWebSocketConfig()
				config.DisableKeepAlive = true
				config.DisableReadDeadline = true
				app.Router().WebSocket("/ws", config, handler)
				server := httptest.NewServer(app.WrappedRouter())
				return strings.Replace(server.URL, "http", "ws", 1) + "/ws", server.Close
			},
		},
		{
			name: "Fiber",
			serve: func(t *testing.T, handler func(router.WebSocketContext) error) (string, func()) {
				app, ok := router.NewFiberAdapter().(*router.FiberAdapter)
				if !ok {
					t.Fatal("expected FiberAdapter")
				}
				config := router.DefaultWebSocketConfig()
				config.DisableKeepAlive = true
				config.DisableReadDeadline = true
				app.Router().WebSocket("/ws", config, handler)
				address, shutdown := startFiberServer(t, app)
				return "ws://" + address + "/ws", shutdown
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerResult := make(chan error, 1)
			url, shutdown := tt.serve(t, func(ws router.WebSocketContext) error {
				interrupter, ok := ws.(router.WebSocketReadInterrupter)
				if !ok {
					err := errors.New("websocket adapter does not implement WebSocketReadInterrupter")
					handlerResult <- err
					return err
				}

				readStarted := make(chan struct{})
				readResult := make(chan error, 1)
				go func() {
					close(readStarted)
					var message map[string]any
					readResult <- ws.ReadJSON(&message)
				}()

				<-readStarted
				if err := interrupter.InterruptRead(); err != nil {
					handlerResult <- err
					return err
				}

				select {
				case err := <-readResult:
					if !errors.Is(err, router.ErrWebSocketReadInterrupted) {
						err = errors.New("active read did not return ErrWebSocketReadInterrupted")
						handlerResult <- err
						return err
					}
				case <-time.After(time.Second):
					err := errors.New("timed out waiting for interrupted read")
					handlerResult <- err
					return err
				}

				var next map[string]any
				if err := ws.ReadJSON(&next); !errors.Is(err, router.ErrWebSocketReadInterrupted) {
					err = errors.New("future read did not return ErrWebSocketReadInterrupted")
					handlerResult <- err
					return err
				}
				if err := interrupter.InterruptRead(); err != nil {
					handlerResult <- err
					return err
				}

				handlerResult <- nil
				return nil
			})
			defer shutdown()

			conn, response, err := websocket.DefaultDialer.Dial(url, nil)
			defer closeWebSocketResponse(t, response)
			if err != nil {
				t.Fatalf("failed to dial websocket: %v", err)
			}
			defer closeWebSocketConn(t, conn)

			select {
			case err := <-handlerResult:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for websocket handler")
			}
		})
	}
}
