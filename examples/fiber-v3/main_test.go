package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
	"github.com/goliatone/go-router/eventstream"
	"github.com/gorilla/websocket"
)

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle did not finish")
	}
	var zero T
	return zero
}
func host(t *testing.T) *exampleHost {
	t.Helper()
	h, err := newExampleHost()
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func start(t *testing.T, h *exampleHost, parent context.Context, drain time.Duration, onBound func(context.Context, string) error) router.ManagedRun {
	t.Helper()
	r, err := h.start(parent, router.ManagedOptions{Address: "127.0.0.1:0", Network: "tcp4", DrainTimeout: drain, OnBound: onBound})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
		_ = r.Wait(ctx)
	})
	return r
}
func ready(t *testing.T, r router.ManagedRun) {
	t.Helper()
	select {
	case <-r.Serving():
	case <-r.Done():
		t.Fatalf("startup: %v", r.Wait(context.Background()))
	case <-time.After(5 * time.Second):
		t.Fatal("not serving")
	}
}
func client(t *testing.T) *http.Client {
	t.Helper()
	c := &http.Client{Timeout: 4 * time.Second}
	t.Cleanup(c.CloseIdleConnections)
	return c
}
func socket(t *testing.T, address, path string) *websocket.Conn {
	t.Helper()
	c, res, err := websocket.DefaultDialer.Dial("ws://"+address+path, nil)
	if res != nil {
		res.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func echo(t *testing.T, c *websocket.Conn) {
	t.Helper()
	if err := c.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil || string(data) != "hello" {
		t.Fatalf("echo %q: %v", data, err)
	}
}
func closed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err := c.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) || (closeErr.Code != 1000 && closeErr.Code != 1001) {
		t.Fatalf("unclean close: %v", err)
	}
}
func waitSubscribers(t *testing.T, h *exampleHost, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if h.stream.SnapshotStats().ActiveSubscribers == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscribers: %+v", h.stream.SnapshotStats())
}
func publish(h *exampleHost, message string) eventstream.Record {
	return h.stream.Publish(eventstream.Scope{"demo": "public"}, eventstream.Event{Name: "message", Payload: []byte(fmt.Sprintf("%q", message))})
}
func openSSE(t *testing.T, h *exampleHost, r router.ManagedRun, path string) *http.Response {
	t.Helper()
	type result struct {
		response *http.Response
		err      error
	}
	done := make(chan result, 1)
	c := client(t)
	go func() { res, err := c.Get("http://" + r.Address() + path); done <- result{res, err} }()
	waitSubscribers(t, h, 1)
	publish(h, "connected")
	res := await(t, done)
	if res.err != nil {
		t.Fatal(res.err)
	}
	t.Cleanup(func() { res.response.Body.Close() })
	if res.response.StatusCode != 200 || res.response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE: %s", res.response.Status)
	}
	return res.response
}

func TestHostRoutesAndOwnedCleanup(t *testing.T) {
	h := host(t)
	r := start(t, h, context.Background(), 2*time.Second, nil)
	ready(t, r)
	res, err := client(t).Get("http://" + r.Address() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || res.Header.Get("X-Backend") != "fiber-v3" || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("health %q %v", body, err)
	}
	ws := socket(t, r.Address(), "/echo")
	echo(t, ws)
	first := publish(h, "first")
	publish(h, "resume-marker")
	events := openSSE(t, h, r, "/events?cursor="+first.Cursor)
	scanner := bufio.NewScanner(events.Body)
	found := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "resume-marker") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("resume missing: %v", scanner.Err())
	}
	eof := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, events.Body); eof <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	closed(t, ws)
	if err := await(t, eof); err != nil {
		t.Fatal(err)
	}
	await(t, h.joined)
	waitSubscribers(t, h, 0)
}

func TestHostHTTPDrain(t *testing.T) {
	for _, mode := range []string{"parent-cancel", "caller-deadline", "drain-deadline"} {
		t.Run(mode, func(t *testing.T) {
			h := host(t)
			entered, release := make(chan context.Context, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			h.server.Router().Get("/held", func(c router.Context) error { entered <- c.Context(); <-release; return c.SendString("complete") })
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			budget := 2 * time.Second
			if mode == "drain-deadline" {
				budget = 80 * time.Millisecond
			}
			r := start(t, h, parent, budget, nil)
			ready(t, r)
			response := make(chan error, 1)
			c := client(t)
			go func() {
				res, err := c.Get("http://" + r.Address() + "/held")
				if err == nil {
					body, e := io.ReadAll(res.Body)
					res.Body.Close()
					err = e
					if err == nil && (res.StatusCode != 200 || string(body) != "complete") {
						err = fmt.Errorf("response %d %q", res.StatusCode, body)
					}
				}
				response <- err
			}()
			requestCtx := await(t, entered)
			if mode == "parent-cancel" {
				cancelParent()
			}
			caller, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if mode == "caller-deadline" {
				cancel()
				caller, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
			}
			defer cancel()
			stopped := make(chan error, 1)
			go func() { stopped <- r.Stop(caller) }()
			await(t, r.Stopping())
			if mode == "drain-deadline" {
				if err := await(t, stopped); !errors.Is(err, router.ErrManagedDrainTimeout) {
					t.Fatalf("drain: %v", err)
				}
				await(t, requestCtx.Done())
			} else {
				select {
				case <-requestCtx.Done():
					t.Fatal("admitted request canceled before drain")
				case <-time.After(40 * time.Millisecond):
				}
				if mode == "caller-deadline" {
					if err := await(t, stopped); !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("caller: %v", err)
					}
				} else {
					select {
					case err := <-stopped:
						t.Fatalf("early drain: %v", err)
					default:
					}
				}
			}
			unblock()
			responseErr := await(t, response)
			if mode != "drain-deadline" && responseErr != nil {
				t.Fatal(responseErr)
			}
			retry, cancelRetry := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelRetry()
			err := r.Wait(retry)
			switch mode {
			case "parent-cancel":
				if !cancellationOnly(err, context.Canceled) {
					t.Fatalf("parent: %v", err)
				}
				if err := await(t, stopped); !cancellationOnly(err, context.Canceled) {
					t.Fatal(err)
				}
			case "caller-deadline":
				if err != nil {
					t.Fatal(err)
				}
				if err := r.Stop(retry); err != nil {
					t.Fatal(err)
				}
			case "drain-deadline":
				if !errors.Is(err, router.ErrManagedDrainTimeout) {
					t.Fatalf("sticky: %v", err)
				}
			}
		})
	}
}

func TestHostHeldWebSocketDeadline(t *testing.T) {
	h := host(t)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	h.server.Router().WebSocket("/held", router.DefaultWebSocketConfig(), func(ws router.WebSocketContext) error {
		done, ok := h.ownSocket(ws)
		if !ok {
			return ws.Close()
		}
		defer done()
		close(entered)
		<-release
		return nil
	})
	r := start(t, h, context.Background(), 80*time.Millisecond, nil)
	ready(t, r)
	ws := socket(t, r.Address(), "/held")
	await(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Stop(ctx); !errors.Is(err, router.ErrManagedDrainTimeout) {
		t.Fatal(err)
	}
	select {
	case <-r.Done():
		t.Fatal("Done before owned worker exited")
	default:
	}
	select {
	case <-h.joined:
		t.Fatal("join before worker exited")
	default:
	}
	unblock()
	if err := r.Wait(ctx); !errors.Is(err, router.ErrManagedDrainTimeout) {
		t.Fatalf("sticky result: %v", err)
	}
	closed(t, ws)
	await(t, h.joined)
}

func TestHostStartupBoundaries(t *testing.T) {
	t.Run("occupied", func(t *testing.T) {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		if err := run(context.Background(), l.Addr().String()); err == nil {
			t.Fatal("startup failure lost")
		}
	})
	t.Run("pre-canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h := host(t)
		r, err := h.start(ctx, router.ManagedOptions{Address: "127.0.0.1:0", Network: "tcp4", DrainTimeout: time.Second})
		if !errors.Is(err, context.Canceled) || r != nil {
			t.Fatalf("pre-cancel: run=%v err=%v", r, err)
		}
		if err := run(ctx, "127.0.0.1:0"); err != nil {
			t.Fatalf("pre-canceled coordinator: %v", err)
		}
	})
	t.Run("stop-during-bound", func(t *testing.T) {
		h := host(t)
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		r := start(t, h, context.Background(), time.Second, func(context.Context, string) error { close(entered); <-release; return nil })
		await(t, entered)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := r.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		unblock()
		wait, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelWait()
		if err := r.Wait(wait); err != nil {
			t.Fatal(err)
		}
		select {
		case <-r.Serving():
			t.Fatal("served after stop")
		default:
		}
		await(t, h.joined)
	})
}

func TestHostSSEDisconnect(t *testing.T) {
	h := host(t)
	r := start(t, h, context.Background(), 2*time.Second, nil)
	ready(t, r)
	events := openSSE(t, h, r, "/events")
	events.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for h.stream.SnapshotStats().ActiveSubscribers != 0 && time.Now().Before(deadline) {
		publish(h, strings.Repeat("x", 8192))
		time.Sleep(10 * time.Millisecond)
	}
	waitSubscribers(t, h, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	await(t, h.joined)
}

func TestHostProcessHelper(t *testing.T) {
	if address := os.Getenv("H034_CHILD"); address != "" {
		flag.CommandLine = flag.NewFlagSet("host", flag.ExitOnError)
		os.Args = []string{os.Args[0], "-address", address}
		main()
		os.Exit(0)
	}
}
func TestHostSignals(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			l, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := l.Addr().String()
			l.Close()
			log, err := os.CreateTemp(t.TempDir(), "host")
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestHostProcessHelper$")
			cmd.Env = append(os.Environ(), "H034_CHILD="+address)
			cmd.Stdout = log
			cmd.Stderr = log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done, finished := make(chan error, 1), make(chan struct{})
			go func() { done <- cmd.Wait(); close(finished) }()
			t.Cleanup(func() {
				select {
				case <-finished:
				default:
					cmd.Process.Kill()
					<-finished
				}
			})
			c := client(t)
			deadline := time.Now().Add(5 * time.Second)
			for {
				res, err := c.Get("http://" + address + "/health")
				if err == nil {
					res.Body.Close()
					if res.StatusCode == 200 {
						break
					}
				}
				if time.Now().After(deadline) {
					data, _ := os.ReadFile(log.Name())
					t.Fatalf("readiness: %s", data)
				}
				time.Sleep(5 * time.Millisecond)
			}
			ws := socket(t, address, "/echo")
			echo(t, ws)
			events, err := c.Get("http://" + address + "/events")
			if err != nil {
				t.Fatal(err)
			}
			defer events.Body.Close()
			if events.StatusCode != 200 || events.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("SSE: %s", events.Status)
			}
			eof := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, events.Body); eof <- err }()
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			closed(t, ws)
			if err := await(t, eof); err != nil {
				t.Fatal(err)
			}
			if err := await(t, done); err != nil {
				data, _ := os.ReadFile(log.Name())
				t.Fatalf("process: %v %s", err, data)
			}
		})
	}
}

func TestCancellationClassification(t *testing.T) {
	cause := errors.New("signal cause")
	independent := errors.New("independent failure")
	if !cancellationOnly(errors.Join(cause, context.Canceled), cause) {
		t.Fatal("expected cancellation not recognized")
	}
	for _, err := range []error{errors.Join(cause, independent), &router.ManagedError{Phase: "on-shutdown", Err: context.Canceled}, independent} {
		if cancellationOnly(err, cause) {
			t.Fatalf("independent diagnostic suppressed: %v", err)
		}
	}
}

// Hold the native close callback after the peer disconnects. Shutdown must join
// that callback without treating the already-closing transport as an error.
func TestHostPeerCloseDuringStop(t *testing.T) {
	h := host(t)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	cfg := router.DefaultWebSocketConfig()
	cfg.OnDisconnect = func(router.WebSocketContext, error) { close(entered); <-release }
	h.server.Router().WebSocket("/closing", cfg, h.echo)
	r := start(t, h, context.Background(), 2*time.Second, nil)
	ready(t, r)
	ws := socket(t, r.Address(), "/closing")
	echo(t, ws)
	ws.Close()
	await(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := r.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close callback not joined: %v", err)
	}
	select {
	case <-h.joined:
		t.Fatal("joined before close callback")
	default:
	}
	unblock()
	retry, cancelRetry := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelRetry()
	if err := r.Wait(retry); err != nil {
		t.Fatalf("normal peer close became shutdown failure: %v", err)
	}
	await(t, h.joined)
}
