# PLAN_ROUTING_REFACTOR

## Summary

This document captures the long-term refactor needed to make go-router's named-route behavior coherent, durable, and safe for strict validation, helper-generated routes, downstream libraries, and CI tooling.

It exists because the first implementation of `PLAN_API.md` exposed structural issues:

- `SetName(...)` currently mutates the route's public name before collision resolution, so `skip` and `error` policies can leave `RouteName()` and `GetRoute(...)` inconsistent.
- helper-generated route names such as static routes and default websocket routes currently participate in the same global namespace as user-declared route names, which makes strict named-route validation unusable in common cases.
- ownership validation currently forces named-route error behavior via defaulting logic, which overrides explicit caller intent.
- manifests currently treat all names equally, even when some names should be considered internal implementation details rather than public identity.

The refactor must solve these issues systemically rather than by special-casing `static.get`, `websocket`, or other individual helpers.

The primary design principle is:

- distinguish a route's local/runtime name from a global named-route lookup claim

That separation lets the router support:

- request-context route names
- global route lookup by stable public name
- helper/internal route naming without polluting the public namespace
- strict validation over only the names that are intended to be globally unique
- predictable manifests and diffs

## Current State

Relevant current behavior in go-router:

- `RouteDefinition.Name` is the only route name field.
- `SetName(...)` writes `RouteDefinition.Name` and then triggers `onSetName(...)`.
- `BaseRouter` stores named routes in a global `namedRoutes map[string]*RouteDefinition`.
- adapters and helpers implicitly rely on `addNamedRoute(...)` as both:
  - route-name assignment
  - global lookup registration
  - collision enforcement
- helper-generated names currently include:
  - `static.get`
  - `static.head`
  - `websocket`
  - OpenAPI docs routes such as `openapi.json`, `openapi.yaml`, `openapi.docs`
- `ValidateRouteDefinitionsWithOptions(...)` now validates named-route conflicts when `NamedRoutePolicy == error`, but it reads only `RouteDefinition.Name` and cannot distinguish helper/internal names from public names.
- `ValidateOwnedRouteSets(...)` currently defaults named-route validation to `error` in a way that prevents an explicit caller choice of `replace`.

Downstream usage confirmed in `../go-crud`:

- `go-crud` uses explicit route names via `RouterRouteInfo.Name(...)`, which delegates to `RouteInfo.SetName(...)`.
- CRUD route names are stable and resource-scoped, e.g. `user:read`, `user:list`, `user:create`, `user:action:publish`.
- `go-crud` tests and metadata logic rely on those explicit names being globally retrievable.
- `go-crud` does not appear to depend on helper-generated go-router route names.
- `go-crud` examples use:
  - raw Fiber static serving in some places
  - `router.ServeOpenAPI(...)`
  - `crud.NewGoRouterAdapter(...)`

Compatibility implication:

- explicit `SetName(...)` must remain the public API for globally named routes
- only helper/internal naming should move out of the public collision domain

## Goals

1. Make named-route policies semantically correct:
   - `replace` means replace the public lookup binding
   - `skip` means keep the existing public name binding and do not assign the conflicting public name to the incoming route
   - `error` means keep the existing public binding, do not assign the conflicting public name to the incoming route, and surface a structured conflict for validation/strict startup

2. Remove helper-generated names from the public named-route namespace by default.

3. Preserve backward compatibility for explicit user naming:
   - `SetName(...)` remains the public route naming mechanism
   - `GetRoute(...)` continues to resolve explicit named routes
   - current default behavior remains `replace`

4. Make strict validation useful in real apps:
   - a single `Static()` registration must not fail strict named-route validation
   - multiple unnamed websocket routes must not fail strict named-route validation
   - OpenAPI-serving helpers must not consume user route-name namespace unless explicitly intended

5. Make manifests reflect public API surface by default, not router internals.

6. Keep the solution adapter-agnostic and centralized in shared router infrastructure rather than scattering special cases across Fiber and HTTPRouter.

## Non-Goals

- Do not change the public `Router` interface shape.
- Do not change `RouteInfo.SetName(...)` signature.
- Do not remove the current path conflict policy system.
- Do not introduce adapter-specific behavior differences for public named-route semantics.
- Do not make helper routes globally addressable by default unless they are explicitly promoted as public named routes.

## Target Model

### 1. Two route-name concepts

Introduce a conceptual separation between:

- local route name
  - used for request context
  - used for debugging/printing
  - may exist even when not globally unique
- public route lookup name
  - globally registered in the router
  - must obey named-route collision policy
  - is used by `GetRoute(...)`, redirects, strict startup validation, ownership validation, and public manifests

The implementation does not need two exported fields on `RouteDefinition`, but it must have two distinct internal concepts.

### 2. Public vs internal naming mode

Every name registration attempt must carry an internal mode:

- `public`
  - explicit user names set by `SetName(...)`
  - participates in collision policy
  - participates in `GetRoute(...)`
  - participates in strict validation
  - participates in ownership validation
  - participates in public manifests
- `internal`
  - helper-generated names from `Static()`, default websocket registration, OpenAPI-serving helpers, and other framework-owned routes
  - does not participate in public name collisions
  - does not occupy global `GetRoute(...)` namespace
  - does not trigger strict named-route validation
  - excluded from default public manifest identity logic

The router should be able to store an internal display/debug name for internal routes without turning it into a public lookup claim.

### 3. Transactional public name assignment

`SetName(...)` must become transactional.

Current broken behavior:

- `SetName("users.show")` writes `route.Name = "users.show"` first
- collision hook runs after the write
- `skip` or `error` may reject lookup registration while the route still reports the conflicting public name

Required behavior:

- the router must attempt the public registration first
- only on success should the route's effective public name be updated
- on `skip` or `error`, the route must retain its prior public/global name state

If the route previously had no public name, the result after failed registration should be no public name.

## Architecture Changes

### 1. Replace bare `namedRoutes map[string]*RouteDefinition` with a registry structure

Refactor `routerRoot` to own a richer registry, for example:

```go
type namedRouteBinding struct {
    Name   string
    Route  *RouteDefinition
    Mode   routeNameMode
}

type routeNameMode int

const (
    routeNameModeInternal routeNameMode = iota
    routeNameModePublic
)
```

Suggested `routerRoot` additions:

- `namedRoutes map[string]*namedRouteBinding`
- `namedRouteConflicts []error`

The exact shape may differ, but the registry must support:

- public lookup storage
- internal-only naming without lookup registration
- conflict recording
- future manifest filtering

### 2. Centralize all name registration in BaseRouter

Add shared helpers in `BaseRouter` and route all adapters/helpers through them. Suggested internal API:

- `applyPublicRouteName(route *RouteDefinition, name string) error`
- `applyInternalRouteName(route *RouteDefinition, name string)`
- `registerNamedRoute(route *RouteDefinition, name string, mode routeNameMode) error`
- `recordNamedRouteConflict(err error)`

No adapter or helper should write to `namedRoutes` directly.

### 3. Preserve a public lookup API

`GetRoute(name string)` remains public and should resolve only public names.

Internal/helper names must not be visible through `GetRoute(...)` unless explicitly promoted to public names.

### 4. Preserve `RouteName()` request context behavior

Request context should continue to expose the route's local/display name.

Implication:

- if a route has an internal helper name, `RouteName()` may return it
- if a user explicitly names a route successfully, `RouteName()` should return the public name
- if a public name assignment fails under `skip` or `error`, the route should not expose the rejected public name

This keeps request context and global registry logically consistent.

## Behavioral Rules

### Public name assignment

When calling `SetName(...)`:

1. Determine the route's current public name, if any.
2. Attempt to register the new name in public mode.
3. Resolve collision according to policy:
   - `replace`
     - bind the name to the incoming route
     - assign the public name to the route
   - `skip`
     - keep existing binding
     - leave incoming route's public name unchanged
     - return/store conflict information as non-fatal
   - `error`
     - keep existing binding
     - leave incoming route's public name unchanged
     - return/store conflict information for validation/strict init
4. If the route previously held another public name and the new name succeeds, remove or replace the old binding accordingly.

### Internal/helper names

Helpers must not call `SetName(...)` for internal names.

Instead, they should use a dedicated internal naming path.

Apply this to:

- `Static()` in Fiber and HTTPRouter
- default websocket route registration in Fiber and HTTPRouter
- `ServeOpenAPI(...)` helper routes if they currently use `SetName(...)`

### Validation

Named-route validation should operate over public route-name claims only.

It must ignore:

- internal helper names
- unnamed routes

It must continue to allow:

- same public name on the same path across multiple methods

It must continue to reject:

- same public name on different paths when policy is `error`

### Ownership validation

Ownership validation must use public names only.

It must not silently override caller policy choices.

The ownership helper should still provide a recommended strict default, but caller intent must win. Acceptable mechanisms:

- constructor/helper that returns strict defaults
- internal "explicitly set" tracking
- pointer-backed option representation

Do not use enum zero values as both:

- "unset"
- "replace"

### Manifest behavior

Default manifest behavior should represent public API surface.

Default manifest should:

- include all routes
- include public route names
- omit internal/helper names from `Name`

Alternative acceptable design:

- default manifest includes only public names in `Name`
- a future internal variant can expose internal/helper names if needed

Do not let helper-generated names create churn or artificial identity in CI diffs.

## go-crud Impact

This refactor must preserve go-crud compatibility.

Observed go-crud usage:

- `crud.NewGoRouterAdapter(...)` calls `RouteInfo.SetName(...)` for CRUD routes.
- controller tests expect named CRUD routes to be globally retrievable.
- CRUD route names are explicit and stable.

Compatibility contract:

- explicit `SetName(...)` continues to create public/global named routes
- `GetRoute(...)` continues to resolve those routes
- CRUD route manifests continue to expose those names
- no go-crud change should be required for basic route registration and lookup

Potential downstream tests to add in go-router:

- register a `go-crud`-style set of routes with explicit names and assert `GetRoute(...)` works
- ensure helper/internal routes do not interfere with CRUD route-name strict validation

Optional downstream follow-up in `../go-crud`:

- add one integration test using `crud.NewGoRouterAdapter(...)` plus `router.NewFiberAdapter(...)`
- assert a named CRUD route is retrievable through go-router's named-route lookup

## Implementation Sequence

### Phase 1. Refactor name registration core

Target files:

- `router_base.go`
- `meta.go`
- `router.go`

Tasks:

1. Introduce internal name registration mode in the shared router core.
2. Replace direct `namedRoutes map[string]*RouteDefinition` semantics with a registry abstraction.
3. Implement transactional public name assignment.
4. Ensure `SetName(...)` only updates effective public naming when the registration result allows it.
5. Preserve `GetRoute(...)` public behavior.

Acceptance:

- `replace`, `skip`, and `error` all produce self-consistent route state
- a skipped or rejected public name does not leak into `RouteName()` or manifests as if it succeeded

### Phase 2. Convert helpers to internal naming

Target files:

- `fiber.go`
- `httprouter.go`
- `openapi.go`

Tasks:

1. Replace helper `SetName(...)` calls with internal naming registration.
2. Ensure `Static()` no longer consumes public route-name namespace.
3. Ensure default websocket registration no longer consumes public route-name namespace.
4. Ensure OpenAPI helper routes are internal unless explicitly intended as public names.

Acceptance:

- one static mount does not fail strict named-route validation
- multiple websocket routes do not fail strict named-route validation unless explicitly named by user code
- OpenAPI helper routes do not pollute public route-name lookup

### Phase 3. Update shared validation and ownership logic

Target files:

- `route_validation.go`
- `route_ownership.go`
- `route_conflicts.go`

Tasks:

1. Update named-route validation to read public claims only.
2. Preserve same-name same-path-across-methods allowance.
3. Fix ownership defaulting so explicit `replace` remains possible.
4. Keep ownership validation strict by recommended default, but not by silent override.

Acceptance:

- named-route validation ignores internal/helper names
- ownership validation honors explicit caller route-name policy

### Phase 4. Update manifest generation

Target files:

- `route_manifest.go`

Tasks:

1. Define how internal/helper names appear in public manifests.
2. Ensure default manifest identity uses public names only.
3. Keep deterministic sorting and current diff behavior.

Acceptance:

- helper-generated routes do not create unstable public manifest identities
- explicit user-named routes still appear with stable names

### Phase 5. Documentation and compatibility tests

Target files:

- `README.md`
- `CHANGELOG.md`
- tests listed below

Tasks:

1. Document public vs internal/helper route naming.
2. Clarify that explicit `SetName(...)` creates a public lookup identity.
3. Document strict mode behavior for helper routes.
4. Document ownership validation defaults clearly.

## Required Tests

### Core router tests

- `replace` updates public lookup and effective route name
- `skip` preserves existing binding and does not apply rejected incoming public name
- `error` preserves existing binding and does not apply rejected incoming public name
- same name + same path + same method is idempotent
- same name + same path + different methods is allowed
- renaming an already named route updates binding coherently
- public lookup and route context remain consistent after every policy outcome

### Helper-route tests

- Fiber `Static()` succeeds under strict named-route error policy
- HTTPRouter `Static()` succeeds under strict named-route error policy
- multiple unnamed websocket routes succeed under strict named-route error policy
- explicitly named websocket routes still participate in public named-route collision policy
- `ServeOpenAPI(...)` routes do not fail strict named-route validation unless explicitly public

### Ownership tests

- explicit `replace` remains `replace`
- explicit `error` remains `error`
- default ownership policy helper produces strict behavior if that helper is used
- internal/helper names are ignored by ownership name-prefix validation

### Manifest tests

- public named routes appear in manifest names
- internal/helper routes do not appear as public names in default manifest
- diffs remain deterministic
- unchanged internal/helper routes do not create public manifest churn

### Compatibility tests

- go-router adapter path used by `go-crud` style naming still resolves with `GetRoute(...)`
- current context tests for explicit names remain green

## Migration / Rollout Notes

- This refactor should land before expanding strict named-route policy usage in host apps.
- If any current docs describe helper-generated names as public lookup names, those docs must be updated.
- If helper-generated names are currently used anywhere for redirects or lookups, those call sites must be migrated to explicit `SetName(...)` on user routes.
- Preserve backward-compatible defaults:
  - named-route default policy remains `replace`
  - explicit `SetName(...)` remains public/global
  - helper/internal route names become less externally visible, not more

## Open Questions To Resolve During Implementation

These should be decided during implementation and then reflected in code/tests/docs:

1. Should internal/helper names remain visible in `RouteName()` request context, or should unnamed helpers expose empty route names by default?
   - Recommended: keep local/internal names available for debugging, but do not expose them through public lookup or public manifests.

2. Should OpenAPI helper routes be internal by default?
   - Recommended: yes.

3. Should there be a future public API to export internal/helper names in manifests?
   - Not needed for this refactor. Defer unless a real consumer appears.

## Delivery Checklist

- refactor shared named-route registry
- convert helper-generated names to internal mode
- make `SetName(...)` transactional
- repair validation and ownership defaulting
- update manifest naming behavior
- add regression tests for the four reported issues
- update docs and changelog
- run `go test ./...`

## Success Criteria

The refactor is complete when all of the following are true:

- no helper-generated route names can break strict named-route validation by default
- `skip` and `error` policies do not leave route state inconsistent
- explicit user-named routes still behave as globally named routes
- ownership validation no longer overrides explicit caller intent
- public manifests reflect public route identity rather than internal helper implementation details
- go-crud explicit route naming continues to work unchanged
