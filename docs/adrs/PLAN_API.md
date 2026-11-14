You are implementing an end-to-end enhancement in github.com/goliatone/go-router to improve platform-level routing safety for host apps (like go-admin) and external module mounting.

Context:
- Host apps need strong defaults for JSON vs UI surfaces, cross-module mounting, and upgrade-safe route evolution.
- Existing go-router already supports:
  - method+path conflict detection
  - path conflict modes (`strict`, `prefer_static` for Fiber)
  - strict startup validation via `Init()`
  - namespace/surface path resolution via `NamespaceResolver`
- Gap today:
  - named route collisions are silently overwritten in `BaseRouter` (`namedRoutes` map assignment)
  - no first-class policy utility for module/path ownership checks
  - no stable route manifest API for CI diff and discoverability

Goal:
Add strict-safe routing capabilities while preserving backward compatibility by default.

Required features (must implement):
1) Named-route collision policy
- Add explicit policy for named-route collisions with at least:
  - `replace` (default; current behavior for compatibility)
  - `error` (fail fast)
  - `skip` (keep existing, ignore new conflicting name)
- Collision means same route name already registered with a different full path.
- Same name reused for the same full path across multiple methods remains allowed for compatibility with current tests and route-name usage patterns.
- If same name+same method+same path, treat as idempotent (no error).
- Apply policy consistently across adapters (`FiberRouter`, `HTTPRouter`) and base registration flow, including post-registration `SetName(...)` updates.
- Integrate with strict startup validation so `error` surfaces during `Init()` in strict mode.
- Do not change existing public method signatures to return errors. Structured conflict errors should surface through validation results and strict-mode panics, consistent with current router behavior.
- Add typed/structured conflict errors with enough metadata:
  - route name, existing method/path, incoming method/path, policy
  - text code like `ROUTE_NAME_CONFLICT` (consistent with existing router error style).

2) Route ownership validation utility (library-level, adapter-agnostic)
- Add a validation package/API that can validate owned route sets against host/module ownership policy.
- Do not rely on bare `[]RouteDefinition` alone for ownership checks; require owner/module context in the input shape (for example `[]OwnedRouteSet` or `map[string][]RouteDefinition`).
- Policy capabilities:
  - reserved roots (e.g. `/api`, `/admin`) and owner assignment
  - module slug to allowed base prefix mapping
  - optional route-name prefix convention check (e.g. `translations.*` belongs to `translations` module)
  - conflict policy default `error`
- Validator must detect:
  - method+path collisions (reuse existing checks where possible)
  - route-name collisions
  - route mounted outside its declared owner prefix
  - two owners claiming same reserved root
- Keep this API optional and additive so existing users are unaffected unless they call it.
- Position this as complementary to `NamespaceResolver`, not a replacement for it.

3) Deterministic route manifest export
- Add a stable manifest API from registered routes.
- Manifest entry should include at least:
  - method, full path, route name
- Output must be deterministic (sorted) for CI snapshot/diff usage.
- Add helper to produce manifest from `[]RouteDefinition` as well as from router instances where practical (the current `Routes()` API is the natural adapter surface).

4) Manifest diff utility
- Add a utility to diff two manifests:
  - added routes
  - removed routes
  - changed routes when a stable identity key matches across manifests
- Deterministic output suitable for CI reporting.
- Define the identity strategy explicitly. Recommended default: use route name when non-empty and unique; otherwise fall back to add/remove classification instead of guessing.

Compatibility constraints:
- Do not break existing behavior by default.
- Default named-route policy must preserve current behavior (`replace`).
- All new strict behavior must be opt-in.

Design constraints:
- Follow existing code style and error patterns in go-router.
- Keep adapter behavior coherent:
  - Fiber supports `prefer_static`
  - HTTPRouter remains strict-only for path conflict mode
- Examples should use the actual adapter API shape (`SetName(...)` on `RouteInfo`; `.Name(...)` only where the route builder API is being used).
- No unrelated refactors.

Likely files:
- `router_base.go`
- `route_validation.go`
- `fiber.go`
- `httprouter.go`
- `router.go` (interfaces/types)
- new files for ownership/manifest/diff utilities and tests
- `README.md`
- `CHANGELOG.md`

Test requirements:
1. Named-route policy tests
- `replace` overwrites
- `skip` keeps original
- `error` returns conflict error with metadata
- idempotent same-name same-method same-path allowed
- same-name same-path across multiple methods remains allowed
- HTTPRouter post-registration `SetName(...)` updates participate in the shared named-route registry/policy

2. Adapter integration tests
- Fiber strict init + `error` policy fails fast on duplicate route names
- HTTP strict init + `error` policy fails fast on duplicate route names
- non-strict mode behavior remains backward compatible

3. Ownership validator tests
- reserved root ownership checks
- route outside owner prefix
- route-name prefix mismatch (if enabled)
- mixed module/host policy pass/fail cases

4. Manifest tests
- deterministic sort order
- manifest includes method/path/name as expected
- diff correctly classifies added/removed/changed

Documentation requirements:
- Update README:
  - named-route policy options and examples
  - ownership validation policy example for host+modules
  - manifest export and diff examples for CI
- Update CHANGELOG with a new version entry describing:
  - new policy knobs
  - compatibility defaults
  - strict opt-in behavior

Execution requirements:
1. Implement code.
2. Add/adjust tests.
3. Run `go test ./...`.
4. Ensure formatting/linting conventions in repo are respected.
5. Provide final summary:
  - API additions
  - migration notes
  - examples for host app strict boot profile.

Important:
- Do not ask me clarifying questions unless absolutely blocked.
- Make reasonable assumptions and complete implementation end-to-end in one pass.
