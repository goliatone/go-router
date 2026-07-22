package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func requireCapability[T any](t *testing.T, value any) T {
	t.Helper()

	capability, ok := value.(T)
	if !ok {
		t.Fatalf("%T does not implement the required capability", value)
	}
	return capability
}

func performFiberRequest(t *testing.T, app *fiber.App, method, path string) (int, []byte) {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return response.StatusCode, body
}

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

	status, body := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/admin/search/relevance")
	if status != http.StatusOK || string(body) != "relevance" {
		t.Fatalf("response = %d %q, want 200 relevance", status, body)
	}
}

func TestFiberRegistrationPlanDispatchesSpecificRouteBeforeCatchAll(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	r.Get("/admin/*", func(c Context) error { return c.SendStatus(404) })
	r.Post("/admin/search/options", func(c Context) error { return c.SendStatus(204) })
	r.Head("/admin/content/:type", func(c Context) error { return c.SendStatus(204) })
	r.Get("/admin/search/relevance", func(c Context) error { return c.SendString("relevance") })

	status, _ := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/admin/search/relevance")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
}

func TestFiberRegistrationPlanOrdersWebSocketWildcardWithOrdinaryGETRoutes(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	r.WebSocket("/admin/*", WebSocketConfig{}, func(WebSocketContext) error { return nil })
	r.Get("/admin/search/relevance", func(c Context) error { return c.SendString("relevance") })

	status, _ := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/admin/search/relevance")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
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

	inspector := requireCapability[RegistrationInspector](t, r)
	snapshot := inspector.RegistrationSnapshot()
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

	replacer := requireCapability[RouteReplacer](t, r)
	replaced, err := replacer.TryReplace(GET, "/preview", func(c Context) error { return c.SendString("replacement") })
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	definition := requireCapability[*RouteDefinition](t, replaced)
	if definition.Name != "preview" {
		t.Fatalf("name = %q, want preview", definition.Name)
	}

	_, body := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/preview")
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

	replacer := requireCapability[RouteReplacer](t, r)
	if _, err := replacer.TryReplace(GET, "/preview", func(c Context) error { return c.SendString("replacement") }); err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	status, _ := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/preview")
	if middlewareCalls != 1 || status != http.StatusOK {
		t.Fatalf("middleware calls=%d status=%d, want 1 and 200", middlewareCalls, status)
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

	mutator := requireCapability[RouteMutator](t, r)
	if _, err := mutator.TryReplaceWithOptions(GET, "/preview", func(c Context) error { return c.SendString("replacement") }, RouteMutationOptions{ReplaceMiddleware: true}, replacement); err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/preview")
	if originalCalls != 0 || replacementCalls != 1 {
		t.Fatalf("middleware calls original=%d replacement=%d, want 0 and 1", originalCalls, replacementCalls)
	}
}

func TestFiberTryUpsertAddsOptionalRouteThenReplacesIt(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	upserter := requireCapability[RouteUpserter](t, r)

	added, replaced, err := upserter.TryUpsert(GET, "/optional", func(c Context) error { return c.SendString("added") })
	if err != nil || replaced || added == nil {
		t.Fatalf("first upsert: route=%v replaced=%t err=%v", added, replaced, err)
	}
	replacement, replaced, err := upserter.TryUpsert(GET, "/optional", func(c Context) error { return c.SendString("replaced") })
	if err != nil || !replaced || replacement != added {
		t.Fatalf("second upsert: same=%t replaced=%t err=%v", replacement == added, replaced, err)
	}

	_, body := performFiberRequest(t, server.WrappedRouter(), http.MethodGet, "/optional")
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

	mutator := requireCapability[RouteMutator](t, r)
	options := RouteMutationOptions{MiddlewareOnAddOnly: true}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/existing", func(c Context) error { return c.SendString("replacement") }, options, addOnly); err != nil || !replaced {
		t.Fatalf("replace upsert: replaced=%t err=%v", replaced, err)
	}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/missing", func(c Context) error { return c.SendString("added") }, options, addOnly); err != nil || replaced {
		t.Fatalf("add upsert: replaced=%t err=%v", replaced, err)
	}

	for _, path := range []string{"/existing", "/missing"} {
		performFiberRequest(t, server.WrappedRouter(), http.MethodGet, path)
	}
	if existingCalls != 1 || addOnlyCalls != 1 {
		t.Fatalf("middleware calls existing=%d add-only=%d, want 1 and 1", existingCalls, addOnlyCalls)
	}
}

func TestHTTPRouterTryUpsertAddsOptionalRouteThenReplacesIt(t *testing.T) {
	server := NewHTTPServer()
	r := server.Router()
	upserter := requireCapability[RouteUpserter](t, r)

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

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/optional", nil)
	response := httptest.NewRecorder()
	server.WrappedRouter().ServeHTTP(response, request)
	if body := response.Body.String(); body != "replaced" {
		t.Fatalf("body = %q, want replaced", body)
	}

	inspector := requireCapability[RegistrationInspector](t, r)
	snapshot := inspector.RegistrationSnapshot()
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

	mutator := requireCapability[RouteMutator](t, r)
	options := RouteMutationOptions{MiddlewareOnAddOnly: true}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/existing", func(c Context) error { return c.SendString("replacement") }, options, addOnly); err != nil || !replaced {
		t.Fatalf("replace upsert: replaced=%t err=%v", replaced, err)
	}
	if _, replaced, err := mutator.TryUpsertWithOptions(GET, "/missing", func(c Context) error { return c.SendString("added") }, options, addOnly); err != nil || replaced {
		t.Fatalf("add upsert: replaced=%t err=%v", replaced, err)
	}

	for _, path := range []string{"/existing", "/missing"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		server.WrappedRouter().ServeHTTP(response, request)
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
	upserter := requireCapability[RouteUpserter](t, r)
	if _, _, err := upserter.TryUpsert(GET, "/users/*rest", func(c Context) error { return c.SendStatus(http.StatusNoContent) }); err == nil {
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
	replacer := requireCapability[RouteReplacer](t, r)
	if _, err := replacer.TryReplace(GET, "/preview", func(c Context) error { return c.SendString("replacement") }); err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/preview", nil)
	server.WrappedRouter().ServeHTTP(response, request)
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

	inspector := requireCapability[RegistrationInspector](t, r)
	snapshot := inspector.RegistrationSnapshot()
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
	inspector := requireCapability[RegistrationInspector](t, r)

	done := make(chan struct{})
	go func() {
		server.Init()
		close(done)
	}()
	for {
		snapshot := inspector.RegistrationSnapshot()
		if snapshot.State == RegistrationSealed && len(snapshot.MountedRoutes) != count {
			t.Fatalf("sealed snapshot mounted=%d, want %d", len(snapshot.MountedRoutes), count)
		}
		select {
		case <-done:
			final := inspector.RegistrationSnapshot()
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
		wg.Go(func() {
			server.Init()
		})
	}
	wg.Wait()
	inspector := requireCapability[RegistrationInspector](t, server.Router())
	snapshot := inspector.RegistrationSnapshot()
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

func TestAnalyzeRouteShadowsClassifiesTrailingSlashEquivalence(t *testing.T) {
	routes := []RouteDefinition{
		{Method: GET, Path: "/admin"},
		{Method: GET, Path: "/admin/"},
	}

	shadows := AnalyzeRouteShadowsWithSemantics(routes, RouteMatchingSemantics{TrailingSlashDistinct: false})
	if len(shadows) != 1 || shadows[0].Kind != RouteShadowTrailingSlashEquivalent {
		t.Fatalf("shadows = %#v, want one trailing-slash-equivalent finding", shadows)
	}
	if got := shadows[0].Reason; got != "earlier route is equivalent when trailing slashes are ignored" {
		t.Fatalf("reason = %q", got)
	}
}

func TestAnalyzeRouteShadowsKeepsStrictTrailingSlashRoutesDistinct(t *testing.T) {
	routes := []RouteDefinition{
		{Method: GET, Path: "/admin"},
		{Method: GET, Path: "/admin/"},
	}

	if shadows := AnalyzeRouteShadowsWithSemantics(routes, RouteMatchingSemantics{TrailingSlashDistinct: true}); len(shadows) != 0 {
		t.Fatalf("shadows = %#v, want none for strict trailing-slash matching", shadows)
	}
}

func TestRouterCapabilitiesAndMatchingSemantics(t *testing.T) {
	fiberServer := NewFiberAdapter()
	fiberRouter := fiberServer.Router()
	if caps := fiberRouter.(RoutingCapabilityProvider).RoutingCapabilities(); !caps.RouteNamePolicy || !caps.OwnershipChecks || !caps.Manifest {
		t.Fatalf("fiber capabilities = %+v", caps)
	}
	if semantics := fiberRouter.(RouteMatchingSemanticsProvider).RouteMatchingSemantics(); semantics.TrailingSlashDistinct {
		t.Fatalf("fiber matching semantics = %+v", semantics)
	}
	if fiberRouter.(TrailingSlashDistinctProvider).TrailingSlashDistinct() {
		t.Fatal("non-strict Fiber unexpectedly requires explicit trailing-slash routes")
	}

	httpServer := NewHTTPServer()
	if semantics := httpServer.Router().(RouteMatchingSemanticsProvider).RouteMatchingSemantics(); !semantics.TrailingSlashDistinct {
		t.Fatalf("httprouter matching semantics = %+v", semantics)
	}
}
