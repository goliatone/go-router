---
title: go-router FeatureGate Middleware
status: draft
owner: go-featuregate / go-router
scope: scope + actor context injection, adapter deprecation
---

# go-router FeatureGate Middleware

## Context

go-featuregate resolves feature flags based on a standard `context.Context`.
Scope is derived from context via a `gate.ClaimsProvider` (the default uses
`scope.ClaimsFromContext`) and turned into a `gate.ScopeChain`.
In go-router handlers, the primary context type is `router.Context`, which
wraps a standard context and request metadata.

A small `routeradapter` previously bridged `router.Context` to
`context.Context` and provided a convenience resolve option. It did not set
scope; it simply exposed helpers to read scope from a context that already
contained it. This design doc captures a middleware-based approach for
populating scope (and optional actor data) directly into the request context
so handlers can call `gate.Enabled(ctx.Context(), key)` without extra glue.

## Goals

- Populate actor claims (tenant/org/user, optional roles/perms) via
  `context.Context` in a single place.
- Keep handler code simple: `gate.Enabled(ctx.Context(), "key")`.
- Avoid per-call boilerplate or dedicated adapter helpers.
- Provide optional actor metadata for audit hooks and override writes.
- Keep dependencies minimal and avoid a go-featuregate -> go-router cycle.

## Non-goals

- Implementing storage adapters or admin overrides.
- Defining tenant/org/user identity resolution rules (app-specific).
- Adding go-featuregate as a hard dependency of go-router.

## Proposed middleware

### Package and ownership

Middleware should live in go-router as an optional integration package, for
example:

- `github.com/goliatone/go-router/middleware/featuregate`

The package depends on go-featuregate `scope` and `gate` to write scope and
actor references into the request context. This keeps the core router module
clean while providing an official integration path.

### Middleware behavior

The middleware runs once per request and:

1. Derives actor claims (tenant/org/user/subject, optional roles/perms)
   using a configurable resolver function.
2. Writes those values into the request `context.Context` using
   `scope.WithTenantID`, `scope.WithOrgID`, `scope.WithUserID` (subject maps
   to user).
3. Optionally attaches actor metadata for audit logging or override writes
   (if the hosting application wants it), using a context key defined in the
   middleware package.
4. Passes the updated context back to the router for downstream handlers.

Note: the default `ClaimsProvider` only reads tenant/org/user from context.
If role/perm scopes are needed, configure a custom `ClaimsProvider` that
reads those values from middleware context.

Handlers then call feature-gate directly using the standard context:

```go
func handler(ctx router.Context) error {
    allowed, err := gate.Enabled(ctx.Context(), "users.signup")
    if err != nil {
        return err
    }
    if !allowed {
        return router.ErrNotFound
    }
    return nil
}
```

### Configuration

The middleware accepts an options struct with functional options:

- `WithClaimsResolver(func(router.Context) (gate.ActorClaims, error))`
  - Returns tenant/org/user/subject IDs (and optional roles/perms).
- `WithActorResolver(func(router.Context) gate.ActorRef)` (optional)
  - Provides actor metadata for audit hooks or mutation logging.
- `WithStrict(bool)` (optional)
  - If true, fails early (returns an error) when required IDs are missing.
  - If false, omits missing IDs and relies on the default scope chain.

Defaults:

- If no resolver is provided, the middleware is a no-op.
- Strict mode defaults to false.

### API sketch

```go
package featuregatemw

type Option func(*config)

type ClaimsResolver func(router.Context) (gate.ActorClaims, error)

type ActorResolver func(router.Context) gate.ActorRef

func New(opts ...Option) router.Middleware

func WithClaimsResolver(resolver ClaimsResolver) Option

func WithActorResolver(resolver ActorResolver) Option

func WithStrict(strict bool) Option
```

Behavioral notes:

- `ClaimsResolver` should return an error when strict mode is enabled and
  required IDs are missing.
- When strict mode fails, the middleware should return a router error
  (e.g., `router.ErrBadRequest`) with a clear message.
- Actor metadata should only be attached if provided and non-empty.
- Actor metadata attached by middleware is not consumed automatically by
  go-featuregate; callers still pass an explicit `gate.ActorRef` for
  mutations.

## Adapter necessity

With middleware in place, the old adapter becomes unnecessary for most users:

- The adapter only provided `Context(ctx)` and scope helpers.
- Middleware populates the standard context directly, so feature-gate can use
  `scope.ClaimsFromContext(ctx)` without any special helper.

There are still two narrow cases where a helper could be useful:

1. **Non-middleware usage**: small apps that do not want any middleware and
   prefer explicit per-handler scope resolution.
2. **Testing helpers**: convenience helpers for converting a
   `router.Context` to `context.Context` in tests.

If we keep a helper, it should live next to the middleware (in go-router) to
avoid a go-featuregate dependency on go-router. The helper would be:

- `func Context(ctx router.Context) context.Context`

Legacy `ScopeSet` and `WithRouterContext` helpers are redundant once middleware is the
recommended approach. They can be omitted to keep the API small.

## Example usage

```go
mw := featuregatemw.New(
    featuregatemw.WithClaimsResolver(func(ctx router.Context) (gate.ActorClaims, error) {
        return gate.ActorClaims{
            TenantID:  ctx.Params("tenant_id"),
            OrgID:     ctx.Params("org_id"),
            SubjectID: ctx.Local("user_id").(string),
        }, nil
    }),
    featuregatemw.WithActorResolver(func(ctx router.Context) gate.ActorRef {
        return gate.ActorRef{ID: ctx.Local("user_id").(string)}
    }),
)

app.Use(mw)
```

## Alternatives

- Keep the adapter only: does not solve the scope population problem; still
  requires per-handler options.
- Push scope derivation into feature-gate: couples gate logic to router and
  complicates reuse in non-router contexts.

## Migration plan

1. Add middleware package under go-router.
2. Update docs in go-router to show middleware usage.
3. Deprecate any legacy router adapter helpers (if they exist) in favor of
   middleware-based scope population.

## Open questions

- Should strict mode default to true in production apps?
- Should the middleware support a per-request override of `ScopeChain`
  (e.g., a value already stored in context)?
