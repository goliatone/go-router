# Relation Metadata Plan – go-router Enhancements

This plan breaks down the implementation of the relation metadata hooks described in `docs/relation_metadata_research.md` (Section “Proposed Hooks for go-router”). Each phase is structured for issue/PR tracking, with checkboxes ready for execution.

## Phase 1: Introduce Relation Provider Interfaces
**Reference:** `docs/relation_metadata_research.md` – RelationMetadataProvider, RelationDescriptor

### Phase 1 Context
- **Why**  
  We need a reusable contract for building relation trees so downstream packages can plug in custom logic.
- **What**  
  Define core types (`RelationNode`, `RelationDescriptor`, `RelationMetadataProvider`, `RelationFilterFunc`) and baseline behaviour.
- **How**  
  1. Create new types in a dedicated module (e.g., `meta_relations.go`).  
  2. Provide a default provider that mirrors existing Bun-tag reflection (lifted from the research doc).  
  3. Expose filter hooks that no-op by default and can be chained.

### Phase 1 Tasks:
- [x] **Task 1.1**: Define `RelationNode`, `RelationDescriptor`, and `RelationMetadataProvider` including error contracts and field documentation.
- [x] **Task 1.2**: Implement default provider that walks struct relations using Bun/CRUD tags and populates nodes.
- [x] **Task 1.3**: Add `RelationFilterFunc` chain support with helper to register global filters.
- [x] **Task 1.4**: Cover new types with focused unit tests (e.g., relation tree creation, filter chaining).

## Phase 2: Wire Providers into Metadata Aggregator
**Reference:** `docs/relation_metadata_research.md` – Aggregator Extension Point, Per-Resource Overrides

### Phase 2 Context
- **Why**  
  Metadata extraction must call providers automatically and allow overrides per resource with a safe fallback.
- **What**  
  Extend `MetadataAggregator` and `ResourceMetadata` to capture relation metadata from providers.
- **How**  
  1. Add default provider registration (`WithRelationProvider`) and per-resource overrides (`WithRelationProviders`).  
  2. Track originating reflect.Type inside `ResourceMetadata` to resolve overrides.  
  3. Ensure `Compile()` invokes providers, applies filters, and stores results for later OpenAPI emission.

### Phase 2 Tasks:
- [x] **Task 2.1**: Add default/per-resource provider registration APIs on `MetadataAggregator`, including tests for fallback logic.
- [x] **Task 2.2**: Embed originating `reflect.Type` (or equivalent identifier) within `ResourceMetadata` and update constructors.
- [x] **Task 2.3**: Invoke providers during `Compile()`, run registered filter hooks, and attach descriptors to metadata.
- [x] **Task 2.4**: Add aggregator tests that verify default provider usage, per-resource overrides, and filter application.

## Phase 3: Emit Relation Metadata via OpenAPI Extension
**Reference:** `docs/relation_metadata_research.md` – Resource-Level Opt-in, OpenAPI Vendor Extension

### Phase 3 Context
- **Why**  
  Downstream tools (e.g., go-crud) need relation info in the OpenAPI spec under a predictable vendor extension.
- **What**  
  Serialize `RelationDescriptor` into `x-formgen-relations` on each resource schema.
- **How**  
  1. Extend OpenAPI conversion helpers to detect relation descriptors.  
  2. Ensure serialization captures includes, relations, and tree data in a consumer-friendly shape.  
  3. Provide integration tests covering generated specs and extension contents.

### Phase 3 Tasks:
- [x] **Task 3.1**: Update `convertSchemaToOpenAPI` (or a dedicated helper) to emit `x-formgen-relations` when relation metadata exists.
- [x] **Task 3.2**: Define JSON structure for the extension (includes list, relations array, tree snapshot) and document invariants in code comments.
- [x] **Task 3.3**: Add integration tests that generate OpenAPI docs and assert extension presence and shape.

## Phase 4: Documentation & Examples
**Reference:** `README.md`, `docs/metadata_*`, `schema.example.json`

### Phase 4 Context
- **Why**  
  Consumers need guidance on registering providers, using filter hooks, and reading the new vendor extension.
- **What**  
  Refresh documentation, examples, and sample specs to showcase relation metadata.
- **How**  
  1. Update README metadata sections with provider/extension usage.  
  2. Add a dedicated doc entry summarizing configuration steps.  
  3. Refresh sample schema (`schema.example.json`) to include `x-formgen-relations`.

### Phase 4 Tasks:
- [x] **Task 4.1**: Document provider registration, per-resource overrides, and filter hooks in `README.md` (metadata/OpenAPI section).
- [x] **Task 4.2**: Expand `docs/metadata_migration_guide.md` (or new doc) with step-by-step integration instructions and caveats.
- [x] **Task 4.3**: Update `schema.example.json` (or new example) to demonstrate the `x-formgen-relations` extension and note how to consume it.
- [x] **Task 4.4**: Include changelog or release notes entry summarizing relation metadata support for ecosystem consumers.
