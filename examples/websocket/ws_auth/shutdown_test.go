package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/goliatone/go-router"
)

type shutdownSocket struct {
	router.WebSocketContext
	closeFn     func() error
	interruptFn func() error
}

func (s *shutdownSocket) Close() error {
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *shutdownSocket) InterruptRead() error {
	if s.interruptFn != nil {
		return s.interruptFn()
	}
	return nil
}

type shutdownServer struct {
	serveFn    func() error
	shutdownFn func(context.Context) error
}

func (s shutdownServer) Serve(string) error                 { return s.serveFn() }
func (s shutdownServer) Shutdown(ctx context.Context) error { return s.shutdownFn(ctx) }

func testShutdownServer() (shutdownServer, <-chan struct{}) {
	started, stopped := make(chan struct{}), make(chan struct{})
	return shutdownServer{
		serveFn: func() error { close(started); <-stopped; return nil },
		shutdownFn: func(ctx context.Context) error {
			close(stopped)
			return ctx.Err()
		},
	}, started
}

func requirePending(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("drain completed before worker cleanup")
	default:
	}
}

func requireDrained(t *testing.T, tracker *socketTracker) {
	t.Helper()
	_, done := tracker.stop()
	select {
	case <-done:
	default:
		t.Fatal("worker completion was lost")
	}
	tracker.Lock()
	defer tracker.Unlock()
	if tracker.workers != 0 || len(tracker.readers) != 0 {
		t.Fatalf("retained workers=%d readers=%d", tracker.workers, len(tracker.readers))
	}
}

func TestSocketTrackerEmptyAndIdleDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		empty := newSocketTracker()
		requireDrained(t, empty)
		requireDrained(t, empty) // repeated stop cannot close the channel twice
		closeErr := errors.New("close failure")
		err := empty.track(func(router.WebSocketContext) error {
			t.Error("stopped tracker admitted a handler")
			return nil
		})(&shutdownSocket{closeFn: func() error { return closeErr }})
		if !errors.Is(err, closeErr) {
			t.Fatalf("rejected socket close error = %v", err)
		}

		tracker := newSocketTracker()
		for range 2 {
			if err := tracker.track(func(router.WebSocketContext) error { return nil })(&shutdownSocket{}); err != nil {
				t.Fatal(err)
			}
			requirePending(t, tracker.drained) // temporary idleness is not shutdown
		}
		requireDrained(t, tracker)
	})
}

func TestSocketTrackerAdmissionRacesStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracker := newSocketTracker()
		start, release := make(chan struct{}), make(chan struct{})
		var workers sync.WaitGroup
		var admitted, closed atomic.Int32
		for range 32 {
			workers.Go(func() {
				<-start
				socket := &shutdownSocket{closeFn: func() error { closed.Add(1); return nil }}
				err := tracker.track(func(router.WebSocketContext) error {
					admitted.Add(1)
					<-release
					return nil
				})(socket)
				if err != nil {
					t.Error(err)
				}
			})
		}
		var snapshot []router.WebSocketReadInterrupter
		var drained <-chan struct{}
		workers.Go(func() { <-start; snapshot, drained = tracker.stop() })
		close(start)
		synctest.Wait()
		if len(snapshot) != int(admitted.Load()) {
			t.Fatalf("snapshot=%d admitted=%d", len(snapshot), admitted.Load())
		}
		if admitted.Load() > 0 {
			requirePending(t, drained)
		}
		if err := tracker.track(func(router.WebSocketContext) error {
			t.Error("handler admitted after stop")
			return nil
		})(&shutdownSocket{}); err != nil {
			t.Error(err)
		}
		close(release)
		workers.Wait()
		if closed.Load() != 32 {
			t.Errorf("closed %d sockets, want 32", closed.Load())
		}
		requireDrained(t, tracker)
		_, again := tracker.stop()
		if again != drained {
			t.Fatal("repeated stop changed the completion channel")
		}
	})
}

func TestDemoShutdownTimeoutThenCleanup(t *testing.T) {
	for _, blocked := range []string{"handler", "close"} {
		t.Run(blocked, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tracker := newSocketTracker()
				release, entered, closing := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				interrupted := make(chan struct{})
				socket := &shutdownSocket{
					interruptFn: func() error {
						tracker.stop() // external callbacks must run outside the registry lock
						close(interrupted)
						return nil
					},
					closeFn: func() error {
						close(closing)
						if blocked == "close" {
							<-release
						}
						return nil
					},
				}
				worker := make(chan error, 1)
				go func() {
					worker <- tracker.track(func(router.WebSocketContext) error {
						close(entered)
						if blocked == "handler" {
							<-release
						}
						return nil
					})(socket)
				}()
				<-entered
				if blocked == "close" {
					<-closing
				}
				server, started := testShutdownServer()
				host, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan error, 1)
				go func() { result <- tracker.run(host, server, "unused", time.Second) }()
				<-started
				cancel()
				<-interrupted
				synctest.Wait()
				requirePending(t, tracker.drained)
				time.Sleep(time.Second - time.Nanosecond)
				synctest.Wait()
				select {
				case err := <-result:
					t.Fatalf("returned before deadline: %v", err)
				default:
				}
				time.Sleep(time.Nanosecond)
				if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("shutdown error = %v, want deadline exceeded", err)
				}
				requirePending(t, tracker.drained)
				unblock()
				if err := <-worker; err != nil {
					t.Fatal(err)
				}
				requireDrained(t, tracker)
				// The bubble joins every goroutine, including Serve, after release.
			})
		})
	}
}

func TestDemoShutdownWaitsForLastClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tracker := newSocketTracker()
		entered, closing, finished := make(chan struct{}, 2), make(chan struct{}, 2), make(chan error, 2)
		releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
		for _, release := range releases {
			readDone := make(chan struct{})
			socket := &shutdownSocket{
				interruptFn: func() error { close(readDone); return nil },
				closeFn: func() error {
					tracker.stop() // close callback reentry must not deadlock
					closing <- struct{}{}
					<-release
					return errors.New("transport close failure")
				},
			}
			go func() {
				finished <- tracker.track(func(router.WebSocketContext) error {
					entered <- struct{}{}
					<-readDone
					return nil
				})(socket)
			}()
		}
		<-entered
		<-entered
		server, started := testShutdownServer()
		host, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- tracker.run(host, server, "unused", time.Second) }()
		<-started
		cancel()
		<-closing
		<-closing
		close(releases[0])
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		requirePending(t, tracker.drained)
		select {
		case err := <-result:
			t.Fatalf("shutdown skipped the last close callback: %v", err)
		default:
		}
		close(releases[1])
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		requireDrained(t, tracker)
	})
}

func TestDemoServeAndShutdownErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		serveErr, shutdownErr := errors.New("serve failure"), errors.New("shutdown failure")
		server := shutdownServer{
			serveFn:    func() error { return serveErr },
			shutdownFn: func(context.Context) error { return shutdownErr },
		}
		tracker := newSocketTracker()
		err := tracker.run(context.Background(), server, "unused", time.Second)
		if !errors.Is(err, serveErr) || !errors.Is(err, shutdownErr) {
			t.Fatalf("lost lifecycle errors: %v", err)
		}
		requireDrained(t, tracker)
	})
}
