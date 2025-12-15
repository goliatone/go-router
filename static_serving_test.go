package router_test

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	req := httptest.NewRequest("GET", "/api/public/style.css", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if got := string(body); got != "body { color: red; }" {
		t.Fatalf("body = %q, want %q", got, "body { color: red; }")
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
	req := httptest.NewRequest("GET", "/api/public/style.css", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	if got := rr.Body.String(); got != "body { color: red; }" {
		t.Fatalf("body = %q, want %q", got, "body { color: red; }")
	}
}

func TestStatic_HTTP_WrappedRouterRegistersLateRoutes(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Static("/public", tempDir)

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/public/style.css", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestStatic_HTTP_ServesIndexAtPrefix(t *testing.T) {
	tempDir := writeStaticFixture(t)

	adapter := router.NewHTTPServer()
	r := adapter.Router()

	r.Static("/public", tempDir)

	h := adapter.WrappedRouter()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/public", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
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

	req := httptest.NewRequest("GET", "/public", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
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
	req := httptest.NewRequest("GET", "/public/style.css", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != 500 {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
}
