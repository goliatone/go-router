package router

import (
	"context"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"

	f2 "github.com/gofiber/fiber/v2"
	f3 "github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

var _ ManagedServer = (*FiberAdapter)(nil)
var _ ManagedServer = (*FiberV3Adapter)(nil)

// The guard compares actual native registrations, not logical route snapshots.
// It is used under registration ownership, before serving. Raw mutation during
// a managed lifetime is prohibited, just like concurrent Fiber registration.
// Retain reflected function values, not code addresses: distinct closures can
// share an entry PC while dispatching different routes.
type nativeRouteEntry struct {
	route              any
	method, path, name string
	handlers           []reflect.Value
}
type nativeRegistrationGuard struct {
	capture    func() []nativeRouteEntry
	expected   []nativeRouteEntry
	dirty      bool
	middleware map[any]bool
	latest     map[any]bool
}

func newNativeRegistrationGuard(capture func() []nativeRouteEntry, allowInitial bool) *nativeRegistrationGuard {
	entries := capture()
	g := &nativeRegistrationGuard{capture: capture, expected: entries, dirty: !allowInitial && len(entries) != 0, middleware: make(map[any]bool)}
	// The sole admitted initial registrations are the built-in logger middleware.
	if allowInitial {
		for _, e := range entries {
			g.middleware[e.route] = true
		}
	}
	return g
}
func (g *nativeRegistrationGuard) before() {
	if g != nil && !reflect.DeepEqual(g.capture(), g.expected) {
		g.dirty = true
	}
}

// afterAdd only admits the handler and method/path delta requested by the
// adapter. Hooks execute inside native registration; their effects are untrusted.
func (g *nativeRegistrationGuard) afterAdd(methods []string, path string, handler any, middleware bool) {
	if g == nil {
		return
	}
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	id := reflect.ValueOf(handler)
	actual := g.capture()
	old := make(map[any]nativeRouteEntry, len(g.expected))
	last := make(map[string]any)
	for _, e := range g.expected {
		old[e.route] = e
		last[e.method] = e.route
	}
	changed := make(map[string]int)
	latest := make(map[any]bool)
	retained := make([]nativeRouteEntry, 0, len(g.expected))
	for _, e := range actual {
		if previous, exists := old[e.route]; exists {
			candidate := e
			if slices.Contains(methods, e.method) && e.path == path && last[e.method] == e.route && len(e.handlers) == len(previous.handlers)+1 {
				candidate.handlers = e.handlers[:len(e.handlers)-1]
				if e.handlers[len(e.handlers)-1] == id {
					changed[e.method]++
					latest[e.route] = true
				} else {
					g.dirty = true
				}
			}
			if !reflect.DeepEqual(candidate, previous) {
				g.dirty = true
			}
			retained = append(retained, previous)
		} else {
			if !slices.Contains(methods, e.method) || e.path != path || e.name != "" || len(e.handlers) != 1 || e.handlers[0] != id {
				g.dirty = true
			}
			changed[e.method]++
			latest[e.route] = true
		}
	}
	if !reflect.DeepEqual(retained, g.expected) && !(len(retained) == 0 && len(g.expected) == 0) {
		g.dirty = true
	}
	for _, method := range methods {
		if changed[method] != 1 {
			g.dirty = true
		}
	}
	if !g.dirty {
		g.latest = latest
		for route := range latest {
			g.middleware[route] = middleware
		}
		g.expected = actual
	}
}

func (g *nativeRegistrationGuard) afterName(method, path, name string, v2 bool) {
	if g == nil {
		return
	}
	expected := append([]nativeRouteEntry(nil), g.expected...)
	for i := range expected {
		e := &expected[i]
		if e.path == path && ((v2 && e.method == method || !v2 && g.latest[e.route]) || method == "GET" && e.method == "HEAD") {
			e.name = name
		}
	}
	actual := g.capture()
	if !reflect.DeepEqual(actual, expected) {
		g.dirty = true
	}
	if !g.dirty {
		g.expected = actual
	}
}

// Preparation may synthesize native HEAD companions. Every existing route must
// remain unchanged and every new entry must be a copy of an owned GET route.
func (g *nativeRegistrationGuard) afterPrepare() {
	if g == nil {
		return
	}
	actual := g.capture()
	old := make(map[any]nativeRouteEntry, len(g.expected))
	for _, e := range g.expected {
		old[e.route] = e
	}
	retained := make([]nativeRouteEntry, 0, len(g.expected))
	heads := make(map[string]bool)
	for _, e := range g.expected {
		if e.method == "HEAD" && !g.middleware[e.route] {
			heads[e.path] = true
		}
	}
	for _, e := range actual {
		if previous, exists := old[e.route]; exists {
			if !reflect.DeepEqual(e, previous) {
				g.dirty = true
			}
			retained = append(retained, previous)
			continue
		}
		matched := false
		if e.method == "HEAD" && !heads[e.path] {
			for _, get := range g.expected {
				if get.method != "GET" || g.middleware[get.route] {
					continue
				}
				get.route, get.method = e.route, "HEAD"
				if reflect.DeepEqual(e, get) {
					matched = true
					break
				}
			}
		}
		if !matched {
			g.dirty = true
		}
		heads[e.path] = true
	}
	if !reflect.DeepEqual(retained, g.expected) && !(len(retained) == 0 && len(g.expected) == 0) {
		g.dirty = true
	}
	if !g.dirty {
		g.expected = actual
	}
}
func (g *nativeRegistrationGuard) validate() error {
	if g == nil {
		return ErrManagedNativeRegistration
	}
	g.before()
	if g.dirty {
		return ErrManagedNativeRegistration
	}
	return nil
}
func fiberRouteEntries(app *f2.App) []nativeRouteEntry {
	var entries []nativeRouteEntry
	for _, stack := range app.Stack() {
		for _, r := range stack {
			e := nativeRouteEntry{route: r, method: r.Method, path: r.Path, name: r.Name}
			for _, h := range r.Handlers {
				e.handlers = append(e.handlers, reflect.ValueOf(h))
			}
			entries = append(entries, e)
		}
	}
	return entries
}
func fiberV3RouteEntries(app *f3.App) []nativeRouteEntry {
	var entries []nativeRouteEntry
	for _, stack := range app.Stack() {
		for _, r := range stack {
			e := nativeRouteEntry{route: r, method: r.Method, path: r.Path, name: r.Name}
			for _, h := range r.Handlers {
				e.handlers = append(e.handlers, reflect.ValueOf(h))
			}
			entries = append(entries, e)
		}
	}
	return entries
}

func (a *FiberAdapter) StartManaged(ctx context.Context, options ManagedOptions) (ManagedRun, error) {
	a.ingress.close()
	cfg := a.app.Config()
	if cfg.Prefork || (cfg.Network != "" && cfg.Network != "tcp" && cfg.Network != "tcp4" && cfg.Network != "tcp6") {
		return nil, ErrManagedUnsupported
	}
	return a.hosting.startManaged(ctx, options, managedDriver{
		initialize: a.Initialize,
		ingress:    &a.ingress,
		validate: func() error {
			if err := a.router.ownership.validate(); err != nil {
				return err
			}
			if a.ingress.runtime != nil {
				return a.ingress.runtime.validate()
			}
			return nil
		},
		prepare: func() *fasthttp.Server {
			a.router.ownership.before()
			a.app.Handler()
			a.router.ownership.afterPrepare()
			return a.app.Server()
		},
		serve: a.app.Listener,
		withContext: func(c *fasthttp.RequestCtx, ctx context.Context, next fasthttp.RequestHandler) {
			native := a.app.AcquireCtx(c)
			native.SetUserContext(ctx)
			defer a.app.ReleaseCtx(native)
			next(c)
		},
	})
}
func (a *FiberV3Adapter) StartManaged(ctx context.Context, options ManagedOptions) (ManagedRun, error) {
	a.ingress.close()
	cfg := a.listenConfig
	if cfg.EnablePrefork || cfg.GracefulContext != nil || cfg.TLSConfig != nil || cfg.TLSConfigFunc != nil || cfg.AutoCertManager != nil || cfg.CertFile != "" || cfg.CertKeyFile != "" || cfg.CertClientFile != "" || cfg.TLSMinVersion != 0 || cfg.ListenerAddrFunc != nil || cfg.UnixSocketFileMode != 0 || (cfg.ListenerNetwork != "" && cfg.ListenerNetwork != "tcp" && cfg.ListenerNetwork != "tcp4" && cfg.ListenerNetwork != "tcp6") {
		return nil, ErrManagedUnsupported
	}
	return a.hosting.startManaged(ctx, options, managedDriver{
		initialize: a.Initialize,
		ingress:    &a.ingress,
		validate: func() error {
			if err := a.router.ownership.validate(); err != nil {
				return err
			}
			if a.ingress.runtime != nil {
				return a.ingress.runtime.validate()
			}
			return nil
		},
		prepare: func() *fasthttp.Server {
			a.router.ownership.before()
			a.app.Handler()
			a.router.ownership.afterPrepare()
			return a.app.Server()
		},
		serve: func(ln net.Listener) error { return a.app.Listener(ln, cfg) },
		withContext: func(c *fasthttp.RequestCtx, ctx context.Context, next fasthttp.RequestHandler) {
			native := a.app.AcquireCtx(c)
			native.SetContext(ctx)
			// Keep this context acquired until dispatch returns: v3 ReleaseCtx clears
			// its user context. The native dispatcher acquires its own pooled context.
			defer a.app.ReleaseCtx(native)
			next(c)
		},
	})
}

// Keep mode arbitration separate from initialization, which may run user code.
type fiberHosting struct {
	mu            sync.Mutex
	managed       *managedRun
	legacy        bool
	legacyDone    bool
	nativeStopped bool
	raw           bool
}

func (h *fiberHosting) startManaged(ctx context.Context, options ManagedOptions, driver managedDriver) (ManagedRun, error) {
	if ctx == nil {
		return nil, ErrManagedOptions
	}
	options, err := options.validated()
	if err != nil {
		return nil, err
	}
	if err = context.Cause(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.managed != nil {
		return nil, ErrManagedAlreadyStarted
	}
	if h.legacy || h.raw {
		return nil, ErrHostingMode
	}
	run := newManagedRun(ctx, options, driver)
	h.managed = run
	run.start()
	return run, nil
}
func (h *fiberHosting) startLegacy() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.managed != nil || h.legacy {
		return ErrHostingMode
	}
	h.legacy = true
	return nil
}
func (h *fiberHosting) finishLegacy() { h.mu.Lock(); h.legacyDone = true; h.mu.Unlock() }
func (h *fiberHosting) useRaw() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.managed != nil {
		return ErrHostingMode
	}
	h.raw = true
	return nil
}
func (h *fiberHosting) shutdown(ctx context.Context, server *fasthttp.Server, shutdown func(context.Context) error) error {
	h.mu.Lock()
	run, finished, stopped := h.managed, h.legacyDone, h.nativeStopped
	h.mu.Unlock()
	if run != nil {
		return run.Stop(ctx)
	}
	// Native shutdown before Serve registers its listener acknowledges no stop.
	// Reject this window explicitly; the legacy caller can retry after readiness.
	if server.GetOpenConnectionsCount() < 0 {
		if finished || stopped {
			return nil
		}
		return ErrHostingMode
	}
	if ctx == nil {
		return ErrManagedOptions
	}
	result := make(chan error, 1)
	go func() {
		err := shutdown(ctx)
		if err == nil {
			h.mu.Lock()
			h.nativeStopped = true
			h.mu.Unlock()
		}
		result <- err
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
