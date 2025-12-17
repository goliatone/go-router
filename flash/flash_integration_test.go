package flash_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
)

func findCookie(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestFlash_SetGet_ClearsCookieAndPreservesOtherCookies(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	r.Post("/set", func(ctx router.Context) error {
		ctx.Cookie(&router.Cookie{Name: "other", Value: "1", Path: "/"})
		flash.WithError(ctx, router.ViewContext{
			"error_message":  "boom",
			"system_message": "Error parsing body",
		})
		return ctx.JSON(200, router.ViewContext{"ok": true})
	})

	r.Get("/get", func(ctx router.Context) error {
		data := flash.Get(ctx)
		return ctx.JSON(200, data)
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("POST", "/set", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	setCookies := resp.Header.Values("Set-Cookie")
	if len(setCookies) < 2 {
		t.Fatalf("expected multiple Set-Cookie headers, got %d", len(setCookies))
	}
	if !strings.Contains(strings.Join(setCookies, "\n"), "other=") {
		t.Fatalf("expected other cookie to be set, got %v", setCookies)
	}
	if !strings.Contains(strings.Join(setCookies, "\n"), "router-app-flash=") {
		t.Fatalf("expected flash cookie to be set, got %v", setCookies)
	}

	flashCookie := findCookie(t, resp, "router-app-flash")
	if flashCookie == nil || flashCookie.Value == "" {
		t.Fatalf("expected router-app-flash cookie to be present")
	}

	req2 := httptest.NewRequest("GET", "/get", nil)
	req2.AddCookie(&http.Cookie{Name: flashCookie.Name, Value: flashCookie.Value})
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var out router.ViewContext
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := out["error_message"]; !ok {
		t.Fatalf("expected error_message in flash data, got %v", out)
	}
	if _, ok := out["system_message"]; !ok {
		t.Fatalf("expected system_message in flash data, got %v", out)
	}

	clearHeaders := resp2.Header.Values("Set-Cookie")
	if len(clearHeaders) == 0 {
		t.Fatalf("expected flash cookie to be cleared via Set-Cookie")
	}
	joined := strings.Join(clearHeaders, "\n")
	joinedLower := strings.ToLower(joined)
	if !strings.Contains(joinedLower, "router-app-flash=") {
		t.Fatalf("expected cleared flash cookie header, got %v", clearHeaders)
	}
	if !strings.Contains(joinedLower, "path=/") {
		t.Fatalf("expected Path=/ on cleared cookie, got %v", clearHeaders)
	}
	if !strings.Contains(joinedLower, "httponly") {
		t.Fatalf("expected HttpOnly on cleared cookie, got %v", clearHeaders)
	}
	if !strings.Contains(joinedLower, "samesite=lax") {
		t.Fatalf("expected SameSite=Lax on cleared cookie, got %v", clearHeaders)
	}
}

func TestFlash_Get_DoesNotClearWhenMissing(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	r.Get("/get", func(ctx router.Context) error {
		data := flash.Get(ctx)
		return ctx.JSON(200, data)
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequest("GET", "/get", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if got := len(resp.Header.Values("Set-Cookie")); got != 0 {
		t.Fatalf("expected no Set-Cookie headers when no flash cookie exists, got %d", got)
	}
}

func TestFlash_Clear_RespectsPathAndDomain(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	custom := flash.New(flash.Config{
		Name:     "custom-flash",
		Path:     "/admin",
		Domain:   "example.com",
		SameSite: "Strict",
	})

	r.Post("/set", func(ctx router.Context) error {
		custom.WithData(ctx, router.ViewContext{"k": "v"})
		return ctx.SendStatus(200)
	})

	r.Get("/get", func(ctx router.Context) error {
		_ = custom.Get(ctx)
		return ctx.SendStatus(200)
	})

	app := adapter.WrappedRouter()

	resp1, err := app.Test(httptest.NewRequest("POST", "/set", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	c := findCookie(t, resp1, "custom-flash")
	if c == nil {
		t.Fatalf("expected custom-flash cookie to be set")
	}

	req2 := httptest.NewRequest("GET", "/get", nil)
	req2.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	joined := strings.Join(resp2.Header.Values("Set-Cookie"), "\n")
	joinedLower := strings.ToLower(joined)
	if !strings.Contains(joinedLower, "custom-flash=") {
		t.Fatalf("expected custom-flash to be cleared, got %q", joined)
	}
	if !strings.Contains(joinedLower, "path=/admin") {
		t.Fatalf("expected Path=/admin on clear cookie, got %q", joined)
	}
	if !strings.Contains(joinedLower, "domain=example.com") {
		t.Fatalf("expected Domain=example.com on clear cookie, got %q", joined)
	}
	if !strings.Contains(joinedLower, "samesite=strict") {
		t.Fatalf("expected SameSite=Strict on clear cookie, got %q", joined)
	}
}

func TestFlash_Toasts_SupportMultipleAndDefaults(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	custom := flash.New(flash.Config{
		Name:                "toast-flash",
		DefaultMessageTitle: "Default Title",
		DefaultMessageText:  "Default Text",
	})

	r.Post("/set", func(ctx router.Context) error {
		custom.SetMessage(ctx, flash.Message{Type: "success", Text: ""})
		custom.SetMessage(ctx, flash.Message{Type: "warning", Title: "Heads up", Text: "Be careful"})
		return ctx.SendStatus(200)
	})

	r.Get("/get", func(ctx router.Context) error {
		msgs, ok := custom.GetMessages(ctx)
		return ctx.JSON(200, router.ViewContext{"ok": ok, "messages": msgs})
	})

	app := adapter.WrappedRouter()

	resp1, err := app.Test(httptest.NewRequest("POST", "/set", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	c := findCookie(t, resp1, "toast-flash")
	if c == nil || c.Value == "" {
		t.Fatalf("expected toast-flash cookie to be set")
	}

	req2 := httptest.NewRequest("GET", "/get", nil)
	req2.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var out struct {
		OK       bool           `json:"ok"`
		Messages []flash.Message `json:"messages"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.OK {
		t.Fatalf("expected ok=true, got false")
	}
	if len(out.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Title != "Default Title" {
		t.Fatalf("expected default title, got %q", out.Messages[0].Title)
	}
	if out.Messages[0].Text != "Default Text" {
		t.Fatalf("expected default text, got %q", out.Messages[0].Text)
	}
}

func TestFlash_HTTPServer_Clear_RespectsPathDomainAndSameSite(t *testing.T) {
	adapter := router.NewHTTPServer()
	r := adapter.Router()

	custom := flash.New(flash.Config{
		Name:     "http-flash",
		Path:     "/admin",
		Domain:   "example.com",
		SameSite: "Lax",
	})

	r.Get("/set", func(ctx router.Context) error {
		custom.WithData(ctx, router.ViewContext{"k": "v"})
		return ctx.SendStatus(200)
	})

	r.Get("/get", func(ctx router.Context) error {
		_ = custom.Get(ctx)
		return ctx.SendStatus(200)
	})

	h := adapter.WrappedRouter()

	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/set", nil)
	h.ServeHTTP(rr1, req1)
	resp1 := rr1.Result()
	defer resp1.Body.Close()

	var setCookie *http.Cookie
	for _, c := range resp1.Cookies() {
		if c.Name == "http-flash" {
			setCookie = c
			break
		}
	}
	if setCookie == nil || setCookie.Value == "" {
		t.Fatalf("expected http-flash cookie to be set")
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/get", nil)
	req2.AddCookie(&http.Cookie{Name: setCookie.Name, Value: setCookie.Value})
	h.ServeHTTP(rr2, req2)
	resp2 := rr2.Result()
	defer resp2.Body.Close()

	joinedLower := strings.ToLower(strings.Join(resp2.Header.Values("Set-Cookie"), "\n"))
	if !strings.Contains(joinedLower, "http-flash=") {
		t.Fatalf("expected http-flash cookie to be cleared, got %q", joinedLower)
	}
	if !strings.Contains(joinedLower, "path=/admin") {
		t.Fatalf("expected path=/admin on clear cookie, got %q", joinedLower)
	}
	if !strings.Contains(joinedLower, "domain=example.com") {
		t.Fatalf("expected domain=example.com on clear cookie, got %q", joinedLower)
	}
	if !strings.Contains(joinedLower, "samesite=lax") {
		t.Fatalf("expected samesite=lax on clear cookie, got %q", joinedLower)
	}
	if !strings.Contains(joinedLower, "expires=thu, 01 jan 1970") {
		t.Fatalf("expected expires=Thu, 01 Jan 1970... on clear cookie, got %q", joinedLower)
	}
}
