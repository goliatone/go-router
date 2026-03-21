package ssefiber_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/eventstream"
	"github.com/goliatone/go-router/ssefiber"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type streamTestContext struct {
	*router.MockContext
	reqCtx     context.Context
	sendStream func(io.Reader) error
}

func (c *streamTestContext) Context() context.Context {
	if c.reqCtx != nil {
		return c.reqCtx
	}
	return context.Background()
}

func (c *streamTestContext) SendStream(r io.Reader) error {
	if c.sendStream != nil {
		return c.sendStream(r)
	}
	return nil
}

func TestMountFiberRequiresRouter(t *testing.T) {
	err := ssefiber.MountFiber(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires router")
}

func TestMountFiberValidatesDependenciesAtMountTime(t *testing.T) {
	adapter := router.NewFiberAdapter()

	err := ssefiber.MountFiber(adapter.Router())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event stream")
}

func TestHandlerStreamsReplayWhenLastEventIDOverridesCursorQuery(t *testing.T) {
	stream := eventstream.New()
	scope := eventstream.Scope{"tenant": "t1"}
	first := stream.Publish(scope, eventstream.Event{Name: "first"})
	second := stream.Publish(scope, eventstream.Event{Name: "second"})

	handler := ssefiber.Handler(
		ssefiber.WithStream(stream),
		ssefiber.WithScopeResolver(staticScopeResolver(scope)),
	)

	ctx := newStreamMockContext(t)
	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx.HeadersM["Last-Event-ID"] = first.Cursor
	ctx.QueriesM["cursor"] = "missing"
	ctx.reqCtx = reqCtx

	var body string
	ctx.sendStream = func(reader io.Reader) error {

		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()

		raw, err := io.ReadAll(reader)
		require.NoError(t, err)
		body = string(raw)
		return nil
	}

	err := handler(ctx)
	require.NoError(t, err)
	assert.Contains(t, body, "retry: 3000")
	assert.Contains(t, body, "id: "+second.Cursor)
	assert.Contains(t, body, "event: second")
	assert.NotContains(t, body, "event: stream_gap")
}

func TestHandlerAppliesClientTuningAndEmitsHeartbeat(t *testing.T) {
	stream := eventstream.New()
	scope := eventstream.Scope{"tenant": "tenant:alpha"}

	handler := ssefiber.Handler(
		ssefiber.WithStream(stream),
		ssefiber.WithScopeResolver(staticScopeResolver(scope)),
		ssefiber.WithAllowClientTuning(true),
		ssefiber.WithHeartbeatInterval(25*time.Millisecond),
		ssefiber.WithRetryInterval(150*time.Millisecond),
		ssefiber.WithHeartbeatBounds(10*time.Millisecond, 30*time.Millisecond),
		ssefiber.WithRetryBounds(100*time.Millisecond, 250*time.Millisecond),
	)

	ctx := newStreamMockContext(t)
	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx.QueriesM["heartbeat_ms"] = "1"
	ctx.QueriesM["retry_ms"] = "999999"
	ctx.reqCtx = reqCtx

	var body string
	ctx.sendStream = func(reader io.Reader) error {

		go func() {
			time.Sleep(40 * time.Millisecond)
			cancel()
		}()

		raw, err := io.ReadAll(reader)
		require.NoError(t, err)
		body = string(raw)
		return nil
	}

	err := handler(ctx)
	require.NoError(t, err)
	assert.Contains(t, body, "retry: 250")
	assert.Contains(t, body, "event: heartbeat")
	assert.Contains(t, body, `"scope_key":"tenant=tenant%3Aalpha"`)
}

func TestHandlerEmitsFullStreamGapPayloadAndCloses(t *testing.T) {
	stream := eventstream.New()
	scope := eventstream.Scope{"tenant": "t1"}
	stream.Publish(scope, eventstream.Event{Name: "existing"})

	handler := ssefiber.Handler(
		ssefiber.WithStream(stream),
		ssefiber.WithScopeResolver(staticScopeResolver(scope)),
	)

	ctx := newStreamMockContext(t)
	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx.HeadersM["Last-Event-ID"] = "missing"
	ctx.reqCtx = reqCtx

	var body string
	ctx.sendStream = func(reader io.Reader) error {
		raw, err := io.ReadAll(reader)
		require.NoError(t, err)
		body = string(raw)
		return nil
	}

	err := handler(ctx)
	require.NoError(t, err)
	assert.Contains(t, body, "retry: 3000")
	assert.Contains(t, body, "event: stream_gap")
	assert.Contains(t, body, `"reason":"cursor_not_found"`)
	assert.Contains(t, body, `"last_event_id":"missing"`)
	assert.Contains(t, body, `"fallback_transport":"polling"`)
	assert.Contains(t, body, `"resume_supported":false`)
	assert.Contains(t, body, `"requires_gap_reconcile":true`)
	assert.NotContains(t, body, "\nid:")
}

func TestMountFiberStreamsEventsAndCleansUpSubscriber(t *testing.T) {
	adapter := router.NewFiberAdapter()
	stream := eventstream.New()
	cancelCh := make(chan context.CancelFunc, 1)

	err := ssefiber.MountFiber(
		adapter.Router(),
		ssefiber.WithStream(stream),
		ssefiber.WithScopeResolver(func(ctx router.Context) (eventstream.Scope, error) {
			return eventstream.Scope{"tenant": ctx.Header("X-Tenant-ID")}, nil
		}),
		ssefiber.WithMiddlewares(router.ToMiddleware(func(ctx router.Context) error {
			reqCtx, cancel := context.WithCancel(ctx.Context())
			cancelCh <- cancel
			ctx.SetContext(reqCtx)
			return ctx.Next()
		})),
	)
	require.NoError(t, err)

	app := adapter.WrappedRouter()

	type responseResult struct {
		body string
		resp *http.Response
		err  error
	}
	resultCh := make(chan responseResult, 1)

	go func() {
		req := httptest.NewRequest(http.MethodGet, "/events", nil)
		req.Header.Set("X-Tenant-ID", "t1")

		resp, err := app.Test(req, -1)
		if err != nil {
			resultCh <- responseResult{err: err}
			return
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		resultCh <- responseResult{
			body: string(raw),
			resp: resp,
			err:  err,
		}
	}()

	cancel := <-cancelCh

	assert.Eventually(t, func() bool {
		return stream.SnapshotStats().ActiveSubscribers == 1
	}, time.Second, 10*time.Millisecond)

	record := stream.Publish(eventstream.Scope{"tenant": "t1"}, eventstream.Event{Name: "lifecycle"})
	assert.Equal(t, "1", record.Cursor)

	time.Sleep(20 * time.Millisecond)
	cancel()

	result := <-resultCh
	require.NoError(t, result.err)
	require.NotNil(t, result.resp)
	assert.Equal(t, http.StatusOK, result.resp.StatusCode)
	assert.Equal(t, "text/event-stream", result.resp.Header.Get("Content-Type"))
	assert.Contains(t, result.body, "retry: 3000")
	assert.Contains(t, result.body, "id: 1")
	assert.Contains(t, result.body, "event: lifecycle")

	assert.Eventually(t, func() bool {
		stats := stream.SnapshotStats()
		return stats.ActiveSubscribers == 0 && stats.DropReasons["client_disconnect"] == 1
	}, time.Second, 10*time.Millisecond)
}

func newStreamMockContext(t *testing.T) *streamTestContext {
	t.Helper()

	ctx := &streamTestContext{MockContext: router.NewMockContext()}
	ctx.On("SetHeader", "Content-Type", "text/event-stream").Return(nil)
	ctx.On("SetHeader", "Cache-Control", "no-cache").Return(nil)
	ctx.On("SetHeader", "Connection", "keep-alive").Return(nil)
	ctx.On("SetHeader", "X-Accel-Buffering", "no").Return(nil)
	return ctx
}

func staticScopeResolver(scope eventstream.Scope) func(router.Context) (eventstream.Scope, error) {
	return func(router.Context) (eventstream.Scope, error) {
		return scope, nil
	}
}
