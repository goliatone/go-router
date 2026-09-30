package router_test

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

func writeStaticFixture(t *testing.T) string {
	t.Helper()

	return writeStaticFiles(t, map[string]string{
		"index.html":        "<h1>Index</h1>",
		"style.css":         "body { color: red; }",
		"nested/file.txt":   "Hello from nested file",
		"nested/index.html": "<h1>Nested Index</h1>",
	})
}

func writeStaticFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	tempDir := t.TempDir()

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

// writeStaticGuardFixture lays out a static root whose admin/ subtree is meant
// to be protected by a path-prefix guard.
func writeStaticGuardFixture(t *testing.T) string {
	t.Helper()

	return writeStaticFiles(t, map[string]string{
		"admin/export.csv": "SECRET-ADMIN",
		"docs/index.html":  "DOCS",
		"public.txt":       "PUBLIC",
	})
}

// denyPathPrefix is an authorization guard that inspects c.Path(), the way
// applications commonly protect a subtree. It compares case-sensitively, so
// the handler must not serve another spelling of a guarded name.
func denyPathPrefix(prefix string) router.MiddlewareFunc {
	return func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			if strings.HasPrefix(c.Path(), prefix) {
				return c.Status(http.StatusForbidden).SendString("forbidden")
			}
			return next(c)
		}
	}
}

type staticPathCase struct {
	target string
	status int
	body   string
}

// staticGuardMounts pairs a static mount with a guarded subtree. Canonical
// targets reach either the guard or the file. Dot segments, empty segments,
// other spellings and unopenable names get a plain 404. Before, they resolved
// to guarded files the guard never saw, or leaked filesystem errors in a 500
// body. Other spellings only resolve on case-insensitive filesystems such as
// the macOS and Windows defaults; staticCaseMounts covers them on every OS.
var staticGuardMounts = []struct {
	name   string
	prefix string
	denied string
	cases  []staticPathCase
}{
	{
		name:   "prefix",
		prefix: "/static",
		denied: "/static/admin",
		cases: []staticPathCase{
			{"/static/public.txt", http.StatusOK, "PUBLIC"},
			{"/static/docs", http.StatusOK, "DOCS"},
			{"/static/docs/", http.StatusOK, "DOCS"},
			{"/static/admin/export.csv", http.StatusForbidden, "forbidden"},
			{"/static/%61dmin/export.csv", http.StatusForbidden, "forbidden"},
			{"/static/%2561dmin/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/./admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/x/../admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/x/%2e%2e/admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/static//admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/./public.txt", http.StatusNotFound, "Not Found"},
			{"/static/docs//index.html", http.StatusNotFound, "Not Found"},
			{"/static/.", http.StatusNotFound, "Not Found"},
			{"/static/../go.mod", http.StatusNotFound, "Not Found"},
			{"/static/%2e%2e/go.mod", http.StatusNotFound, "Not Found"},
			{"/static/ADMIN/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/public.txt/x", http.StatusNotFound, "Not Found"}, // ENOTDIR
			{"/static/a%00b", http.StatusNotFound, "Not Found"},        // fs.ErrInvalid
			{"/static/%ff", http.StatusNotFound, "Not Found"},          // invalid UTF-8
		},
	},
	{
		name:   "root",
		prefix: "/",
		denied: "/admin",
		cases: []staticPathCase{
			{"/public.txt", http.StatusOK, "PUBLIC"},
			{"/admin/export.csv", http.StatusForbidden, "forbidden"},
			{"/./admin/export.csv", http.StatusNotFound, "Not Found"},
			{"//admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/x/../admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/x/%2e%2e/admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/ADMIN/export.csv", http.StatusNotFound, "Not Found"},
		},
	},
}

func TestStatic_Fiber_NonCanonicalPathsCannotBypassGuard(t *testing.T) {
	for _, mount := range staticGuardMounts {
		t.Run(mount.name, func(t *testing.T) {
			adapter := router.NewFiberAdapter()
			r := adapter.Router()
			r.Use(denyPathPrefix(mount.denied))
			r.Static(mount.prefix, writeStaticGuardFixture(t))
			runFiberStaticCases(t, adapter.WrappedRouter(), mount.cases)
		})
	}
}

func TestStatic_HTTP_NonCanonicalPathsCannotBypassGuard(t *testing.T) {
	for _, mount := range staticGuardMounts {
		t.Run(mount.name, func(t *testing.T) {
			adapter := router.NewHTTPServer()
			r := adapter.Router()
			r.Use(denyPathPrefix(mount.denied))
			r.Static(mount.prefix, writeStaticGuardFixture(t))
			runHTTPStaticCases(t, adapter.WrappedRouter(), mount.cases)
		})
	}
}

// A non-canonical path that only matches the broader mount must not reach
// files served by a more specific mount with its own middleware.
func TestStatic_Fiber_NonCanonicalPathsCannotBypassRouteMiddleware(t *testing.T) {
	dir := writeStaticGuardFixture(t)

	adapter := router.NewFiberAdapter()
	r := adapter.Router()
	admin := r.Group("/static/admin")
	admin.Use(denyPathPrefix("/static/admin"))
	admin.Static("/", filepath.Join(dir, "admin"))
	r.Static("/static", dir)

	runFiberStaticCases(t, adapter.WrappedRouter(), []staticPathCase{
		{"/static/public.txt", http.StatusOK, "PUBLIC"},
		{"/static/admin/export.csv", http.StatusForbidden, "forbidden"},
		{"/static/./admin/export.csv", http.StatusNotFound, "Not Found"},
		{"/static/x/../admin/export.csv", http.StatusNotFound, "Not Found"},
		{"/static/x/%2e%2e/admin/export.csv", http.StatusNotFound, "Not Found"},
		{"/static/ADMIN/export.csv", http.StatusNotFound, "Not Found"},
	})
}

// Fiber matches routes case-insensitively, so "/STATIC/..." reaches the mount
// registered as "/static" without carrying its prefix. The handler used to end
// the chain there with an empty 200.
func TestStatic_Fiber_PrefixSpellingMustMatch(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()
	r.Use(denyPathPrefix("/static/admin"))
	r.Static("/static", writeStaticGuardFixture(t))

	runFiberStaticCases(t, adapter.WrappedRouter(), []staticPathCase{
		{"/static/public.txt", http.StatusOK, "PUBLIC"},
		{"/STATIC/public.txt", http.StatusNotFound, "Not Found"},
		{"/Static/admin/export.csv", http.StatusNotFound, "Not Found"},
	})
}

// foldFS resolves names case-insensitively, like the default macOS and Windows
// filesystems, while directory listings keep the stored spelling. It only
// implements Open, so fs.Stat and fs.ReadDir go through the folding lookup.
// With unlisted set, its directories cannot be listed.
type foldFS struct {
	files    fstest.MapFS
	unlisted bool
}

func (f foldFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	stored := "."
	if name != "." {
		for seg := range strings.SplitSeq(name, "/") {
			entries, err := f.files.ReadDir(stored)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(entries, func(e fs.DirEntry) bool { return strings.EqualFold(e.Name(), seg) })
			if i < 0 {
				return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
			}
			stored = path.Join(stored, entries[i].Name())
		}
	}
	file, err := f.files.Open(stored)
	if err != nil || !f.unlisted {
		return file, err
	}
	return struct{ fs.File }{file}, nil
}

func staticCaseFiles() fstest.MapFS {
	return fstest.MapFS{
		"admin/export.csv": {Data: []byte("SECRET-ADMIN")},
		"docs/index.html":  {Data: []byte("DOCS")},
		"public.txt":       {Data: []byte("PUBLIC")},
	}
}

// staticCaseMounts serve files from filesystems that treat case differently.
// A case-insensitive filesystem also opens other spellings of a stored name,
// which a guard comparing exact names never matched, so only the stored
// spelling may be served; when the directory cannot be listed to confirm it,
// the handler refuses. A case-sensitive filesystem keeps names that differ
// only in case apart, and each must keep serving its own file.
var staticCaseMounts = []struct {
	name  string
	fsys  fs.FS
	cases []staticPathCase
}{
	{
		name: "case-insensitive",
		fsys: foldFS{files: staticCaseFiles()},
		cases: []staticPathCase{
			{"/static/public.txt", http.StatusOK, "PUBLIC"},
			{"/static/docs/", http.StatusOK, "DOCS"},
			{"/static/admin/export.csv", http.StatusForbidden, "forbidden"},
			{"/static/ADMIN/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/Admin/export.csv", http.StatusNotFound, "Not Found"},
			{"/static/Public.txt", http.StatusNotFound, "Not Found"},
			{"/static/DOCS/", http.StatusNotFound, "Not Found"},
			{"/static/docs/INDEX.HTML", http.StatusNotFound, "Not Found"},
		},
	},
	{
		name: "case-insensitive unlisted",
		fsys: foldFS{files: staticCaseFiles(), unlisted: true},
		cases: []staticPathCase{
			{"/static/public.txt", http.StatusInternalServerError, "Internal Server Error"},
			{"/static/ADMIN/export.csv", http.StatusInternalServerError, "Internal Server Error"},
		},
	},
	{
		name: "case-sensitive",
		fsys: fstest.MapFS{
			"readme": {Data: []byte("lower")},
			"README": {Data: []byte("upper")},
		},
		cases: []staticPathCase{
			{"/static/readme", http.StatusOK, "lower"},
			{"/static/README", http.StatusOK, "upper"},
			{"/static/Readme", http.StatusNotFound, "Not Found"},
		},
	},
}

func TestStatic_Fiber_ServesStoredSpellingOnly(t *testing.T) {
	for _, mount := range staticCaseMounts {
		t.Run(mount.name, func(t *testing.T) {
			adapter := router.NewFiberAdapter()
			r := adapter.Router()
			r.Use(denyPathPrefix("/static/admin"))
			r.Static("/static", "", router.Static{FS: mount.fsys})
			runFiberStaticCases(t, adapter.WrappedRouter(), mount.cases)
		})
	}
}

func TestStatic_HTTP_ServesStoredSpellingOnly(t *testing.T) {
	for _, mount := range staticCaseMounts {
		t.Run(mount.name, func(t *testing.T) {
			adapter := router.NewHTTPServer()
			r := adapter.Router()
			r.Use(denyPathPrefix("/static/admin"))
			r.Static("/static", "", router.Static{FS: mount.fsys})
			runHTTPStaticCases(t, adapter.WrappedRouter(), mount.cases)
		})
	}
}

func runFiberStaticCases(t *testing.T, app *fiber.App, cases []staticPathCase) {
	t.Helper()

	for _, tc := range cases {
		resp, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, nil), -1)
		if err != nil {
			t.Fatalf("GET %s failed: %v", tc.target, err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("GET %s: failed to read body: %v", tc.target, err)
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatalf("GET %s: failed to close body: %v", tc.target, closeErr)
		}
		assertStaticPathCase(t, tc, resp.StatusCode, string(body))
	}
}

func runHTTPStaticCases(t *testing.T, h http.Handler, cases []staticPathCase) {
	t.Helper()

	for _, tc := range cases {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, nil))
		assertStaticPathCase(t, tc, rr.Code, rr.Body.String())
	}
}

func assertStaticPathCase(t *testing.T, tc staticPathCase, status int, body string) {
	t.Helper()

	if status != tc.status || body != tc.body {
		t.Errorf("GET %s = %d %q, want %d %q", tc.target, status, body, tc.status, tc.body)
	}
}
