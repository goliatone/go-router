package router

import (
	"errors"
	"strings"
	"testing"

	goerrors "github.com/goliatone/go-errors"
)

func TestBaseRouterNamedRoutePolicyReplaceOverwritesLookup(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicyReplace,
		root:             &routerRoot{},
	}

	first := &RouteDefinition{Method: GET, Path: "/users/:id", Name: "users.show"}
	second := &RouteDefinition{Method: GET, Path: "/members/:id", Name: "users.show"}

	if err := br.addNamedRoute(first.Name, first); err != nil {
		t.Fatalf("unexpected error adding first route: %v", err)
	}
	if err := br.addNamedRoute(second.Name, second); err != nil {
		t.Fatalf("unexpected error replacing route: %v", err)
	}

	if got := br.GetRoute("users.show"); got != second {
		t.Fatalf("expected latest route to win, got %+v", got)
	}
}

func TestBaseRouterNamedRoutePolicySkipKeepsOriginalLookup(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicySkip,
		root:             &routerRoot{},
	}

	first := &RouteDefinition{Method: GET, Path: "/users/:id", Name: "users.show"}
	second := &RouteDefinition{Method: GET, Path: "/members/:id", Name: "users.show"}

	if err := br.addNamedRoute(first.Name, first); err != nil {
		t.Fatalf("unexpected error adding first route: %v", err)
	}
	if err := br.addNamedRoute(second.Name, second); err == nil {
		t.Fatal("expected skip policy to return a conflict error")
	}

	if got := br.GetRoute("users.show"); got != first {
		t.Fatalf("expected original route to remain registered, got %+v", got)
	}
	if second.effectivePublicName() != "users.show" {
		t.Fatalf("expected direct addNamedRoute to keep second route public name metadata for compatibility, got %q", second.effectivePublicName())
	}
}

func TestValidateRouteDefinitionsWithOptions_NamedRoutePolicyError(t *testing.T) {
	routes := []*RouteDefinition{
		{Method: GET, Path: "/users/:id", Name: "users.show"},
		{Method: GET, Path: "/members/:id", Name: "users.show"},
	}

	errs := ValidateRouteDefinitionsWithOptions(routes, RouteValidationOptions{
		NamedRoutePolicy: NamedRouteCollisionPolicyError,
	})
	if len(errs) != 1 {
		t.Fatalf("expected 1 named-route conflict, got %d", len(errs))
	}

	var routeErr *goerrors.Error
	if !errors.As(errs[0], &routeErr) {
		t.Fatalf("expected go-errors.Error, got %T", errs[0])
	}
	if routeErr.TextCode != "ROUTE_NAME_CONFLICT" {
		t.Fatalf("expected ROUTE_NAME_CONFLICT, got %q", routeErr.TextCode)
	}
	if routeErr.Metadata["route_name"] != "users.show" {
		t.Fatalf("expected route_name metadata to be users.show, got %#v", routeErr.Metadata["route_name"])
	}
	if routeErr.Metadata["existing_path"] != "/users/:id" {
		t.Fatalf("expected existing_path metadata, got %#v", routeErr.Metadata["existing_path"])
	}
	if routeErr.Metadata["incoming_path"] != "/members/:id" {
		t.Fatalf("expected incoming_path metadata, got %#v", routeErr.Metadata["incoming_path"])
	}
}

func TestBaseRouterNamedRoutePolicyIdempotentForSameMethodAndPath(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicyError,
		root:             &routerRoot{},
	}

	first := &RouteDefinition{Method: GET, Path: "/users/:id", Name: "users.show"}
	second := &RouteDefinition{Method: GET, Path: "/users/:id", Name: "users.show"}

	if err := br.addNamedRoute(first.Name, first); err != nil {
		t.Fatalf("unexpected error adding first route: %v", err)
	}
	if err := br.addNamedRoute(second.Name, second); err != nil {
		t.Fatalf("expected identical name/method/path to be idempotent, got %v", err)
	}
}

func TestValidateRouteDefinitionsWithOptions_AllowsSameNameAndPathAcrossMethods(t *testing.T) {
	routes := []*RouteDefinition{
		{Method: GET, Path: "/users/:id", Name: "users.show"},
		{Method: POST, Path: "/users/:id", Name: "users.show"},
	}

	errs := ValidateRouteDefinitionsWithOptions(routes, RouteValidationOptions{
		NamedRoutePolicy: NamedRouteCollisionPolicyError,
	})
	if len(errs) != 0 {
		t.Fatalf("expected same name on same path across methods to be allowed, got %d errors", len(errs))
	}
}

func TestBaseRouterValidateRoutesIncludesLateRouteNames(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicyError,
		root:             &routerRoot{},
	}

	br.addLateRoute(GET, "/assets", func(Context) error { return nil }, "static.get")
	br.addLateRoute(GET, "/assets/*filepath", func(Context) error { return nil }, "static.get")

	errs := br.ValidateRoutes()
	if len(errs) == 0 {
		t.Fatal("expected late-route named conflicts to be validated")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_NAME_CONFLICT") {
		t.Fatalf("expected named-route conflict, got %q", errs[0].Error())
	}
}

func TestRouteDefinitionSetNameSkipLeavesExistingRouteStateUntouched(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicySkip,
		root:             &routerRoot{},
	}

	first := &RouteDefinition{Method: GET, Path: "/users/:id"}
	first.onSetName = func(route *RouteDefinition, name string) error {
		return br.applyPublicRouteName(route, name)
	}
	second := &RouteDefinition{Method: GET, Path: "/members/:id", Name: "websocket", nameMode: routeNameModeInternal}
	second.onSetName = func(route *RouteDefinition, name string) error {
		return br.applyPublicRouteName(route, name)
	}
	br.root.routes = []*RouteDefinition{first, second}

	first.SetName("users.show")
	second.SetName("users.show")

	if got := br.GetRoute("users.show"); got != first {
		t.Fatalf("expected original route binding to remain, got %+v", got)
	}
	if second.Name != "websocket" {
		t.Fatalf("expected conflicting SetName to preserve runtime name, got %q", second.Name)
	}
	if second.effectivePublicName() != "" {
		t.Fatalf("expected conflicting SetName to avoid assigning a public name, got %q", second.effectivePublicName())
	}
}

func TestRouteDefinitionSetNameErrorRecordsConflictAndLeavesRouteUntouched(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicyError,
		root:             &routerRoot{},
	}

	first := &RouteDefinition{Method: GET, Path: "/users/:id"}
	first.onSetName = func(route *RouteDefinition, name string) error {
		return br.applyPublicRouteName(route, name)
	}
	second := &RouteDefinition{Method: GET, Path: "/members/:id"}
	second.onSetName = func(route *RouteDefinition, name string) error {
		return br.applyPublicRouteName(route, name)
	}
	br.root.routes = []*RouteDefinition{first, second}

	first.SetName("users.show")
	second.SetName("users.show")

	if got := br.GetRoute("users.show"); got != first {
		t.Fatalf("expected original route binding to remain, got %+v", got)
	}
	if second.Name != "" {
		t.Fatalf("expected rejected SetName to leave runtime name unchanged, got %q", second.Name)
	}
	if second.effectivePublicName() != "" {
		t.Fatalf("expected rejected SetName to leave public name empty, got %q", second.effectivePublicName())
	}
	errs := br.namedRouteConflicts()
	if len(errs) != 1 {
		t.Fatalf("expected one recorded named-route conflict, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_NAME_CONFLICT") {
		t.Fatalf("expected ROUTE_NAME_CONFLICT, got %q", errs[0].Error())
	}
}

func TestRouteDefinitionSetNameReplaceRemovesOldBinding(t *testing.T) {
	br := &BaseRouter{
		namedRoutePolicy: NamedRouteCollisionPolicyReplace,
		root:             &routerRoot{},
	}

	route := &RouteDefinition{Method: GET, Path: "/users/:id"}
	route.onSetName = func(route *RouteDefinition, name string) error {
		return br.applyPublicRouteName(route, name)
	}
	br.root.routes = []*RouteDefinition{route}

	route.SetName("users.show")
	route.SetName("members.show")

	if got := br.GetRoute("users.show"); got != nil {
		t.Fatalf("expected old binding to be removed, got %+v", got)
	}
	if got := br.GetRoute("members.show"); got != route {
		t.Fatalf("expected renamed route binding, got %+v", got)
	}
	if route.Name != "members.show" {
		t.Fatalf("expected runtime name to update to new public name, got %q", route.Name)
	}
}
