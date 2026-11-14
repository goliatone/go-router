# FeatureGate Middleware Task Plan – go-router

This document organizes the FeatureGate middleware implementation in `github.com/goliatone/go-router` into actionable phases and tasks. Mark tasks complete with `- [x]` as work progresses.

## Phase 1: Establish Middleware Package and Public API
**Reference:** "Proposed middleware" and "Configuration" sections of `MIDDLEWARE_TDD.md`

### Phase 1 Context
- **Why**  
  Feature flag scope/actor integration should live in a dedicated middleware package to keep the core router module clean and avoid a go-featuregate -> go-router dependency cycle.
- **What**  
  Create `middleware/featuregate` with a `New` constructor, option types, and resolver interfaces used to configure claims and actor injection.
- **How**  
  1. Add the package and public types (`Option`, `ClaimsResolver`, `ActorResolver`).  
  2. Define a private config struct with defaults (no-op resolver, strict false).  
  3. Wire functional options to mutate config.  
  4. Keep dependencies limited to go-featuregate `gate/scope` and go-router types.

### Phase 1 Tasks:
- [x] **Task 1.1**: Create `middleware/featuregate` package with `New(opts ...Option) router.Middleware`.
- [x] **Task 1.2**: Add `Option`, `ClaimsResolver`, `ActorResolver` types plus option constructors (`WithClaimsResolver`, `WithActorResolver`, `WithStrict`).
- [x] **Task 1.3**: Implement config defaults (no-op resolver, strict false) and option wiring.
- [x] **Task 1.4**: Add package doc comment describing intent and dependency boundaries.

## Phase 2: Resolve Claims and Inject Scope into Context
**Reference:** "Middleware behavior" section of `MIDDLEWARE_TDD.md`

### Phase 2 Context
- **Why**  
  go-featuregate derives scope from `context.Context`, while handlers use `router.Context`. Middleware must bridge this once per request.
- **What**  
  Resolve claims using a configured resolver and inject tenant/org/user IDs into the request context using `scope.WithTenantID`, `scope.WithOrgID`, and `scope.WithUserID`.
- **How**  
  1. Call `ClaimsResolver` when present.  
  2. Apply scope setters for non-empty values.  
  3. Write the updated context back to the router context.  
  4. If no resolver is configured, return `next(ctx)` without changes.

### Phase 2 Tasks:
- [x] **Task 2.1**: Implement middleware handler that reads `router.Context`, calls resolver, and writes updated `context.Context`.
- [x] **Task 2.2**: Map `gate.ActorClaims` to `scope` setters, skipping empty fields.
- [x] **Task 2.3**: Ensure no-op behavior when no resolver is configured.
- [x] **Task 2.4**: Add unit tests for scope injection and no-op behavior.

## Phase 3: Attach Optional Actor Metadata
**Reference:** "Middleware behavior" and "Configuration" sections of `MIDDLEWARE_TDD.md`

### Phase 3 Context
- **Why**  
  Some apps need actor metadata for audit hooks or mutation overrides, but go-featuregate does not consume actor refs automatically.
- **What**  
  Store a `gate.ActorRef` in context when an `ActorResolver` is configured, using a middleware-owned context key.
- **How**  
  1. Define an unexported context key type.  
  2. Attach the actor ref when non-empty.  
  3. Provide a small helper to retrieve the actor ref if useful.

### Phase 3 Tasks:
- [x] **Task 3.1**: Add an internal context key type and storage helper for actor refs.
- [x] **Task 3.2**: Implement actor injection when `WithActorResolver` is configured.
- [x] **Task 3.3**: Add tests covering actor injection and absence when unset.

## Phase 4: Implement Strict Mode and Error Handling
**Reference:** "Configuration" and "Behavioral notes" sections of `MIDDLEWARE_TDD.md`

### Phase 4 Context
- **Why**  
  Some apps require enforcement of required claims and should fail early if IDs are missing or invalid.
- **What**  
  Add `WithStrict(true)` to enable validation and return a router error (e.g., `router.ErrBadRequest`) with a clear message when validation fails.
- **How**  
  1. Define minimal required IDs in strict mode (tenant/org/user or subject).  
  2. Validate resolved claims; convert resolver errors into router errors.  
  3. Keep strict mode optional and off by default.

### Phase 4 Tasks:
- [x] **Task 4.1**: Define strict-mode validation rules and required ID fields.
- [x] **Task 4.2**: Implement validation and error conversion for strict mode.
- [x] **Task 4.3**: Add tests for strict mode error paths and success cases.

## Phase 5: Optional Helper and Adapter Deprecation
**Reference:** "Adapter necessity" and "Migration plan" sections of `MIDDLEWARE_TDD.md`

### Phase 5 Context
- **Why**  
  Some teams may want a small helper for tests or non-middleware flows, and any legacy adapter should point users to middleware.
- **What**  
  Optionally add `Context(ctx router.Context) context.Context` in the middleware package and deprecate any legacy adapter helper if present.
- **How**  
  1. Decide whether the helper is needed based on repo conventions.  
  2. If an adapter exists, mark it deprecated and update docs.

### Phase 5 Tasks:
- [x] **Task 5.1**: Decide on adding the `Context` helper and document the decision.
- [x] **Task 5.2**: If included, add the helper and tests in `middleware/featuregate`.
- [x] **Task 5.3**: Locate legacy adapter helpers and add deprecation notes if present.

Phase 5 Notes:
- Decision: include `featuregate.Context` as a convenience helper for non-middleware usage and tests.
- Adapter audit: no legacy adapter helpers found in this repository (no `routeradapter`, `ScopeSet`, or `WithRouterContext`).

## Phase 6: Documentation and Examples
**Reference:** "Example usage" and "Migration plan" sections of `MIDDLEWARE_TDD.md`

### Phase 6 Context
- **Why**  
  Adoption requires clear guidance on configuration, strict mode behavior, and usage with `gate.Enabled`.
- **What**  
  Document the middleware, option usage, strict mode expectations, and actor metadata behavior.
- **How**  
  1. Update `README.md` or a dedicated doc section.  
  2. Add a concise example in package docs.  
  3. Note that actor refs are optional and not consumed by go-featuregate automatically.

### Phase 6 Tasks:
- [x] **Task 6.1**: Add or update docs with middleware setup and resolver examples.
- [x] **Task 6.2**: Document strict mode requirements and error behavior.
- [x] **Task 6.3**: Add package-level example snippet for `gate.Enabled` usage.

## Phase 7: Test Coverage
**Reference:** "Goals" section of `MIDDLEWARE_TDD.md`

### Phase 7 Context
- **Why**  
  Middleware regressions are subtle; tests should validate context injection and error behavior across configurations.
- **What**  
  Add test coverage for resolver success/error, strict mode, no-op configuration, and actor metadata injection.
- **How**  
  1. Use router middleware test helpers to simulate a request.  
  2. Assert context values set by middleware.  
  3. Ensure error paths short-circuit correctly.

### Phase 7 Tasks:
- [x] **Task 7.1**: Add tests for claims resolution and scope injection.
- [x] **Task 7.2**: Add tests for strict mode error handling.
- [x] **Task 7.3**: Add tests for actor injection and retrieval.

---

**Next Steps**
- Confirm strict-mode claim requirements (tenant/org/user vs subject-only).
- Decide on adding the optional `Context` helper.
- Pick the doc target (README vs dedicated middleware doc).
