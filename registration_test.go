package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSortRoutesBySpecificityHandlesInterleavedMethods(t *testing.T) {
	routes := []*RouteDefinition{
		{Method: GET, Path: "/admin/*"},
		{Method: POST, Path: "/admin/search/options"},
		{Method: HEAD, Path: "/admin/content/:type"},
		{Method: GET, Path: "/admin/search/relevance"},
		{Method: POST, Path: "/admin/search/:action"},
	}

	sortRoutesBySpecificity(routes)

	if got := routes[0].Path; got != "/admin/search/relevance" {
		t.Fatalf("first GET route = %q, want exact route", got)
	}
	if got := routes[1].Path; got != "/admin/search/options" {
		t.Fatalf("first POST route = %q, want static route", got)
	}
	if got := routes[3].Path; got != "/admin/*" {
		t.Fatalf("second GET route = %q, want catch-all", got)
	}
}

func TestSortRoutesBySpecificityHandlesUnrelatedSameMethodRoutes(t *testing.T) {
	routes := []*RouteDefinition{
		{Method: GET, Path: "/admin/*"},
		{Method: GET, Path: "/health"},
		{Method: GET, Path: "/admin/search/relevance"},
	}

	sortRoutesBySpecificity(routes)

	if got := routes[2].Path; got != "/admin/*" {
		t.Fatalf("last GET route = %q, want catch-all", got)
	}
	if got := routes[1].Path; got != "/admin/search/relevance" {
		t.Fatalf("specific admin route = %q, want exact route before catch-all", got)
	}
}

func TestFiberRegistrationPlanDispatchesSpecificRouteAcrossUnrelatedSameMethodRoute(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	r.Get("/admin/*", func(c Context) error { return c.SendStatus(404) })
	r.Get("/health", func(c Context) error { return c.SendStatus(204) })
	r.Get("/admin/search/relevance", func(c Context) error { return c.SendString("relevance") })

	response, err := server.WrappedRouter().Test(httptest.NewRequest("GET", "/admin/search/relevance", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "relevance" {
		t.Fatalf("response = %d %q, want 200 relevance", response.StatusCode, body)
	}
}

func TestFiberRegistrationPlanDispatchesSpecificRouteBeforeCatchAll(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	r.Get("/admin/*", func(c Context) error { return c.SendStatus(404) })
	r.Post("/admin/search/options", func(c Context) error { return c.SendStatus(204) })
	r.Head("/admin/content/:type", func(c Context) error { return c.SendStatus(204) })
	r.Get("/admin/search/relevance", func(c Context) error { return c.SendString("relevance") })

	response, err := server.WrappedRouter().Test(httptest.NewRequest("GET", "/admin/search/relevance", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
}

func TestFiberRegistrationPlanOrdersWebSocketWildcardWithOrdinaryGETRoutes(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	r.WebSocket("/admin/*", WebSocketConfig{}, func(WebSocketContext) error { return nil })
	r.Get("/admin/search/relevance", func(c Context) error { return c.SendString("relevance") })

	response, err := server.WrappedRouter().Test(httptest.NewRequest(http.MethodGet, "/admin/search/relevance", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
}

func TestFiberRegistrationRejectsMutationAfterSeal(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	readyRoute := r.Get("/ready", func(c Context) error { return c.SendStatus(204) })
	server.Init()

	deferred := func() (recovered any) {
		defer func() { recovered = recover() }()
		r.Get("/late", func(c Context) error { return c.SendStatus(204) })
		return nil
	}()
	err, ok := deferred.(error)
	if !ok || !errors.Is(err, ErrRouterSealed) {
		t.Fatalf("panic = %#v, want typed ErrRouterSealed", deferred)
	}
	nameMutation := func() (recovered any) {
		defer func() { recovered = recover() }()
		readyRoute.SetName("ready")
		return nil
	}()
	nameErr, ok := nameMutation.(error)
	if !ok || !errors.Is(nameErr, ErrRouterSealed) {
		t.Fatalf("route name panic = %#v, want typed ErrRouterSealed", nameMutation)
	}

	snapshot := r.(RegistrationInspector).RegistrationSnapshot()
	if snapshot.State != RegistrationSealed {
		t.Fatalf("state = %q, want sealed", snapshot.State)
	}
	if len(snapshot.MountedRoutes) != 1 || snapshot.MountedRoutes[0].Path != "/ready" {
		t.Fatalf("mounted routes = %#v", snapshot.MountedRoutes)
	}
}

func TestFiberTryReplaceIsExplicitAndPreservesRouteIdentity(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	r.Get("/preview", func(c Context) error { return c.SendString("original") }).SetName("preview")

	replacer := r.(RouteReplacer)
	replaced, err := replacer.TryReplace(GET, "/preview", func(c Context) error { return c.SendString("replacement") })
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	definition := replaced.(*RouteDefinition)
	if definition.Name != "preview" {
		t.Fatalf("name = %q, want preview", definition.Name)
	}

	response, err := server.WrappedRouter().Test(httptest.NewRequest("GET", "/preview", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "replacement" {
		t.Fatalf("body = %q, want replacement", body)
	}

	if _, err := replacer.TryReplace(GET, "/missing", func(Context) error { return nil }); !errors.Is(err, ErrRouterSealed) {
		t.Fatalf("post-seal replace error = %v, want ErrRouterSealed", err)
	}
}

func TestFiberTryReplacePreservesExistingMiddlewareByDefault(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	middlewareCalls := 0
	middleware := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			middlewareCalls++
			return next(c)
		}
	}
	r.Get("/preview", func(c Context) error { return c.SendString("original") }, middleware)

	if _, err := r.(RouteReplacer).TryReplace(GET, "/preview", func(c Context) error { return c.SendString("replacement") }); err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	response, err := server.WrappedRouter().Test(httptest.NewRequest(http.MethodGet, "/preview", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if middlewareCalls != 1 || response.StatusCode != http.StatusOK {
		t.Fatalf("middleware calls=%d status=%d, want 1 and 200", middlewareCalls, response.StatusCode)
	}
}

func TestFiberTryReplaceCanExplicitlyReplaceMiddlewareChain(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	originalCalls := 0
	replacementCalls := 0
	original := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { originalCalls++; return next(c) }
	}
	replacement := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { replacementCalls++; return next(c) }
	}
	r.Get("/preview", func(c Context) error { return c.SendString("original") }, original)

	mutator := r.(RouteMutator)
	if _, err := mutator.TryReplaceWithOptions(GET, "/preview", func(c Context) error { return c.SendString("replacement") }, RouteMutationOptions{ReplaceMiddleware: true}, replacement); err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	if _, err := server.WrappedRouter().Test(httptest.NewRequest(http.MethodGet, "/preview", nil)); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if originalCalls != 0 || replacementCalls != 1 {
		t.Fatalf("middleware calls original=%d replacement=%d, want 0 and 1", originalCalls, replacementCalls)
	}
}

func TestFiberTryUpsertAddsOptionalRouteThenReplacesIt(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	upserter := r.(RouteUpserter)

	added, replaced, err := upserter.TryUpsert(GET, "/optional", func(c Context) error { return c.SendString("added") })
	if err != nil || replaced || added == nil {
		t.Fatalf("first upsert: route=%v replaced=%t err=%v", added, replaced, err)
	}
	replacement, replaced, err := upserter.TryUpsert(GET, "/optional", func(c Context) error { return c.SendString("replaced") })
	if err != nil || !replaced || replacement != added {
		t.Fatalf("second upsert: same=%t replaced=%t err=%v", replacement == added, replaced, err)
	}

	response, err := server.WrappedRouter().Test(httptest.NewRequest("GET", "/optional", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	if string(body) != "replaced" {
		t.Fatalf("body = %q, want replaced", body)
	}
}

func TestFiberTryUpsertCanApplyMiddlewareOnlyWhenAdding(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	existingCalls := 0
	addOnlyCalls := 0
	existing := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { existingCalls++; return next(c) }
	}
	addOnly := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { addOnlyCalls++; return next(c) }
	}
	r.Get("/existing", func(c Context) error { return c.SendString("original") }, existing)

	mutator := r.(RouteMutator)
	options := RouteMutationOptions{MiddlewareOnAddOnly: true}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/existing", func(c Context) error { return c.SendString("replacement") }, options, addOnly); err != nil || !replaced {
		t.Fatalf("replace upsert: replaced=%t err=%v", replaced, err)
	}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/missing", func(c Context) error { return c.SendString("added") }, options, addOnly); err != nil || replaced {
		t.Fatalf("add upsert: replaced=%t err=%v", replaced, err)
	}

	for _, path := range []string{"/existing", "/missing"} {
		if _, err := server.WrappedRouter().Test(httptest.NewRequest(http.MethodGet, path, nil)); err != nil {
			t.Fatalf("request %s failed: %v", path, err)
		}
	}
	if existingCalls != 1 || addOnlyCalls != 1 {
		t.Fatalf("middleware calls existing=%d add-only=%d, want 1 and 1", existingCalls, addOnlyCalls)
	}
}

func TestHTTPRouterTryUpsertAddsOptionalRouteThenReplacesIt(t *testing.T) {
	server := NewHTTPServer()
	r := server.Router()
	upserter := r.(RouteUpserter)

	added, replaced, err := upserter.TryUpsert(GET, "/optional", func(c Context) error {
		return c.SendString("added")
	})
	if err != nil || replaced || added == nil {
		t.Fatalf("first upsert: route=%v replaced=%t err=%v", added, replaced, err)
	}
	replacement, replaced, err := upserter.TryUpsert(GET, "/optional", func(c Context) error {
		return c.SendString("replaced")
	})
	if err != nil || !replaced || replacement != added {
		t.Fatalf("second upsert: same=%t replaced=%t err=%v", replacement == added, replaced, err)
	}

	request := httptest.NewRequest(http.MethodGet, "/optional", nil)
	response := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(response, request)
	if body := response.Body.String(); body != "replaced" {
		t.Fatalf("body = %q, want replaced", body)
	}

	snapshot := r.(RegistrationInspector).RegistrationSnapshot()
	if snapshot.State != RegistrationSealed || len(snapshot.MountedRoutes) != 1 {
		t.Fatalf("snapshot = %#v, want one sealed route", snapshot)
	}
}

func TestHTTPRouterTryUpsertCanApplyMiddlewareOnlyWhenAdding(t *testing.T) {
	server := NewHTTPServer()
	r := server.Router()
	existingCalls := 0
	addOnlyCalls := 0
	existing := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { existingCalls++; return next(c) }
	}
	addOnly := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { addOnlyCalls++; return next(c) }
	}
	r.Get("/existing", func(c Context) error { return c.SendString("original") }, existing)

	mutator := r.(RouteMutator)
	options := RouteMutationOptions{MiddlewareOnAddOnly: true}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/existing", func(c Context) error { return c.SendString("replacement") }, options, addOnly); err != nil || !replaced {
		t.Fatalf("replace upsert: replaced=%t err=%v", replaced, err)
	}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/missing", func(c Context) error { return c.SendString("added") }, options, addOnly); err != nil || replaced {
		t.Fatalf("add upsert: replaced=%t err=%v", replaced, err)
	}

	for _, path := range []string{"/existing", "/missing"} {
		response := httptest.NewRecorder()
		server.WrappedRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("request %s status=%d, want 200", path, response.Code)
		}
	}
	if existingCalls != 1 || addOnlyCalls != 1 {
		t.Fatalf("middleware calls existing=%d add-only=%d, want 1 and 1", existingCalls, addOnlyCalls)
	}
}

func TestHTTPRouterTryUpsertReturnsConflictInsteadOfPanicking(t *testing.T) {
	server := NewHTTPServer()
	r := server.Router()
	r.Get("/users/:id", func(c Context) error { return c.SendStatus(http.StatusNoContent) })

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("TryUpsert panicked: %v", recovered)
		}
	}()
	if _, _, err := r.(RouteUpserter).TryUpsert(GET, "/users/*rest", func(c Context) error { return c.SendStatus(http.StatusNoContent) }); err == nil {
		t.Fatal("expected conflicting upsert to return an error")
	}
}

func TestHTTPRouterTryReplacePreservesExistingMiddleware(t *testing.T) {
	server := NewHTTPServer()
	r := server.Router()
	middlewareCalls := 0
	middleware := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error { middlewareCalls++; return next(c) }
	}
	r.Get("/preview", func(c Context) error { return c.SendString("original") }, middleware)
	if _, err := r.(RouteReplacer).TryReplace(GET, "/preview", func(c Context) error { return c.SendString("replacement") }); err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	response := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/preview", nil))
	if middlewareCalls != 1 || response.Code != http.StatusOK {
		t.Fatalf("middleware calls=%d status=%d, want 1 and 200", middlewareCalls, response.Code)
	}
}

func TestFiberRegistrationSerializesConcurrentCollection(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	const count = 32

	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			r.Get(fmt.Sprintf("/module/%d", index), func(c Context) error { return c.SendStatus(204) })
		}(i)
	}
	wg.Wait()
	server.Init()

	snapshot := r.(RegistrationInspector).RegistrationSnapshot()
	if len(snapshot.DeclaredRoutes) != count || len(snapshot.MountedRoutes) != count {
		t.Fatalf("declared=%d mounted=%d, want %d", len(snapshot.DeclaredRoutes), len(snapshot.MountedRoutes), count)
	}
}

func TestFiberFinalizationPublishesCompleteMountedSnapshot(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	const count = 128
	for index := range count {
		r.Get(fmt.Sprintf("/module/%d", index), func(c Context) error { return c.SendStatus(http.StatusNoContent) })
	}

	done := make(chan struct{})
	go func() {
		server.Init()
		close(done)
	}()
	for {
		snapshot := r.(RegistrationInspector).RegistrationSnapshot()
		if snapshot.State == RegistrationSealed && len(snapshot.MountedRoutes) != count {
			t.Fatalf("sealed snapshot mounted=%d, want %d", len(snapshot.MountedRoutes), count)
		}
		select {
		case <-done:
			final := r.(RegistrationInspector).RegistrationSnapshot()
			if final.State != RegistrationSealed || len(final.MountedRoutes) != count {
				t.Fatalf("final snapshot state=%s mounted=%d, want sealed and %d", final.State, len(final.MountedRoutes), count)
			}
			return
		default:
		}
	}
}

func TestFiberConcurrentInitIsSerialized(t *testing.T) {
	server := NewFiberAdapter()
	server.Router().Get("/ready", func(c Context) error { return c.SendStatus(http.StatusNoContent) })

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.Init()
		}()
	}
	wg.Wait()
	snapshot := server.Router().(RegistrationInspector).RegistrationSnapshot()
	if snapshot.State != RegistrationSealed || len(snapshot.MountedRoutes) != 1 {
		t.Fatalf("snapshot = %#v, want one sealed route", snapshot)
	}
}

func TestAnalyzeRouteShadows(t *testing.T) {
	routes := []RouteDefinition{
		{Method: GET, Path: "/admin/*"},
		{Method: POST, Path: "/admin/search/relevance"},
		{Method: GET, Path: "/admin/search/relevance"},
		{Method: GET, Path: "/admin/content/:type"},
		{Method: GET, Path: "/admin/content/archive_event_session"},
	}

	shadows := AnalyzeRouteShadows(routes)
	if len(shadows) != 3 {
		t.Fatalf("shadows = %#v, want three GET findings", shadows)
	}
	for _, shadow := range shadows {
		if shadow.ShadowedByPath != "/admin/*" {
			t.Fatalf("shadowed by = %q, want /admin/*", shadow.ShadowedByPath)
		}
	}
}
