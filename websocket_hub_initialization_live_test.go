package router_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/gorilla/websocket"
)

func initializationLiveFixture[T any](s router.Server[T], handler router.HandlerFunc, cfg router.WebSocketConfig) lifecycleFixture {
	s.Router().Get("/ws", handler, router.WebSocketUpgrade(cfg))
	return lifecycleFixture{s, func(path string, h router.HandlerFunc) { s.Router().Get(path, h) }}
}

func TestWSHubInitializationLiveBackends(t *testing.T) {
	factories := map[string]func(router.HandlerFunc, router.WebSocketConfig) lifecycleFixture{
		"fiber-v2": func(h router.HandlerFunc, c router.WebSocketConfig) lifecycleFixture {
			return initializationLiveFixture(router.NewFiberAdapter(), h, c)
		},
		"fiber-v3": func(h router.HandlerFunc, c router.WebSocketConfig) lifecycleFixture {
			return initializationLiveFixture(router.NewFiberV3Adapter(), h, c)
		},
		"httprouter": func(h router.HandlerFunc, c router.WebSocketConfig) lifecycleFixture {
			return initializationLiveFixture(router.NewHTTPServer(), h, c)
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			hub := router.NewWSHub()
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			entered := make(chan struct{}, 2)
			stopped := make(chan error, 2)
			checkTestError(t, hub.OnConnect(func(ctx context.Context, client router.WSClient, _ any) error {
				// Exercise hub reentry and enough sends to fill an undrained queue.
				_ = hub.Clients()
				for range 300 {
					if err := client.Send([]byte("welcome")); err != nil {
						return err
					}
				}
				entered <- struct{}{}
				if client.Query("user") == "slow" {
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				client.Set("user", client.Query("user"))
				return nil
			}))
			checkTestError(t, hub.OnConnect(func(_ context.Context, client router.WSClient, _ any) error {
				if client.GetString("user") == "" {
					return errors.New("initializers ran out of order")
				}
				checkTestError(t, client.OnMessage(func(context.Context, []byte) error {
					return client.OnMessage(func(context.Context, []byte) error { return nil })
				}))
				return client.OnJSON("message", func(_ context.Context, data json.RawMessage) error {
					return client.Send([]byte(fmt.Sprintf("%s:%s", client.GetString("user"), data)))
				})
			}))
			cfg := router.DefaultWebSocketConfig()
			cfg.DisableKeepAlive, cfg.DisableReadDeadline = true, true
			disconnected := make(chan int, 2)
			cfg.OnDisconnect = func(router.WebSocketContext, error) {
				// Adapter close callbacks must be able to query the hub while
				// its Close call is still on the stack.
				disconnected <- hub.ClientCount()
				_ = hub.Clients()
			}
			handler := func(c router.Context) error {
				err := hub.Handler()(c)
				stopped <- err
				return err
			}
			f := factory(handler, cfg)
			f.get("/ready", func(c router.Context) error { return c.SendString("ready") })
			address := lifecycleAddress(t)
			serveDone := make(chan error, 1)
			go func() { serveDone <- f.server.Serve(address) }()
			t.Cleanup(func() {
				unblock()
				checkTestError(t, hub.Close())
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				checkTestError(t, f.server.Shutdown(ctx))
				if err := lifecycleReceive(t, serveDone); err != nil && !errors.Is(err, http.ErrServerClosed) {
					t.Error(err)
				}
			})
			httpClient := &http.Client{Timeout: time.Second}
			defer httpClient.CloseIdleConnections()
			for deadline := time.Now().Add(2 * time.Second); ; {
				resp, err := testHTTPGet(t, httpClient, "http://"+address+"/ready")
				if err == nil {
					closeWebSocketResponse(t, resp)
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
			dial := func(user string) *websocket.Conn {
				conn, resp, err := websocket.DefaultDialer.Dial("ws://"+address+"/ws?user="+user, nil)
				closeWebSocketResponse(t, resp)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { closeWebSocketConn(t, conn) })
				checkTestError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"message","data":"first"}`)))
				checkTestError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
				return conn
			}
			read := func(conn *websocket.Conn, want string) {
				for range 301 {
					_, data, err := conn.ReadMessage()
					if err != nil {
						t.Fatal(err)
					}
					if string(data) == want {
						return
					}
				}
				t.Fatalf("missing reply %q", want)
			}
			slow := dial("slow")
			lifecycleReceive(t, entered)
			fast := dial("fast")
			lifecycleReceive(t, entered)
			read(fast, `fast:"first"`)
			unblock()
			read(slow, `slow:"first"`)
			checkTestError(t, slow.WriteMessage(websocket.TextMessage, []byte(`{"type":"message","data":"next"}`)))
			read(slow, `slow:"next"`)
			// Neither peer sends a close response: the hub must stop its own readers.
			checkTestError(t, hub.Close())
			for range 2 {
				checkTestError(t, lifecycleReceive(t, stopped))
				if count := lifecycleReceive(t, disconnected); count != 0 {
					t.Errorf("disconnect callback saw %d registered clients during shutdown", count)
				}
			}
		})
	}
}
