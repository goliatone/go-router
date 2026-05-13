package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

func TestAsResponseStateUnsupported(t *testing.T) {
	if state, ok := router.AsResponseState(nil); ok || state != nil {
		t.Fatalf("expected nil context not to support ResponseState")
	}
	if state, ok := router.AsResponseState(&stubContext{}); ok || state != nil {
		t.Fatalf("expected unsupported context not to support ResponseState")
	}
}

func TestFiberContextResponseState(t *testing.T) {
	adapter := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New()
	})
	r := adapter.Router()

	var statusCode int
	var written bool
	var bodySize int64
	var isStream bool
	var headerAfterMutation string

	r.Get("/state", func(ctx router.Context) error {
		state, ok := router.AsResponseState(ctx)
		if !ok {
			t.Fatal("expected Fiber context to support ResponseState")
		}
		ctx.Status(http.StatusCreated)
		ctx.SetHeader("X-Test", "original")
		if err := ctx.SendString("hello"); err != nil {
			return err
		}
		headers := state.ResponseHeaders()
		headers.Set("X-Test", "mutated")
		headerAfterMutation = state.ResponseHeaders().Get("X-Test")
		statusCode = state.StatusCode()
		written = state.ResponseWritten()
		bodySize = state.ResponseBodySize()
		isStream = state.ResponseIsStream()
		return nil
	})

	resp, err := adapter.WrappedRouter().Test(httptest.NewRequest(http.MethodGet, "/state", nil))
	if err != nil {
		t.Fatalf("fiber request failed: %v", err)
	}
	defer resp.Body.Close()

	if statusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, statusCode)
	}
	if !written {
		t.Fatal("expected response to be written")
	}
	if bodySize != int64(len("hello")) {
		t.Fatalf("expected body size 5, got %d", bodySize)
	}
	if isStream {
		t.Fatal("expected non-stream response")
	}
	if headerAfterMutation != "original" {
		t.Fatalf("expected cloned response headers, got %q", headerAfterMutation)
	}
}

func TestHTTPRouterContextResponseState(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/state", nil)
	ctx := router.NewHTTPRouterContext(rec, req, nil, nil)

	state, ok := router.AsResponseState(ctx)
	if !ok {
		t.Fatal("expected HTTP router context to support ResponseState")
	}

	ctx.SetHeader("X-Test", "original")
	if err := ctx.JSON(http.StatusAccepted, router.ViewContext{"ok": true}); err != nil {
		t.Fatalf("JSON failed: %v", err)
	}
	headers := state.ResponseHeaders()
	headers.Set("X-Test", "mutated")

	if state.StatusCode() != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, state.StatusCode())
	}
	if !state.ResponseWritten() {
		t.Fatal("expected response to be written")
	}
	if state.ResponseBodySize() != int64(rec.Body.Len()) {
		t.Fatalf("expected body size %d, got %d", rec.Body.Len(), state.ResponseBodySize())
	}
	if state.ResponseIsStream() {
		t.Fatal("expected JSON response not to be marked as stream")
	}
	if state.ResponseHeaders().Get("X-Test") != "original" {
		t.Fatalf("expected cloned response headers, got %q", state.ResponseHeaders().Get("X-Test"))
	}
}

func TestDownloadResponseStateMarksStreams(t *testing.T) {
	ctx := router.NewMockContext()
	responder := router.NewDownloadResponder(ctx)

	payload := router.DownloadPayload{
		Reader:         strings.NewReader("streamed body"),
		Size:           int64(len("streamed body")),
		MaxBufferBytes: 1,
	}
	if err := responder.WriteDownload(context.Background(), payload); err != nil {
		t.Fatalf("WriteDownload failed: %v", err)
	}

	state, ok := router.AsResponseState(ctx)
	if !ok {
		t.Fatal("expected MockContext to support ResponseState")
	}
	if !state.ResponseWritten() {
		t.Fatal("expected download to mark response written")
	}
	if !state.ResponseIsStream() {
		t.Fatal("expected oversized download to be marked as stream")
	}
	if state.StatusCode() != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, state.StatusCode())
	}
}

func TestMockContextRenderUpdatesResponseState(t *testing.T) {
	ctx := router.NewMockContext()
	ctx.RenderBodyM = "<h1>rendered</h1>"

	if err := ctx.Render("page", nil); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	state, ok := router.AsResponseState(ctx)
	if !ok {
		t.Fatal("expected MockContext to support ResponseState")
	}
	if ctx.ResponseBodyM != "<h1>rendered</h1>" {
		t.Fatalf("expected rendered body, got %q", ctx.ResponseBodyM)
	}
	if !state.ResponseWritten() {
		t.Fatal("expected mock render to mark response written")
	}
	if state.ResponseBodySize() != int64(len(ctx.ResponseBodyM)) {
		t.Fatalf("expected body size %d, got %d", len(ctx.ResponseBodyM), state.ResponseBodySize())
	}
	if state.ResponseIsStream() {
		t.Fatal("expected mock render not to mark response as stream")
	}
}

func TestMockContextCookieUpdatesResponseHeaders(t *testing.T) {
	ctx := router.NewMockContext()
	cookie := router.FirstPartySessionCookie("session", "abc")

	ctx.Cookie(&cookie)

	values := ctx.ResponseHeadersM.Values("Set-Cookie")
	if len(values) != 1 {
		t.Fatalf("expected one Set-Cookie response header, got %d: %v", len(values), values)
	}
	if !strings.Contains(values[0], "session=abc") || !strings.Contains(values[0], "SameSite=Lax") {
		t.Fatalf("expected mock Set-Cookie header with attributes, got %q", values[0])
	}
	if got := ctx.Cookies("session"); got != "abc" {
		t.Fatalf("expected mock cookie store to keep value, got %q", got)
	}
}
