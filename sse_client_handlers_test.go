package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
)

func TestRegisterSSEHandlersServesEmbeddedAssets(t *testing.T) {
	adapter := router.NewFiberAdapter()
	router.RegisterSSEHandlers(adapter.Router())
	app := adapter.WrappedRouter()

	req := httptest.NewRequest(http.MethodGet, "/sseclient/client.min.js", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if got := resp.Header.Get("X-SSE-Client-Version"); got != router.SSEClientVersion {
		t.Fatalf("expected X-SSE-Client-Version %q, got %q", router.SSEClientVersion, got)
	}

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("expected javascript content type, got %q", got)
	}

	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "client.min.js") {
		t.Fatalf("expected minified filename in Content-Disposition, got %q", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body error: %v", err)
	}

	if !strings.Contains(string(body), "GoRouterSSEClient") {
		t.Fatalf("expected embedded SSE bundle marker in response body")
	}
}

func TestRegisterSSEHandlersServesModuleAndTypes(t *testing.T) {
	adapter := router.NewFiberAdapter()
	router.RegisterSSEHandlers(adapter.Router())
	app := adapter.WrappedRouter()

	moduleResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/sseclient/client.mjs", nil), -1)
	if err != nil {
		t.Fatalf("module app.Test error: %v", err)
	}
	defer moduleResp.Body.Close()

	moduleBody, err := io.ReadAll(moduleResp.Body)
	if err != nil {
		t.Fatalf("read module body error: %v", err)
	}

	if !strings.Contains(string(moduleBody), "createSSEClient") {
		t.Fatalf("expected ESM bundle to export createSSEClient")
	}

	typesResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/sseclient/client.d.ts", nil), -1)
	if err != nil {
		t.Fatalf("types app.Test error: %v", err)
	}
	defer typesResp.Body.Close()

	typesBody, err := io.ReadAll(typesResp.Body)
	if err != nil {
		t.Fatalf("read types body error: %v", err)
	}

	if got := typesResp.Header.Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("expected text/plain content type for d.ts, got %q", got)
	}

	if !strings.Contains(string(typesBody), "interface SSEClient") {
		t.Fatalf("expected TypeScript definitions in response body")
	}
}

func TestRegisterSSEHandlersSupportsETagAndInfo(t *testing.T) {
	adapter := router.NewFiberAdapter()
	router.RegisterSSEHandlers(adapter.Router())
	app := adapter.WrappedRouter()

	firstResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/sseclient/client.js", nil), -1)
	if err != nil {
		t.Fatalf("initial app.Test error: %v", err)
	}
	defer firstResp.Body.Close()

	etag := firstResp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header for client.js")
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/sseclient/client.js", nil)
	secondReq.Header.Set("If-None-Match", etag)

	secondResp, err := app.Test(secondReq, -1)
	if err != nil {
		t.Fatalf("etag app.Test error: %v", err)
	}
	defer secondResp.Body.Close()

	if secondResp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304, got %d", secondResp.StatusCode)
	}

	infoResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/sseclient/info", nil), -1)
	if err != nil {
		t.Fatalf("info app.Test error: %v", err)
	}
	defer infoResp.Body.Close()

	var info struct {
		Version string         `json:"version"`
		Build   string         `json:"build"`
		Files   map[string]any `json:"files"`
	}
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		t.Fatalf("decode info error: %v", err)
	}

	if info.Version != router.SSEClientVersion {
		t.Fatalf("expected version %q, got %q", router.SSEClientVersion, info.Version)
	}

	if _, ok := info.Files["client.mjs"]; !ok {
		t.Fatal("expected client.mjs entry in SSE client info")
	}
}

func TestRegisterSSEHandlersUsesConfiguredBaseRoute(t *testing.T) {
	adapter := router.NewFiberAdapter()
	router.RegisterSSEHandlers(adapter.Router(), router.SSEClientHandlerConfig{
		BaseRoute: "/assets/runtime",
	})
	app := adapter.WrappedRouter()

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/assets/runtime/client.mjs", nil), -1)
	if err != nil {
		t.Fatalf("custom base route app.Test error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}
