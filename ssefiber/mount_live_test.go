package ssefiber_test

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	fiber2 "github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	router "github.com/goliatone/go-router"
	"github.com/goliatone/go-router/eventstream"
	"github.com/goliatone/go-router/ssefiber"
)

type liveSSEServer interface {
	Serve(string) error
	Shutdown(context.Context) error
}
type liveSSEFixture struct {
	server liveSSEServer
	mount  func(...ssefiber.Option) error
}

func liveSSE[T any](s router.Server[T]) liveSSEFixture {
	s.Router().Get("/ready", func(c router.Context) error { return c.SendString("ready") })
	return liveSSEFixture{s, func(opts ...ssefiber.Option) error { return ssefiber.Mount(s.Router(), opts...) }}
}
func TestMountLiveBackendReplayHeartbeatAndCleanup(t *testing.T) {
	factories := map[string]func() liveSSEFixture{
		"fiber-v2": func() liveSSEFixture {
			return liveSSE(router.NewFiberAdapter(func(a *fiber2.App) *fiber2.App { return a }))
		},
		"fiber-v3": func() liveSSEFixture {
			return liveSSE(router.NewFiberV3Adapter(func(a *fiber3.App) *fiber3.App { return a }))
		},
		"httprouter": func() liveSSEFixture { return liveSSE(router.NewHTTPServer()) },
	}
	for name, factory := range factories {
		for _, hostCancel := range []bool{false, true} {
			mode := "disconnect"
			if hostCancel {
				mode = "host-cancel"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				f := factory()
				stream := eventstream.New()
				scope := eventstream.Scope{"tenant": "t1"}
				first := stream.Publish(scope, eventstream.Event{Name: "first"})
				stream.Publish(scope, eventstream.Event{Name: "second"})
				host, cancel := context.WithCancel(context.Background())
				defer cancel()
				err := f.mount(ssefiber.WithStream(stream), ssefiber.WithScopeResolver(staticScopeResolver(scope)), ssefiber.WithHeartbeatInterval(15*time.Millisecond), ssefiber.WithHeartbeatBounds(5*time.Millisecond, time.Second), ssefiber.WithMiddlewares(func(next router.HandlerFunc) router.HandlerFunc {
					return func(c router.Context) error { c.SetContext(host); return next(c) }
				}))
				if err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				address := listener.Addr().String()
				listener.Close()
				serveDone := make(chan error, 1)
				go func() { serveDone <- f.server.Serve(address) }()
				client := &http.Client{Timeout: 3 * time.Second}
				t.Cleanup(func() {
					cancel()
					ctx, c := context.WithTimeout(context.Background(), time.Second)
					defer c()
					if err := f.server.Shutdown(ctx); err != nil {
						t.Error(err)
					}
					select {
					case err := <-serveDone:
						if err != nil && err != http.ErrServerClosed {
							t.Error(err)
						}
					case <-time.After(time.Second):
						t.Error("server did not stop")
					}
					client.CloseIdleConnections()
				})
				deadline := time.Now().Add(time.Second)
				for {
					resp, err := client.Get("http://" + address + "/ready")
					if err == nil {
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						break
					}
					if time.Now().After(deadline) {
						t.Fatal(err)
					}
					time.Sleep(time.Millisecond)
				}
				req, err := http.NewRequest("GET", "http://"+address+"/events?cursor=missing", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Last-Event-ID", first.Cursor)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
					t.Fatal(resp.Status)
				}
				scanner := bufio.NewScanner(resp.Body)
				var text strings.Builder
				for scanner.Scan() {
					text.WriteString(scanner.Text())
					text.WriteByte('\n')
					if strings.Contains(text.String(), "event: heartbeat") {
						break
					}
				}
				if err := scanner.Err(); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(text.String(), "event: second") || strings.Contains(text.String(), "event: first") || strings.Contains(text.String(), "stream_gap") {
					t.Fatal(text.String())
				}
				if hostCancel {
					cancel()
					for scanner.Scan() {
					}
					if err := scanner.Err(); err != nil {
						t.Fatal(err)
					}
				} else {
					resp.Body.Close()
				}
				deadline = time.Now().Add(time.Second)
				for stream.SnapshotStats().ActiveSubscribers != 0 {
					if time.Now().After(deadline) {
						t.Fatal("subscription leaked after stream teardown")
					}
					time.Sleep(time.Millisecond)
				}
			})
		}
	}
}
