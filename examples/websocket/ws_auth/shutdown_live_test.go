package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func liveShutdownFixture[T any](server router.Server[T], cfg router.WebSocketConfig, handler func(router.WebSocketContext) error) demoServer {
	server.Router().WebSocket("/ws", cfg, handler)
	return server
}

func TestDemoShutdownLive(t *testing.T) {
	factories := map[string]func(router.WebSocketConfig, func(router.WebSocketContext) error) demoServer{
		"fiber-v3": func(cfg router.WebSocketConfig, handler func(router.WebSocketContext) error) demoServer {
			return liveShutdownFixture(router.NewFiberV3Adapter(), cfg, handler)
		},
		"httprouter": func(cfg router.WebSocketConfig, handler func(router.WebSocketContext) error) demoServer {
			return liveShutdownFixture(router.NewHTTPServer(), cfg, handler)
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			tracker := newSocketTracker()
			disconnected := make(chan struct{}, 1)
			cfg := router.DefaultWebSocketConfig()
			cfg.DisableReadDeadline, cfg.DisableKeepAlive = true, true
			cfg.OnDisconnect = func(router.WebSocketContext, error) {
				tracker.stop() // adapter callback reentry during tracked cleanup
				disconnected <- struct{}{}
			}
			server := factory(cfg, tracker.track(func(ws router.WebSocketContext) error {
				for {
					kind, data, err := ws.ReadMessage()
					if err != nil {
						return nil
					}
					if err := ws.WriteMessage(kind, data); err != nil {
						return err
					}
				}
			}))
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			host, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			var runErr error
			go func() {
				runErr = tracker.run(host, server, address, 2*time.Second)
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
					if runErr != nil {
						t.Error(runErr)
					}
				case <-time.After(3 * time.Second):
					t.Error("demo did not stop")
				}
			})

			dialer := websocket.Dialer{HandshakeTimeout: time.Second}
			var conn *websocket.Conn
			for deadline := time.Now().Add(2 * time.Second); ; {
				candidate, response, dialErr := dialer.DialContext(t.Context(), "ws://"+address+"/ws", nil)
				if response != nil && response.Body != nil {
					if err := response.Body.Close(); err != nil {
						t.Error(err)
					}
				}
				if dialErr == nil {
					conn = candidate
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(dialErr)
				}
				time.Sleep(time.Millisecond)
			}
			t.Cleanup(func() {
				if err := conn.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte("drain")); err != nil {
				t.Fatal(err)
			}
			_, payload, err := conn.ReadMessage()
			if err != nil || string(payload) != "drain" {
				t.Fatalf("echo = %q, error = %v", payload, err)
			}
			cancel() // shut down while a real connection is blocked in its next read
			select {
			case <-done:
				if runErr != nil {
					t.Fatal(runErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown with an active socket timed out")
			}
			select {
			case <-disconnected:
			case <-time.After(time.Second):
				t.Fatal("disconnect callback did not finish")
			}
			requireDrained(t, tracker)
		})
	}
}
