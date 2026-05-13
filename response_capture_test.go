package router_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

func TestCaptureResponseCapturesAndReplaysNonStream(t *testing.T) {
	base := router.NewMockContext()

	captured, err := router.CaptureResponse(base, 1024, func(ctx router.Context) error {
		ctx.SetHeader("X-Test", "captured")
		ctx.Status(http.StatusCreated)
		return ctx.SendString("hello")
	})
	if err != nil {
		t.Fatalf("CaptureResponse failed: %v", err)
	}
	if base.ResponseBodyM != "" {
		t.Fatalf("expected capture not to mutate live response body, got %q", base.ResponseBodyM)
	}
	if captured.StatusCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, captured.StatusCode)
	}
	if captured.Headers.Get("X-Test") != "captured" {
		t.Fatalf("expected captured header, got %q", captured.Headers.Get("X-Test"))
	}
	if string(captured.Body) != "hello" {
		t.Fatalf("expected captured body, got %q", captured.Body)
	}

	replay := router.NewMockContext()
	if err := router.ReplayCapturedResponse(replay, captured); err != nil {
		t.Fatalf("ReplayCapturedResponse failed: %v", err)
	}
	if replay.StatusCodeM != http.StatusCreated {
		t.Fatalf("expected replay status %d, got %d", http.StatusCreated, replay.StatusCodeM)
	}
	if replay.ResponseHeadersM.Get("X-Test") != "captured" {
		t.Fatalf("expected replay header, got %q", replay.ResponseHeadersM.Get("X-Test"))
	}
	if replay.ResponseBodyM != "hello" {
		t.Fatalf("expected replay body, got %q", replay.ResponseBodyM)
	}
}

func TestReplayCapturedResponsePreservesRepeatedHeaders(t *testing.T) {
	base := router.NewMockContext()

	captured, err := router.CaptureResponse(base, 1024, func(ctx router.Context) error {
		ctx.Cookie(&router.Cookie{Name: "session", Value: "abc", Path: "/"})
		ctx.Cookie(&router.Cookie{Name: "prefs", Value: "dark", Path: "/"})
		ctx.SetHeader("X-Test", "scalar")
		return ctx.SendString("hello")
	})
	if err != nil {
		t.Fatalf("CaptureResponse failed: %v", err)
	}
	if got := captured.Headers.Values("Set-Cookie"); len(got) != 2 {
		t.Fatalf("expected two captured Set-Cookie headers, got %d: %v", len(got), got)
	}

	replay := router.NewMockContext()
	if err := router.ReplayCapturedResponse(replay, captured); err != nil {
		t.Fatalf("ReplayCapturedResponse failed: %v", err)
	}

	cookies := replay.ResponseHeadersM.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Fatalf("expected two replayed Set-Cookie headers, got %d: %v", len(cookies), cookies)
	}
	if !strings.Contains(cookies[0], "session=abc") || !strings.Contains(cookies[1], "prefs=dark") {
		t.Fatalf("expected replayed cookies to preserve values, got %v", cookies)
	}
	if values := replay.ResponseHeadersM.Values("X-Test"); len(values) != 1 || values[0] != "scalar" {
		t.Fatalf("expected scalar header replay, got %v", values)
	}
}

func TestReplayCapturedResponsePreservesEmptyBodyStatus(t *testing.T) {
	replay := router.NewMockContext()
	captured := &router.CapturedResponse{
		StatusCode: http.StatusAccepted,
		Headers:    make(http.Header),
	}

	if err := router.ReplayCapturedResponse(replay, captured); err != nil {
		t.Fatalf("ReplayCapturedResponse failed: %v", err)
	}
	if replay.StatusCodeM != http.StatusAccepted {
		t.Fatalf("expected replay status %d, got %d", http.StatusAccepted, replay.StatusCodeM)
	}
	if !replay.ResponseWritten() {
		t.Fatal("expected replay to mark response written")
	}
	if replay.ResponseBodyM != "" {
		t.Fatalf("expected empty replay body, got %q", replay.ResponseBodyM)
	}
}

func TestReplayCapturedResponsePreservesEmptyRedirectStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	replay := router.NewHTTPRouterContext(
		rec,
		httptest.NewRequest(http.MethodGet, "/redirect", nil),
		nil,
		nil,
	)
	captured := &router.CapturedResponse{
		StatusCode: http.StatusFound,
		Headers: http.Header{
			"Location": []string{"/target"},
		},
	}

	if err := router.ReplayCapturedResponse(replay, captured); err != nil {
		t.Fatalf("ReplayCapturedResponse failed: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("expected replay status %d, got %d", http.StatusFound, rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/target" {
		t.Fatalf("expected Location header, got %q", got)
	}
	if rec.Body.String() != "" {
		t.Fatalf("expected empty replay body, got %q", rec.Body.String())
	}
}

func TestCaptureResponsePreservesCookieAttributes(t *testing.T) {
	captured, err := router.CaptureResponse(router.NewMockContext(), 1024, func(ctx router.Context) error {
		firstParty := router.FirstPartySessionCookie("session", "abc")
		embedded := router.EmbeddedThirdPartySessionCookie("embedded", "xyz")
		ctx.Cookie(&firstParty)
		ctx.Cookie(&embedded)
		return ctx.SendString("ok")
	})
	if err != nil {
		t.Fatalf("CaptureResponse failed: %v", err)
	}

	cookies := captured.Headers.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Fatalf("expected two captured cookies, got %d: %v", len(cookies), cookies)
	}
	session := cookies[0]
	if !strings.Contains(session, "session=abc") || !strings.Contains(session, "HttpOnly") || !strings.Contains(session, "SameSite=Lax") {
		t.Fatalf("expected first-party session cookie attributes, got %q", session)
	}
	if strings.Contains(session, "Max-Age") || strings.Contains(session, "Expires=") {
		t.Fatalf("expected session-only cookie not to include persistence attributes, got %q", session)
	}

	embedded := cookies[1]
	if !strings.Contains(embedded, "embedded=xyz") || !strings.Contains(embedded, "HttpOnly") || !strings.Contains(embedded, "Secure") || !strings.Contains(embedded, "SameSite=None") {
		t.Fatalf("expected embedded cookie security attributes, got %q", embedded)
	}
}

func TestCaptureResponseRenderUsesTemplateRenderer(t *testing.T) {
	base := router.NewMockContext()
	base.RenderBodyM = "<h1>cached</h1>"

	captured, err := router.CaptureResponse(base, 1024, func(ctx router.Context) error {
		return ctx.Render("page", router.ViewContext{"title": "Cached"})
	})
	if err != nil {
		t.Fatalf("CaptureResponse render failed: %v", err)
	}
	if base.ResponseBodyM != "" {
		t.Fatalf("expected capture render not to mutate live response body, got %q", base.ResponseBodyM)
	}
	if string(captured.Body) != "<h1>cached</h1>" {
		t.Fatalf("expected rendered body, got %q", captured.Body)
	}
	if captured.Headers.Get(router.HeaderContentType) != "text/html; charset=utf-8" {
		t.Fatalf("expected HTML content type, got %q", captured.Headers.Get(router.HeaderContentType))
	}
}

func TestCaptureResponseRenderRejectsOversizedOutput(t *testing.T) {
	base := router.NewMockContext()
	base.RenderBodyM = "<h1>too large</h1>"

	_, err := router.CaptureResponse(base, 4, func(ctx router.Context) error {
		return ctx.Render("page", nil)
	})
	if !errors.Is(err, router.ErrResponseCaptureTooLarge) {
		t.Fatalf("expected ErrResponseCaptureTooLarge, got %v", err)
	}
}

func TestCaptureResponseFiberJSONUsesConfiguredEncoder(t *testing.T) {
	adapter := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(fiber.Config{
			JSONEncoder: func(v any) ([]byte, error) {
				return []byte(`{"custom":true}`), nil
			},
		})
	})
	r := adapter.Router()

	var captured *router.CapturedResponse
	r.Get("/json", func(ctx router.Context) error {
		var err error
		captured, err = router.CaptureResponse(ctx, 1024, func(c router.Context) error {
			return c.JSON(http.StatusAccepted, router.ViewContext{"ok": true})
		})
		return err
	})

	resp, err := adapter.WrappedRouter().Test(httptest.NewRequest(http.MethodGet, "/json", nil))
	if err != nil {
		t.Fatalf("fiber request failed: %v", err)
	}
	defer resp.Body.Close()

	if captured == nil {
		t.Fatal("expected captured response")
	}
	if captured.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, captured.StatusCode)
	}
	if got := string(captured.Body); got != `{"custom":true}` {
		t.Fatalf("expected custom Fiber JSON bytes without newline, got %q", got)
	}
}

func TestCaptureResponseHTTPRouterJSONRejectsOversizedOutput(t *testing.T) {
	base := router.NewHTTPRouterContext(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/json", nil),
		nil,
		nil,
	)

	_, err := router.CaptureResponse(base, 8, func(ctx router.Context) error {
		return ctx.JSON(http.StatusOK, router.ViewContext{"message": "too large"})
	})
	if !errors.Is(err, router.ErrResponseCaptureTooLarge) {
		t.Fatalf("expected ErrResponseCaptureTooLarge, got %v", err)
	}
}

func TestCaptureResponseHTTPRouterJSONPreservesEncoderBehavior(t *testing.T) {
	base := router.NewHTTPRouterContext(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/json", nil),
		nil,
		nil,
	)

	captured, err := router.CaptureResponse(base, 1024, func(ctx router.Context) error {
		return ctx.JSON(http.StatusAccepted, router.ViewContext{"ok": true})
	})
	if err != nil {
		t.Fatalf("CaptureResponse failed: %v", err)
	}

	if captured.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, captured.StatusCode)
	}
	if got := string(captured.Body); got != "{\"ok\":true}\n" {
		t.Fatalf("expected HTTP router JSON encoder bytes, got %q", got)
	}
}

func TestCaptureResponseRejectsOversizedBody(t *testing.T) {
	_, err := router.CaptureResponse(router.NewMockContext(), 4, func(ctx router.Context) error {
		return ctx.SendString("too large")
	})
	if !errors.Is(err, router.ErrResponseCaptureTooLarge) {
		t.Fatalf("expected ErrResponseCaptureTooLarge, got %v", err)
	}
}

func TestCaptureResponseRejectsStreams(t *testing.T) {
	_, err := router.CaptureResponse(router.NewMockContext(), 1024, func(ctx router.Context) error {
		return ctx.SendStream(strings.NewReader("stream"))
	})
	if !errors.Is(err, router.ErrResponseCaptureStream) {
		t.Fatalf("expected ErrResponseCaptureStream, got %v", err)
	}
}
