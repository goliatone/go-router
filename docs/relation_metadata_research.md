# Relation Metadata Extraction – Research Notes

## 1. Current Behaviour in `go-crud`

- **Entry Points**
  - `registerQueryConfig` / `getRelationMetadataForType` (`go-crud/query_config.go:49-86`) cache a `relationMetadata` tree per resource type.
  - `buildRelationMetadata` recursively walks Bun relations declared via `bun:"rel:..."` tags, respecting `crud:"-"` exclusions and `json` aliases (`query_config.go:88-157`).
  - The resulting tree captures:
    - `relationName` (struct field name/alias)
    - `fields` for each model (using repository-provided `FieldMapProvider`)
    - `children` relation nodes (lower-cased + JSON aliases)
- **Repository Dependencies**
  - Optional `FieldMapProvider` comes from repositories that implement `GetModelFields()` (`query_config.go:25-47`), enabling custom column aliases (e.g., locales, JSONB).
  - Bubbles through `buildRelationMetadata` so each node knows which filter keys/columns are legal.
- **Runtime Usage**
  - Request parsing builds `relationIncludeNode` trees, merges duplicates (`go-crud/query.go:400-460`).
  - `collectIncludeDetails` flattens the tree into two outputs (`query.go:462-499`):
    - `Include []string` (paths like `authors.books.publisher`)
    - `Relations []RelationInfo` (filters per relation path)
  - Responses embed these details in `$meta.relations` (`response.go:11-45`), keeping clients informed about legal includes + relation-level filters actually applied.
- **Key Observations**
  - Metadata is derived at runtime using actual repository state (includes custom field maps, service overrides).
  - Relation filters can inject arbitrary SQL column names (`relationFilter.column`), so safe publication requires knowledge of repository-level overrides.
  - Caching is type-based, but invalidated implicitly by process restarts; there is no active watch for repository mutations.

## 2. Proposed Hooks for `go-router`

- **Goal**: allow `go-router` metadata generation to expose relation/include hints without binding to `go-crud` specifics, while permitting downstream frameworks to veto or enrich data.

- **Suggested Abstractions**
  1. **RelationMetadataProvider interface**
     ```go
     type RelationMetadataProvider interface {
         BuildRelationTree(resourceType reflect.Type) (*router.RelationNode, error)
     }
     ```
     - Default implementation mirrors `go-crud`’s `buildRelationMetadata` using reflection + Bun tags.
     - Frameworks can supply an alternate provider (e.g., `go-crud` injects repository-aware provider).
  2. **Aggregator Extension Point**
     ```go
     func (ma *MetadataAggregator) WithRelationProvider(provider RelationMetadataProvider) *MetadataAggregator
     ```
     - Stored on the aggregator as the default provider; used whenever a resource does not declare its own override.
     - Providers run before `Compile()` finalizes `ma.Components`.
  3. **Per-Resource Overrides**
     - Support optional overrides via `WithRelationProviders(map[reflect.Type]RelationMetadataProvider)` (or equivalent registration helper).
     - During compilation, look up the provider for the given resource type; fall back to the default when no override exists.
  3. **Resource-Level Opt-in**
     - `ResourceMetadata` gains an optional `Relations *RelationDescriptor` payload (tree + flattened includes).
     - `convertSchemaToOpenAPI` (or a sibling helper) emits vendor extensions (`x-formgen-relations`) so OpenAPI consumers can ingest paths + supported filters.
  4. **Filter Hooks**
     - Allow a chain of `RelationFilterFunc(type reflect.Type, node *RelationNode) *RelationNode` that can prune or decorate relations prior to emission.
     - Default implementation returns node unchanged; frameworks register callbacks (e.g., to remove soft-deleted relations or apply ACL rules).

- **Data Model Sketch**
  ```go
  type RelationNode struct {
      Name       string
      Display    string   // optional alias
      Fields     []string // legal filter/select fields
      Children   map[string]*RelationNode
      Operators  []string // supported filter operators (optional)
  }
  type RelationDescriptor struct {
      Tree      *RelationNode
      Includes  []string
      Relations []RelationInfo // mirrors go-crud response payload
  }
  ```

- **Integration Flow**
  1. `GetResourceMetadata` invokes the registered provider (if any) with the resource type.
  2. Provider returns a `RelationDescriptor`; aggregator stores it on `ResourceMetadata`.
  3. Before `Compile`, optional `RelationFilterFunc`s run to mutate the descriptor.
  4. OpenAPI conversion layer serializes descriptor into predictable extensions/components.

## 3. Risks, Open Questions, and Decision Log

- **Stale Documentation**
  - Repository/service overrides (e.g., dynamic ACLs, tenant-specific filters) may hide relations at runtime; static extraction could expose paths clients cannot actually use.
  - **Mitigation**: make emission opt-in; require frameworks to provide a provider + filter callback acknowledging ownership of drift.
- **Performance**
  - Deep relation graphs can be expensive; need caching similar to `go-crud` with explicit invalidation hooks when providers change state.
- **Security**
  - Publishing raw filter columns may leak internal schema; providers should surface sanitized field identifiers and optional operator sets.
  - Need guidance on redacting sensitive relations (e.g., password resets).
- **API Format**
  - **Decision**: export relation metadata via an OpenAPI vendor extension (`x-formgen-relations`) for the resource schema; revisit dedicated components only if tooling requirements arise.
- **Multiplicity of Providers**
  - **Decision**: support an optional per-resource provider registry layered on top of a default provider. Aggregator resolves overrides first, then falls back automatically.
- **Next Steps**
  - Spike a minimal provider in go-router that mirrors existing Bun tag traversal.
  - Prototype extension emission and evaluate size/cost on real schema.
  - Sync with go-crud team on preferred override strategy (e.g., service-level filters, cached descriptors).

---

**Summary**: go-crud currently derives relation metadata dynamically using repository-aware helpers and surfaces the results at runtime. To move this responsibility into go-router safely, we need pluggable providers and filter hooks so frameworks control the final relation set, along with careful consideration of drift, performance, and security.
