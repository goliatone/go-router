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

func TestFiberRegistrationSerializesConcurrentCollection(t *testing.T) {
	server := NewFiberAdapter()
	r := server.Router()
	const count = 32

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
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
