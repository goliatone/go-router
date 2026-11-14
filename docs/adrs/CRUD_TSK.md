# CRUD Task Plan – go-router Metadata Enhancements

This document organizes the metadata enhancements required inside `github.com/goliatone/go-router` into actionable phases and tasks. Mark tasks complete with `- [x]` as work progresses.

## Phase 1: Surface Recommended Label Field in OpenAPI
**Reference:** Section 1 of `ROUTER_FEAT.md` – "Recommended Label Field Extension"

### Phase 1 Context
- **Why**  
  `go-crud` and UI builders (e.g., `go-formgen`) need a canonical display field. Without tag metadata, generated schemas omit that hint and force consumers to patch metadata at runtime.
- **What**  
  Always capture struct tags during metadata extraction, compute the label field, and emit it as an OpenAPI vendor extension (e.g., `x-formgen-label-field`).
- **How**  
  1. Enable tag capture when `GetResourceMetadata` calls `ExtractSchemaFromType`.  
  2. Add a helper that selects the best label candidate (prefer `crud:"label:<field>"`).  
  3. Extend `convertSchemaToOpenAPI` to attach the vendor extension on each schema.  
  4. Cover extraction, selection, and OpenAPI output with focused tests and update docs accordingly.

### Phase 1 Tasks:
- [x] **Task 1.1**: Update `router/meta_generator.go` to call `ExtractSchemaFromType` with `IncludeTagMetadata: true`, verifying the guidance in `docs/metadata_performance_guide.md`.
- [x] **Task 1.2**: Implement a label-selection helper in `router/meta_generator.go` that prefers `crud:"label:<name>"` tags and leaves TODO hooks for future fallbacks.
- [x] **Task 1.3**: Modify `router/meta_openapi.go` so `convertSchemaToOpenAPI` emits `x-formgen-label-field` while guarding against conflicting hints.
- [x] **Task 1.4**: Add unit tests (e.g., `meta_generator_test.go`, `meta_openapi_test.go`) confirming tag metadata extraction and OpenAPI output include the label field.
- [x] **Task 1.5**: Update `README.md` and related metadata guides to document the extension and tagging expectations.

## Phase 2: Publish Shared Pagination & Query Parameter Components
**Reference:** Section 2 of `ROUTER_FEAT.md` – "Stable Pagination & Query Parameters"

### Phase 2 Context
- **Why**  
  Pagination/query parameters (`limit`, `offset`, `select`, `order`, etc.) are duplicated per route in the OpenAPI output. Clients need reusable components with consistent defaults (limit `25`, offset `0`) that match runtime behaviour.
- **What**  
  Centralize parameter definitions inside `MetadataAggregator`, reference them via `$ref`, and keep docs/tests in sync with the generated OpenAPI.
- **How**  
  1. Refactor `buildRoutesMetadata` to register canonical parameter entries once.  
  2. Replace inline parameter definitions with references to the shared components.  
  3. Ensure the OpenAPI generator emits the components and reflects runtime defaults.  
  4. Document the parameters and verify with tests that the spec stays consistent.

### Phase 2 Tasks:
- [x] **Task 2.1**: Extract parameter construction into helpers that populate shared entries for `limit`, `offset`, `select`, `order`, and other reusable query parameters.
- [x] **Task 2.2**: Update route metadata assembly to replace inline parameter structs with `$ref` pointers to the shared definitions.
- [x] **Task 2.3**: Ensure `MetadataAggregator.GenerateOpenAPI` includes the shared parameters under `components.parameters` without breaking existing consumers.
- [x] **Task 2.4**: Expand tests (e.g., `meta_openapi_test.go`, `router_base_test.go`) to assert shared parameters exist once, carry correct defaults/descriptions, and are referenced by operations.
- [x] **Task 2.5**: Refresh `README.md` (pagination/query section) with canonical defaults and examples aligned with the generated OpenAPI.

## Phase 3: Capture Relation/Include Metadata Hooks (Research Spike)
**Reference:** Section 3 of `ROUTER_FEAT.md` – "Relation/Include Metadata (Exploratory)"

### Phase 3 Context
- **Why**  
  `go-crud` currently derives include metadata at runtime. Exposing similar information in go-router would provide early visibility to clients but risks drifting from service-level restrictions.
- **What**  
  Document the hooks needed to safely extract and filter relation metadata within go-router, paving the way for a future implementation.
- **How**  
  1. Review the relation walker inside `go-crud` to understand required inputs and filters.  
  2. Define extension points (e.g., pluggable filter/provider) that let frameworks tailor relation exposure before metadata finalizes.  
  3. Record risks, assumptions, and acceptance criteria for moving this responsibility into go-router.

### Phase 3 Tasks:
- [x] **Task 3.1**: Summarize the current relation metadata flow from `go-crud` (e.g., `query_config.go:93-145`, `query.go:440-499`) and identify dependencies on repository handlers.
- [x] **Task 3.2**: Propose a pluggable API for `MetadataAggregator` (relation filter/provider) that enables safe integration without mandating adoption.
- [x] **Task 3.3**: Document risks, open questions, and decision points for bringing relation metadata into go-router, capturing outcomes for future spikes.

---

**Next Steps**
- Focus on Phase 1 to unblock schema-driven form generation, then Phase 2 for spec consistency.
- Log decisions from Phase 3 in an issue or design record so future spikes start with shared context.
