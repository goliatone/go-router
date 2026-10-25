package router_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	"github.com/goliatone/go-router"
)

// Keep lifecycle expectations executable before introducing another backend.
type lifecycleServer interface {
	Init()
	Serve(string) error
	Shutdown(context.Context) error
}

type lifecycleFixture struct {
	server lifecycleServer
	get    func(string, router.HandlerFunc)
}

func lifecycleFixtures() map[string]func() lifecycleFixture {
	return map[string]func() lifecycleFixture{
		"fiber-v3": func() lifecycleFixture {
			s := router.NewFiberV3Adapter(func(app *fiber3.App) *fiber3.App { return app })
			return lifecycleFixture{s, func(path string, h router.HandlerFunc) { s.Router().Get(path, h) }}
		},
		"fiber-v2": func() lifecycleFixture {
			s := router.NewFiberAdapter(func(app *fiber.App) *fiber.App { return app })
			return lifecycleFixture{s, func(path string, h router.HandlerFunc) { s.Router().Get(path, h) }}
		},
		"httprouter": func() lifecycleFixture {
			s := router.NewHTTPServer()
			return lifecycleFixture{s, func(path string, h router.HandlerFunc) { s.Router().Get(path, h) }}
		},
	}
}

func lifecycleAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func lifecycleReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("lifecycle operation did not complete")
		var zero T
		return zero
	}
}

func TestServerLifecycleStartupFailure(t *testing.T) {
	for name, factory := range lifecycleFixtures() {
		t.Run(name, func(t *testing.T) {
			l, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			f := factory()
			f.get("/ready", func(c router.Context) error { return c.SendString("ready") })
			f.server.Init()
			f.server.Init()
			done := make(chan error, 1)
			go func() { done <- f.server.Serve(l.Addr().String()) }()
			if err := lifecycleReceive(t, done); err == nil {
				t.Fatal("occupied address must return a startup error")
			} else {
				if _, ok := errors.AsType[*net.OpError](err); !ok {
					t.Fatalf("startup error lost its network cause: %v", err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := f.server.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServerLifecycleDrainAndDeadline(t *testing.T) {
	for name, factory := range lifecycleFixtures() {
		for _, deadline := range []bool{false, true} {
			mode := "drain"
			if deadline {
				mode = "deadline"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				f := factory()
				entered := make(chan struct{})
				release := make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				f.get("/ready", func(c router.Context) error { return c.SendString("ready") })
				f.get("/held", func(c router.Context) error {
					close(entered)
					<-release
					return c.SendString("completed")
				})
				address := lifecycleAddress(t)
				serveDone := make(chan error, 1)
				go func() { serveDone <- f.server.Serve(address) }()
				client := &http.Client{Timeout: 2 * time.Second}
				t.Cleanup(func() {
					unblock()
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := f.server.Shutdown(ctx); err != nil {
						t.Errorf("cleanup shutdown: %v", err)
					}
					// HTTPServer returns ErrServerClosed; Fiber returns nil.
					if err := lifecycleReceive(t, serveDone); err != nil && !errors.Is(err, http.ErrServerClosed) {
						t.Errorf("serve: %v", err)
					}
					client.CloseIdleConnections()
				})
				readyUntil := time.Now().Add(2 * time.Second)
				for {
					resp, err := client.Get("http://" + address + "/ready")
					if err == nil {
						body, readErr := io.ReadAll(resp.Body)
						resp.Body.Close()
						if readErr != nil || string(body) != "ready" || resp.StatusCode != http.StatusOK {
							t.Fatalf("readiness response: %q, %v", body, readErr)
						}
						break
					}
					if time.Now().After(readyUntil) {
						t.Fatal(err)
					}
					time.Sleep(time.Millisecond)
				}
				response := make(chan error, 1)
				go func() {
					resp, err := client.Get("http://" + address + "/held")
					if err == nil {
						defer resp.Body.Close()
						var body []byte
						body, err = io.ReadAll(resp.Body)
						if err == nil && (string(body) != "completed" || resp.StatusCode != http.StatusOK) {
							err = errors.New("in-flight response was not completed")
						}
					}
					response <- err
				}()
				lifecycleReceive(t, entered)
				timeout := 2 * time.Second
				if deadline {
					timeout = 80 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				defer cancel()
				shutdownDone := make(chan error, 1)
				go func() { shutdownDone <- f.server.Shutdown(ctx) }()
				if deadline {
					if err := lifecycleReceive(t, shutdownDone); !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("shutdown deadline: %v", err)
					}
					// Deadline expiry does not abort the handler; the host still owns it.
					unblock()
				} else {
					select {
					case err := <-shutdownDone:
						t.Fatalf("shutdown returned before the request completed: %v", err)
					case <-time.After(20 * time.Millisecond):
					}
					unblock()
					if err := lifecycleReceive(t, shutdownDone); err != nil {
						t.Fatal(err)
					}
				}
				if err := lifecycleReceive(t, response); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
