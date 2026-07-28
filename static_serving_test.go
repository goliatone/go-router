package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goliatone/go-router"
)

func writeStaticFixture(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()

	files := map[string]string{
		"index.html":        "<h1>Index</h1>",
		"style.css":         "body { color: red; }",
		"nested/file.txt":   "Hello from nested file",
		"nested/index.html": "<h1>Nested Index</h1>",
	}

	for fpath, content := range files {
		fullPath := filepath.Join(tempDir, fpath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	return tempDir
}

func TestStatic_Fiber_GroupPrefix(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	group := r.Group("/api")
	group.Static("/public", tempDir)

	app := adapter.WrappedRouter()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/public/style.css", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("failed to close body: %v", closeErr)
	}
	if got := string(body); got != "body { color: red; }" {
		t.Fatalf("body = %q, want %q", got, "body { color: red; }")
	}
}

func TestStatic_Fiber_RootPrefix(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewFiberAdapter()
	adapter.Router().Static("/", tempDir)
	app := adapter.WrappedRouter()

	resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/style.css", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("failed to close body: %v", closeErr)
	}
	if got := string(body); got != "body { color: red; }" {
		t.Fatalf("body = %q, want %q", got, "body { color: red; }")
	}

	resp, err = app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("root request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("root status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read root body: %v", err)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Fatalf("failed to close root body: %v", closeErr)
	}
	if got := string(body); got != "<h1>Index</h1>" {
		t.Fatalf("root body = %q, want %q", got, "<h1>Index</h1>")
	}

	resp, err = app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/style.css", nil))
	if err != nil {
		t.Fatalf("head request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("head status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("failed to close head body: %v", err)
	}
}

func TestStatic_HTTP_GroupPrefix(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	group := r.Group("/api")
	group.Static("/public", tempDir)

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/public/style.css", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	if got := rr.Body.String(); got != "body { color: red; }" {
		t.Fatalf("body = %q, want %q", got, "body { color: red; }")
	}
}

func TestStatic_HTTP_RootPrefix(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	adapter.Router().Static("/", tempDir)

	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/style.css", nil)
	adapter.WrappedRouter().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Body.String(); got != "body { color: red; }" {
		t.Fatalf("body = %q, want %q", got, "body { color: red; }")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	adapter.WrappedRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Body.String(); got != "<h1>Index</h1>" {
		t.Fatalf("root body = %q, want %q", got, "<h1>Index</h1>")
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/style.css", nil)
	adapter.WrappedRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("head status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestStatic_HTTP_WrappedRouterRegistersLateRoutes(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Static("/public", tempDir)

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/public/style.css", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestStatic_HTTP_ServesIndexAtPrefix(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Static("/public", tempDir)

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/public", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Body.String(); got != "<h1>Index</h1>" {
		t.Fatalf("body = %q, want %q", got, "<h1>Index</h1>")
	}
}

func TestStatic_Fiber_CustomFSRootSubdir(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	r.Static("/public", "", router.Static{
		FS:   os.DirFS(tempDir),
		Root: "nested",
	})

	app := adapter.WrappedRouter()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/public", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("failed to close body: %v", err)
	}
	if got := string(body); got != "<h1>Nested Index</h1>" {
		t.Fatalf("body = %q, want %q", got, "<h1>Nested Index</h1>")
	}
}

func TestStatic_HTTP_InvalidFSRootReturns500(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Static("/public", "", router.Static{
		FS:   os.DirFS(tempDir),
		Root: "missing",
	})

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/public/style.css", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestStatic_HTTP_BrowseRendersDirectoryListing(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Static("/public", tempDir, router.Static{Browse: true})

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/public/nested/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "file.txt") {
		t.Fatalf("expected directory listing to contain file entry, got %q", body)
	}
	if got := rr.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want %q", got, "text/html; charset=utf-8")
	}
}
