package router

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
	"github.com/gorilla/websocket"
)

// These hooks mutate during adapter-owned native calls, before finalization.
// The closure-swap case shares a code address with the intended handler.
func TestManagedNativeHookOwnership(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, mutation := range []string{"route", "name", "closure-swap", "observe", "auto-head"} {
			if major == "v2" && mutation == "auto-head" {
				continue
			}
			t.Run(major+"/"+mutation, func(t *testing.T) {
				var server ManagedServer
				if major == "v2" {
					var earlier f2.Handler
					s := NewFiberAdapter(func(app *f2.App) *f2.App {
						app.Hooks().OnRoute(func(r f2.Route) error {
							if r.Path == "/earlier" {
								earlier = r.Handlers[0]
							}
							if r.Path == "/owned" {
								if mutation == "route" {
									r.Handlers[0] = func(c *f2.Ctx) error { return c.SendString("unowned") }
								}
								if mutation == "closure-swap" {
									r.Handlers[0] = earlier
								}
							}
							return nil
						})
						app.Hooks().OnName(func(r f2.Route) error {
							if mutation == "name" {
								r.Handlers[0] = func(c *f2.Ctx) error { return c.SendString("unowned") }
							}
							return nil
						})
						return app
					})
					s.Router().Get("/earlier", func(c Context) error { return c.SendString("earlier") })
					s.Router().Get("/owned", func(c Context) error { return c.SendString("owned") }).SetName("owned")
					server = s.(ManagedServer)
				} else {
					var earlier f3.Handler
					s := NewFiberV3Adapter(func(app *f3.App) *f3.App {
						app.Hooks().OnRoute(func(r f3.Route) error {
							if r.Path == "/earlier" {
								earlier = r.Handlers[0]
							}
							if r.Path == "/owned" {
								if mutation == "route" || mutation == "auto-head" && r.Method == "HEAD" {
									r.Handlers[0] = func(c f3.Ctx) error { return c.SendString("unowned") }
								}
								if mutation == "closure-swap" {
									r.Handlers[0] = earlier
								}
							}
							return nil
						})
						app.Hooks().OnName(func(r f3.Route) error {
							if mutation == "name" {
								r.Handlers[0] = func(c f3.Ctx) error { return c.SendString("unowned") }
							}
							return nil
						})
						return app
					})
					s.Router().Get("/earlier", func(c Context) error { return c.SendString("earlier") })
					s.Router().Get("/owned", func(c Context) error { return c.SendString("owned") }).SetName("owned")
					server = s.(ManagedServer)
				}
				run := managedStart(t, managedFixture{server: server}, context.Background(), managedOptions())
				if mutation == "observe" {
					managedReady(t, run)
					if body := managedRequest(t, run.Address(), "/owned"); body != "owned" {
						t.Fatal(body)
					}
					if major == "v3" {
						client := http.Client{Timeout: time.Second}
						resp, err := client.Head("http://" + run.Address() + "/owned")
						if err != nil {
							t.Fatal(err)
						}
						resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							t.Fatal(resp.StatusCode)
						}
					}
					return
				}
				managedAwait(t, run.Done())
				if err := run.Wait(context.Background()); !errors.Is(err, ErrManagedNativeRegistration) {
					t.Fatalf("expected native ownership rejection, got %v", err)
				}
				select {
				case <-run.Bound():
					t.Fatal("unsupported hook mutation reached bind")
				default:
				}
				select {
				case <-run.Serving():
					t.Fatal("unsupported hook mutation reached serving")
				default:
				}
			})
		}
	}
}

func TestManagedV2WebSocketLifetime(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		name := "retained-after-return"
		if overlap {
			name = "close-overlaps-return"
		}
		t.Run(name, func(t *testing.T) {
			saved := make(chan WebSocketContext, 1)
			returnHandler, releaseClose := make(chan struct{}), make(chan struct{})
			closeEntered := make(chan struct{})
			var returnOnce, closeOnce sync.Once
			finishHandler := func() { returnOnce.Do(func() { close(returnHandler) }) }
			finishClose := func() { closeOnce.Do(func() { close(releaseClose) }) }
			defer finishHandler()
			defer finishClose()
			cfg := DefaultWebSocketConfig()
			cfg.DisableReadDeadline = true
			if overlap {
				cfg.OnDisconnect = func(WebSocketContext, error) { close(closeEntered); <-releaseClose }
			}
			s := NewFiberAdapter()
			s.Router().WebSocket("/ws", cfg, func(c WebSocketContext) error { saved <- c; <-returnHandler; return nil })
			run := managedStart(t, managedFixture{server: s.(ManagedServer)}, context.Background(), managedOptions())
			managedReady(t, run)
			client, _, err := websocket.DefaultDialer.Dial("ws://"+run.Address()+"/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			c := <-saved
			closeDone := make(chan struct{})
			if overlap {
				go func() { defer close(closeDone); _ = c.Close() }()
				managedAwait(t, closeEntered)
			}
			finishHandler()
			_ = client.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := client.ReadMessage(); err == nil {
				t.Fatal("handler return left transport open")
			}
			if c.IsConnected() {
				t.Fatal("retained context still connected")
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.WriteMessage(TextMessage, []byte("late")); err == nil {
				t.Fatal("late write accepted")
			}
			if err := c.SetReadDeadline(time.Now()); err == nil {
				t.Fatal("late deadline accepted")
			}
			c.SetContext(context.Background())
			_ = c.Context()
			if overlap {
				finishClose()
				managedAwait(t, closeDone)
			}
			if err := run.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			managedAwait(t, run.Done())
		})
	}
}

func TestManagedRegistrationDefaultMiddlewareAndHead(t *testing.T) {
	t.Run("v2", func(t *testing.T) { testManagedRegistrationDefaults(t, NewFiberAdapter()) })
	t.Run("v3", func(t *testing.T) { testManagedRegistrationDefaults(t, NewFiberV3Adapter()) })
}

func testManagedRegistrationDefaults[T any](t *testing.T, s Server[T]) {
	s.Router().Get("/", func(c Context) error { return c.SendString("root") }).SetName("root")
	s.Router().Group("/api").Get("/item", func(c Context) error { return c.SendString("item") }).SetName("item")
	s.Router().Head("/api/item", func(c Context) error { c.SetHeader("X-Explicit-Head", "yes"); return nil })
	s.Router().(MissHandlerRegistrar).HandleMiss(GET, func(c Context) error { return c.SendString("miss") })
	run := managedStart(t, managedFixture{server: s.(ManagedServer)}, context.Background(), managedOptions())
	managedReady(t, run)
	for path, want := range map[string]string{"/": "root", "/api/item": "item", "/missing": "miss"} {
		if got := managedRequest(t, run.Address(), path); got != want {
			t.Fatalf("%s: %q", path, got)
		}
	}
	client := http.Client{Timeout: time.Second}
	resp, err := client.Head("http://" + run.Address() + "/api/item")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Explicit-Head") != "yes" {
		t.Fatalf("HEAD: %d %v", resp.StatusCode, resp.Header)
	}
}
