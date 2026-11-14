package router_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	router "github.com/goliatone/go-router"
)

func TestManagedPipelinedDrain(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, bounded := range []bool{false, true} {
			for _, pipelined := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/bounded=%t/pipelined=%t", major, bounded, pipelined), func(t *testing.T) {
					testManagedPipeline(t, major, bounded, pipelined, "buffered", true)
				})
			}
		}
	}
}

func TestManagedStreamPipelinedDrain(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, bounded := range []bool{false, true} {
			for _, body := range []string{"fixed", "chunked"} {
				for _, pipelined := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/bounded=%t/%s/pipelined=%t", major, bounded, body, pipelined), func(t *testing.T) {
						testManagedPipeline(t, major, bounded, pipelined, body, true)
					})
				}
			}
		}
	}
}

// Flushing each response must preserve normal pipelined keepalive requests.
func TestManagedPipelinedResponses(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, bounded := range []bool{false, true} {
			for _, body := range []string{"buffered", "fixed", "chunked"} {
				t.Run(fmt.Sprintf("%s/bounded=%t/%s", major, bounded, body), func(t *testing.T) {
					testManagedPipeline(t, major, bounded, true, body, false)
				})
			}
		}
	}
}

type managedHeldReader struct {
	io.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *managedHeldReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return r.Reader.Read(p)
}

func testManagedPipeline(t *testing.T, major string, bounded, pipelined bool, body string, stopDuringResponse bool) {
	t.Helper()
	const payload = "admitted-response"
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var calls atomic.Int32
	handler := func(c router.Context) error {
		first := calls.Add(1) == 1
		if body == "buffered" {
			if first {
				close(entered)
				<-release
			}
			return c.SendString(payload)
		}
		var reader io.Reader = strings.NewReader(payload)
		if first {
			// Native serialization reads this only after the handler has returned.
			reader = &managedHeldReader{Reader: reader, entered: entered, release: release}
		}
		if body == "fixed" {
			// fasthttp recognizes LimitedReader's length without consuming its body.
			reader = io.LimitReader(reader, int64(len(payload)))
		}
		return c.SendStream(reader)
	}
	managed := managedPipelineServer(t, major, bounded, handler)
	run, err := managed.StartManaged(context.Background(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unblock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := run.Stop(ctx); err != nil {
			t.Errorf("cleanup Stop: %v", err)
		}
		if err := run.Wait(ctx); err != nil {
			t.Errorf("cleanup Wait: %v", err)
		}
	})
	select {
	case <-run.Serving():
	case <-run.Done():
		t.Fatalf("startup: %v", run.Wait(context.Background()))
	case <-time.After(3 * time.Second):
		t.Fatal("startup timeout")
	}
	conn, err := net.DialTimeout("tcp", run.Address(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	wire := "GET /held HTTP/1.1\r\nHost: test\r\n\r\n"
	if pipelined {
		wire += wire
	}
	if _, err := io.WriteString(conn, wire); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("response did not reach hold point")
	}
	if stopDuringResponse {
		// A caller timeout must not shorten the shared drain budget.
		stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err = run.Stop(stopCtx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("caller wait should expire while response is held: %v", err)
		}
	}
	unblock()
	reader := bufio.NewReader(conn)
	readResponse := func() {
		t.Helper()
		res, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
		if err != nil {
			t.Fatalf("admitted response lost: %v (calls=%d)", err, calls.Load())
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 || string(data) != payload {
			t.Fatalf("admitted response: status=%d body=%q err=%v", res.StatusCode, data, err)
		}
		if body == "chunked" && (len(res.TransferEncoding) != 1 || res.TransferEncoding[0] != "chunked") {
			t.Fatalf("expected chunked framing: %v", res.TransferEncoding)
		}
		if body == "fixed" && res.ContentLength != int64(len(payload)) {
			t.Fatalf("expected fixed length: %d", res.ContentLength)
		}
	}
	readResponse()
	wantCalls := int32(1)
	if !stopDuringResponse {
		readResponse()
		wantCalls = 2
	} else {
		// Either a complete rejection or clean EOF is valid for unadmitted work.
		_, err = reader.Peek(1)
		if err == nil {
			res, err := http.ReadResponse(reader, &http.Request{Method: "GET"})
			if err != nil {
				t.Fatalf("partial drain rejection: %v", err)
			}
			_, readErr := io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != 503 || readErr != nil {
				t.Fatalf("drain rejection: status=%d err=%v", res.StatusCode, readErr)
			}
		} else if !errors.Is(err, io.EOF) {
			t.Fatalf("connection did not close cleanly: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := run.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := run.Wait(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := calls.Load(); got != wantCalls {
		t.Errorf("handler calls: got %d want %d", got, wantCalls)
	}
}

// A timed-out drain closes the owned connection and cancels the stream context,
// but completion still waits for a noncooperative reader to return.
func TestManagedStreamDrainDeadline(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, bounded := range []bool{false, true} {
			for _, body := range []string{"fixed", "chunked"} {
				t.Run(fmt.Sprintf("%s/bounded=%t/%s", major, bounded, body), func(t *testing.T) {
					entered, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					contexts := make(chan context.Context, 1)
					var calls atomic.Int32
					handler := func(c router.Context) error {
						if calls.Add(1) != 1 {
							return c.SendString("unadmitted")
						}
						contexts <- c.Context()
						var reader io.Reader = &managedHeldReader{Reader: strings.NewReader("held"), entered: entered, release: release}
						if body == "fixed" {
							reader = io.LimitReader(reader, 4)
						}
						return c.SendStream(reader)
					}
					managed := managedPipelineServer(t, major, bounded, handler)
					run, err := managed.StartManaged(context.Background(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: 60 * time.Millisecond})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						unblock()
						ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()
						if err := run.Stop(ctx); err != nil && !errors.Is(err, router.ErrManagedDrainTimeout) {
							t.Errorf("cleanup Stop: %v", err)
						}
						if err := run.Wait(ctx); err != nil && !errors.Is(err, router.ErrManagedDrainTimeout) {
							t.Errorf("cleanup Wait: %v", err)
						}
					})
					select {
					case <-run.Serving():
					case <-run.Done():
						t.Fatalf("startup: %v", run.Wait(context.Background()))
					case <-time.After(3 * time.Second):
						t.Fatal("startup timeout")
					}
					conn, err := net.DialTimeout("tcp", run.Address(), time.Second)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
						t.Fatal(err)
					}
					if _, err := io.WriteString(conn, strings.Repeat("GET /held HTTP/1.1\r\nHost: test\r\n\r\n", 2)); err != nil {
						t.Fatal(err)
					}
					select {
					case <-entered:
					case <-time.After(3 * time.Second):
						t.Fatal("stream never entered serialization")
					}
					requestContext := <-contexts
					wait, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					if err := run.Stop(wait); !errors.Is(err, router.ErrManagedDrainTimeout) || !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("drain timeout: %v", err)
					}
					select {
					case <-requestContext.Done():
					default:
						t.Error("stream context survived deadline")
					}
					select {
					case <-run.Done():
						t.Error("completion preceded reader release")
					default:
					}
					if _, err := io.Copy(io.Discard, conn); err != nil {
						t.Fatalf("forced connection close: %v", err)
					}
					unblock()
					if err := run.Wait(wait); !errors.Is(err, router.ErrManagedDrainTimeout) {
						t.Fatalf("terminal drain error: %v", err)
					}
					if calls.Load() != 1 {
						t.Fatalf("unadmitted pipeline dispatched: %d", calls.Load())
					}
				})
			}
		}
	}
}

func managedPipelineServer(t *testing.T, major string, bounded bool, handler router.HandlerFunc) router.ManagedServer {
	t.Helper()
	var managed router.ManagedServer
	var ingress router.BoundedIngressServer
	if major == "v2" {
		s := router.NewFiberAdapter()
		s.Router().Get("/held", handler)
		managed, ingress = s.(router.ManagedServer), s.(router.BoundedIngressServer)
	} else {
		s := router.NewFiberV3Adapter()
		s.Router().Get("/held", handler)
		managed, ingress = s.(router.ManagedServer), s.(router.BoundedIngressServer)
	}
	if bounded {
		if err := ingress.ConfigureBoundedIngress(router.BoundedIngressOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	return managed
}
