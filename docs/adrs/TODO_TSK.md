# TODO Debt Plan – go-router

This plan captures outstanding TODO and forward-looking code comments across the repository. Tasks are grouped by related workstreams and follow the same structure used in `RELATION_TSK.md`. Each checkbox is ready for issue/PR tracking.

NOTE: If you need to use the go command (i.e. go test) use the full path: /Users/goliatone/.g/go/bin/go test

## Phase 1: Router Bootstrap & Static Responses
**Reference:** `fiber.go`, `router_base.go`, `route.go`

### Phase 1 Context
- **Why**
  Current adapter setup and static serving hooks leave TODO markers about configuration timing, per-router reuse, and path safety.
- **What**
  Align router bootstrapping so options can be composed before instantiation, allow static responses to be rewritten with access to the payload, and harden path handling.
- **How**
  1. Introduce a deferred fiber app builder that applies option functions before creating the instance.
  2. Extend static response hooks to read/modify the response body and propagate the bytes back to the sender.
  3. Ensure router mounts reuse cached group routers and sanitize route paths before registration.

### Phase 1 Tasks:
- [ ] **Task 1.1 P3** (`fiber.go:34-44`): Refactor `NewFiberAdapter` so option functions run before `fiber.New`, supporting asynchronous option accumulation prior to adapter initialization.
- [ ] **Task 1.2 P2** (`router_base.go:306-313`): Change `Static.ModifyResponse` to accept and return the response body (`func(Context, []byte) ([]byte, error)`) and update callers to send the possibly mutated payload.
- [ ] **Task 1.3 P3** (`fiber.go:179-198`): Make `FiberRouter.Mount` mirror `Group` behaviour by caching sub-routers in `r.routers[prefix]` and returning the singleton instance.
- [ ] **Task 1.4 P2** (`route.go:125-131`): Implement route path sanitization (e.g., default `/`, leading slash enforcement, collapse duplicate separators) before assigning to `Route.definition.Path`.

## Phase 2: Router Interface Cohesion
**Reference:** `router.go`

### Phase 2 Context
- **Why**
  The core interfaces carry TODOs about naming clarity and segregation of concerns, signalling API polish work.
- **What**
  Rename body parsing to match semantics, move introspection helpers behind purpose-built interfaces, and reevaluate redundant type shells.
- **How**
  1. Audit downstream usage of `Context.Bind` and plan a `ParseBody` rename with shims or deprecations.
  2. Extract route enumeration helpers into a dedicated metadata interface to decouple runtime routing from introspection.
  3. Decide whether `PrefixedRouter` should merge into `Router[T]` or be replaced by an accessor on the main interface.

### Phase 2 Tasks:
- [ ] **Task 2.1 P4** (`router.go:147`): Rename `Context.Bind` to `ParseBody` (or introduce an alias) and update adapters/tests to preserve compatibility.
- [ ] **Task 2.2 P3** (`router.go:247-251`): Move `Routes()` into a separate `MetaRouter` (or similar) interface so production routers are not forced to expose debugging hooks.
- [ ] **Task 2.3 P4** (`router.go:254-258`): Evaluate folding `PrefixedRouter.GetPrefix()` into `Router[T]` (or providing an alternative) and remove the extra interface if redundant.

## Phase 3: Metadata Extraction & Tagging
**Reference:** `meta_extract.go`, `meta_generator.go`, `meta_openapi.go`

### Phase 3 Context
- **Why**
  Metadata extraction has TODOs covering embedded table names, relationship/property modelling, type coverage, and label/tag enrichment.
- **What**
  Capture table metadata from embedded structs, clarify how relationships surface in schemas, enhance map/special-type handling, and improve label/tag derivation.
- **How**
  1. Use `bun.BaseModel` (and similar) metadata even when anonymous fields are skipped.
  2. Decide whether relationships should appear in both `properties` and `relationships`, documenting the chosen shape.
  3. Support map schemas via `additionalProperties` and extend special-type detection beyond UUID.
  4. Improve label resolution by honouring alias directives and falling back to repository/service hints.
  5. Populate tag descriptions from metadata so OpenAPI tags carry meaningful text.

### Phase 3 Tasks:
- [ ] **Task 3.1 P2** (`meta_extract.go:114-124`): Extract and store table names from skipped anonymous fields (e.g., `bun.BaseModel`) so metadata retains table context.
- [ ] **Task 3.2 P2** (`meta_extract.go:282-287`): Decide and implement how relationship fields should surface in `properties` vs. `relationships`, ensuring documentation reflects the model.
- [ ] **Task 3.3 P2** (`meta_extract.go:530-540`): Emit `additionalProperties` schemas (with inferred types) when encountering `reflect.Map` fields.
- [ ] **Task 3.4 P3** (`meta_extract.go:567-576`): Expand `mapScalarType` special-case handling (e.g., `time.Time`, `sql.Null*`, `decimal`) and add tests covering the new types.
- [ ] **Task 3.5 P2** (`meta_generator.go:124-139`): Support cross-property label aliases by resolving alias directives to existing properties or surfacing conflicts explicitly.
- [ ] **Task 3.6 P3** (`meta_generator.go:145-148`): Implement fallbacks that use repository/service hints when no explicit `crud:"label"` tag is present.
- [ ] **Task 3.7 P4** (`meta_openapi.go:275-281`): Source descriptive text for tags (from metadata annotations or defaults) instead of leaving OpenAPI tag descriptions empty.

## Phase 4: OpenAPI Renderer Cleanup
**Reference:** `openapi.go`

### Phase 4 Context
- **Why**
  Duplicate top-level metadata fields remain alongside `OpenAPIInfo`, carrying a TODO to remove them.
- **What**
  Eliminate redundant `Title`, `Version`, `Description`, and related fields from `OpenAPIRenderer` in favour of the nested `Info` object.
- **How**
  1. Audit code paths that set or read the duplicate fields.
  2. Migrate usage to `OpenAPIRenderer.Info`.
  3. Delete the legacy fields and adjust constructors/tests accordingly.

### Phase 4 Tasks:
- [ ] **Task 4.1 P4** (`openapi.go:64-78`): Remove duplicate renderer metadata fields in favour of the `Info` struct and update initialization/helpers to rely solely on `Info`.

## Phase 5: Test Debt TODOs
**Reference:** `websocket_advanced_test.go`, `builder_test.go`, `examples/router/main_test.go`

### Phase 5 Context
- **Why**
  Several tests are skipped or simplified due to visibility and mocking gaps, and tooling upgrades are noted as TODOs.
- **What**
  Expose the types needed for advanced WebSocket tests, modernize router mocks, and align framework expectations across test suites.
- **How**
  1. Export or shim the WebSocket JSON message types so black-box tests can exercise real handlers.
  2. Update the mock WebSocket context to satisfy the interface and re-enable the skipped router tests.
  3. Replace hand-rolled router mocks with generated gomock fixtures.
  4. Normalize REST behaviour assertions between Fiber and HTTPRouter examples.

### Phase 5 Tasks:
- [ ] **Task 5.1 P2** (`websocket_advanced_test.go:17-40`): Expose `router.JSONMessage` (or supply a public test helper) so JSON message helper tests no longer rely on ad-hoc structs.
- [ ] **Task 5.2 P2** (`websocket_advanced_test.go:71-108`): Update the WebSocket mock context to satisfy `router.WebSocketContext`, restore real JSON message routing assertions, and re-enable the skipped test.
- [ ] **Task 5.3 P4** (`builder_test.go:335-360`): Replace the hand-written `MockRouter` with gomock-generated stubs (`mockgen -source=router.go`) to honour the TODO.
- [ ] **Task 5.4 P2** (`examples/router/main_test.go:330-340`): Harmonize missing `Content-Type` behaviour across adapters so the example tests assert the same status code regardless of framework.

## Phase 6: UI Schema Enrichment
**Reference:** `UIS_TDD.md` (§6, §7, §9)

### Phase 6 Context
- **Why**
  OpenAPI output lacks `x-relationships`/`x-endpoint` hints needed for UI builders. Aligning with the contract in `author.schema.json` removes downstream patchwork.
- **What**
  Enrich schema extraction, extend relationship models, emit property-level extensions, and surface configuration hooks for endpoints.
- **How**
  1. Preserve relational properties during extraction and capture aliasing between scalar FKs and object fields (§6.1, §7.1).
  2. Expand `RelationshipInfo` with cardinality, inverse, and endpoint hints while introducing `EndpointHint` (§6.2, §7.2, §7.3).
  3. Teach the OpenAPI renderer to emit `$ref`, `x-relationships`, and `x-endpoint` for both object and scalar fields (§6.3).
  4. Provide `UISchemaOptions` so callers can supply defaults/overrides without tags (§6.4).
  5. Backfill tests, fixtures, and docs to demonstrate the new metadata (§9).

### Phase 6 Tasks:
- [x] **Task 6.1 P1**: Update `ExtractSchemaFromType` to retain relational properties, populate `RelationAliases`, and parse endpoint-related `crud` directives (`UIS_TDD.md` §6.1, §7.1).
- [x] **Task 6.2 P1**: Extend `RelationshipInfo` and introduce `EndpointHint`, wiring new fields through metadata aggregation (`UIS_TDD.md` §6.2, §7.2, §7.3).
- [x] **Task 6.3 P1**: Refactor the OpenAPI renderer to add `$ref`, `x-relationships`, and `x-endpoint` blocks for object and scalar relation fields (`UIS_TDD.md` §6.3).
- [x] **Task 6.4 P2**: Implement `UISchemaOptions`, default endpoint derivation, and override plumbing on `MetadataAggregator` (`UIS_TDD.md` §6.4).
- [x] **Task 6.5 P2**: Ensure related component schemas are registered/emitted when relations reference external types (`UIS_TDD.md` §6.3, §8).
- [x] **Task 6.6 P2**: Add unit/integration tests validating extraction and rendered OpenAPI metadata against fixtures (`UIS_TDD.md` §9).
- [x] **Task 6.7 P3**: Refresh documentation and example schema (`schema.example.json`, README segments) to showcase the enriched metadata (`UIS_TDD.md` §9, §10).

## Phase 7: Fiber View/Locals Merge Strategy
**Reference:** `fiber.go`, `fiber_test.go`, `quickstart/error_fiber.go`

### Phase 7 Context
- **Why**
  Error pages currently lose `Locals` data when user view maps overlap, forcing downstream code to pass `nil` binds to get correct status codes.
- **What**
  Add a configurable merge strategy for view bind vs. `Locals`, defaulting to "view wins with warning," while allowing apps to override per-key resolution.
- **How**
  1. Introduce a merge strategy type/config on the Fiber adapter and default implementation that logs when locals are overridden.
  2. Use the strategy inside `fiberContext.Render` to merge `Locals` with serialized view data, honoring the selected precedence.
  3. Provide an adapter option/helper for apps (e.g., quickstart error handler) to choose alternative resolution (locals-first or custom).
  4. Backfill tests to cover default and custom strategies and document the behavior for downstream consumers.

### Phase 7 Tasks:
- [x] **Task 7.1 P2** (`fiber.go`): Define a `RenderMergeStrategy` type and adapter config field, plus a default implementation that warns when view data overwrites `Locals`.
- [x] **Task 7.2 P1** (`fiber.go`): Update `fiberContext.Render` to serialize binds to a map, merge `Locals` via the strategy (view wins by default), and pass the merged `fiber.Map` to `c.ctx.Render`.
- [x] **Task 7.3 P2** (`fiber.go`): Add a public adapter option to inject a custom merge strategy so applications (e.g., quickstart) can prefer locals or handle conflicts case-by-case.
- [x] **Task 7.4 P3** (`fiber.go`): Expose a helper to coerce arbitrary bind + locals into a merged `fiber.Map` for plain Fiber handlers to reuse outside go-router contexts.
- [x] **Task 7.5 P1** (`fiber_test.go`): Add tests covering default (view-wins) merge with warning, custom locals-first strategy, and non-collision merges.
- [x] **Task 7.6 P3** (`README.md` or docs): Document merge precedence, collision logging, and how to supply a custom strategy in adapter initialization.
