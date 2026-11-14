package router_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/goliatone/go-router/eventstream"
	"github.com/goliatone/go-router/ssefiber"
)

// Exercise the optional capability from an external consumer while retaining
// the constructors' unchanged required Server contract and default options.
func TestManagedExternalConsumer(t *testing.T) {
	t.Run("v2", func(t *testing.T) { testManagedConsumer(t, router.NewFiberAdapter()) })
	t.Run("v3", func(t *testing.T) { testManagedConsumer(t, router.NewFiberV3Adapter()) })
}
func testManagedConsumer[T any](t *testing.T, server router.Server[T]) {
	server.Router().Get("/healthz", func(c router.Context) error { return c.SendString("ready") })
	managed, ok := server.(router.ManagedServer)
	if !ok {
		t.Fatal("missing optional capability")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run, err := managed.StartManaged(ctx, router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = run.Stop(stopCtx)
		_ = run.Wait(stopCtx)
	}()
	select {
	case <-run.Serving():
	case <-run.Done():
		t.Fatal(run.Wait(ctx))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + run.Address() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || string(body) != "ready" {
		t.Fatalf("%d %q %v", resp.StatusCode, body, err)
	}
	if err := run.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestManagedSSEConsumerContext(t *testing.T) {
	t.Run("v2", func(t *testing.T) { testManagedSSEConsumer(t, router.NewFiberAdapter()) })
	t.Run("v3", func(t *testing.T) { testManagedSSEConsumer(t, router.NewFiberV3Adapter()) })
}
func testManagedSSEConsumer[T any](t *testing.T, server router.Server[T]) {
	stream := eventstream.New()
	cancels := make(chan context.CancelFunc, 1)
	err := ssefiber.Mount(server.Router(), ssefiber.WithStream(stream), ssefiber.WithScopeResolver(func(router.Context) (eventstream.Scope, error) { return eventstream.Scope{"tenant": "test"}, nil }), ssefiber.WithHeartbeatInterval(10*time.Millisecond), ssefiber.WithHeartbeatBounds(time.Millisecond, time.Second), ssefiber.WithMiddlewares(func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			ctx, cancel := context.WithCancel(c.Context())
			cancels <- cancel
			c.SetContext(ctx)
			return next(c)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	stopStream := func() {
		select {
		case cancel := <-cancels:
			cancel()
		default:
		}
	}
	opts := router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second, OnShutdown: func(context.Context) error { stopStream(); return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run, err := server.(router.ManagedServer).StartManaged(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopStream()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = run.Stop(stopCtx)
		_ = run.Wait(stopCtx)
	}()
	select {
	case <-run.Serving():
	case <-run.Done():
		t.Fatal(run.Wait(ctx))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	conn, err := net.DialTimeout("tcp", run.Address(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprint(conn, "GET /events HTTP/1.1\r\nHost: localhost\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	lines := bufio.NewReader(response.Body)
	heartbeat := false
	for i := 0; i < 20; i++ {
		line, err := lines.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "event: heartbeat") {
			heartbeat = true
		}
		if heartbeat && line == "\n" {
			break
		}
	}
	if !heartbeat || stream.SnapshotStats().ActiveSubscribers != 1 {
		t.Fatal("stream canceled at handler return")
	}
	if err := run.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := run.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if stream.SnapshotStats().ActiveSubscribers != 0 {
		t.Fatal("subscription not released")
	}
}
