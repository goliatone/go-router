# go-router Enhancements for go-crud Integration

This note collects the context and requirements that originate in `go-crud`, but must be satisfied inside the `go-router` repository. The focus is on improving the metadata/openAPI generation so that `go-crud` (and other downstream packages) can delegate schema hints to `go-router` without duplicating logic.

## Background

- `go-crud` controllers expose a `/resource/schema` endpoint that depends on `router.MetadataAggregator` + `GenerateOpenAPI()` (`go-crud/controller.go:268-283`).
- The metadata backing that OpenAPI document is assembled by `router.GetResourceMetadata()` which calls `ExtractSchemaFromType()` and `buildRoutesMetadata()` (`go-router/meta_generator.go:21-214`).
- Form builders (`go-formgen`) want to infer default label fields, pagination defaults, and relation capabilities directly from that OpenAPI payload to avoid client-side duplication.
- Currently, `go-crud` is forced to bolt on some of these hints at runtime, which leads to drift between the documented contract and actual controller behavior.

## Goals

1. Extend schema metadata so that OpenAPI components expose a recommended display label field via a vendor extension (e.g. `x-formgen-label-field`).
2. Ensure pagination/query parameters are formally documented and reusable in the generated OpenAPI spec, with defaults and descriptions kept in one place.
3. Evaluate whether relation/include metadata should also be surfaced from `go-router`, and identify the hooks needed if we decide to do it there instead of in `go-crud`.

## 1. Recommended Label Field Extension

### Current State
- `ExtractSchemaFromType()` can capture all struct tags, but only if `IncludeTagMetadata` is enabled (`go-router/meta_extract.go:320-347`).
- `GetResourceMetadata()` invokes `ExtractSchemaFromType()` with default options (tag metadata disabled), so OpenAPI schemas lack tag-derived hints (`meta_generator.go:21-29`).
- `convertSchemaToOpenAPI()` simply mirrors `SchemaMetadata.Properties` without any extensions (`meta_openapi.go:286-293`).

### Desired Behavior
- When a struct field contains either `crud:"label:<alias>"`, or an explicit Bun tag that already marks a logical display column, mark a single property on the entity schema with `x-formgen-label-field: true` (or provide the field name at the schema root).
- This should work out of the box for any consumer of `MetadataAggregator`, not just `go-crud`.

### Proposed Steps
1. Flip on tag capture when `GetResourceMetadata()` calls `ExtractSchemaFromType()` (set `IncludeTagMetadata: true`). Measure the impact on schema size and extraction cost—existing perf docs (`docs/metadata_performance_guide.md`) suggest a modest overhead.
2. Add a helper in `meta_generator.go` to scan the resulting `SchemaMetadata` for the best label candidate:
   - Prefer an explicit `crud:"label:<field>"`.
   - Fallback to repository identifier hints if exposed (open question—`go-router` might not know about repository handlers, so we may need a hook).
3. Modify `convertSchemaToOpenAPI()` so that it copies a `labelField` marker into `components.schemas.{Resource}` as an OpenAPI extension:
   ```yaml
   components:
     schemas:
       user:
         type: object
         x-formgen-label-field: "name"
         properties:
           name:
             type: string
   ```
4. Update and/or add tests covering the new metadata:
   - Unit tests around `convertSchemaToOpenAPI`.
   - Extraction tests that confirm the Tag metadata is present when enabled (`extract_meta_test.go`, `schema_integration_test.go`).
5. Document the new extension in `README.md` and metadata guides so other consumers know it exists.

### Open Questions
- Should the extension live on the schema root (`x-formgen-label-field: "name"`) or on the property (`properties.name.x-formgen-label-field: true`)? The former seems easier for consumers that need the field name only once.
- Do we need to support a different tag namespace (e.g. `formgen:"label"`) to avoid overloading `crud`?

## 2. Stable Pagination & Query Parameters

### Current State
- `buildRoutesMetadata()` enumerates query parameters such as `limit`, `offset`, `select`, and `order`, but repeats them inline for every route (`meta_generator.go:96-215`).
- Defaults (25/0) are embedded in the schema, yet the README/OpenAPI docs don’t call out the contract or reuse the parameter definitions.
- Form generators and API clients want consistent, machine-readable parameter components to avoid parsing route-level duplication.

### Desired Behavior
- Provide canonical `components.parameters` entries for pagination and filtering parameters, and reference them from each path operation.
- Ensure descriptions cover supported syntax (e.g., `order=name asc`), and match the behavior implemented in `go-crud`’s query builder.
- Keep README/documentation in sync with the generated OpenAPI so downstream teams can rely on the published defaults.

### Proposed Steps
1. Refactor `buildRoutesMetadata()` to populate shared parameter definitions (e.g., `Ma.Parameters["Limit"] = Parameter{...}`) and reference them via `$ref` from each route.
2. Expose the same defaults the runtime uses (`DefaultLimit`, `DefaultOffset` in `go-crud`, or equivalent constants exported in go-router if necessary).
3. Update OpenAPI generation to emit those components (`MetadataAggregator.Components["parameters"]`), keeping backward compatibility for existing consumers that expect inline parameters.
4. Document the parameters in `README.md` / guides, including examples that match go-crud handler behavior.
5. Add tests that assert the generated OpenAPI document contains the shared parameters and correct defaults.

### Open Questions
- Should the default values remain hard-coded in go-router, or should we allow go-crud (or other callers) to override them via configuration before compiling metadata?
- Are there query parameters (`select`, `include`, `{field}__op`) that should also move into reusable components, or should we leave complex filter syntax documented inline for clarity?

## 3. Relation/Include Metadata (Exploratory)

### Context
- `go-crud` currently computes allowed include paths and relation filters at runtime and echoes them under `$meta.relations` in list responses (`go-crud/query.go:440-499`, `response.go:33-41`).
- Surfacing the same information in the schema would let clients know in advance which nested filters are legal.
- However, `go-router` lacks awareness of repository/service overrides that may restrict relations dynamically, so there is a risk of stale documentation.

### Considerations for go-router
- `buildRelationMetadata()` in `go-crud/query_config.go:93-145` already walks Bun relations and caches them. Porting that logic to go-router would allow metadata extraction without `go-crud`.
- If we move the feature into go-router, we need extension points so frameworks can prune relations based on their own rules (e.g., injecting a filter that runs before `ConvertSchemaToOpenAPI`).
- Because of the potential drift, this piece may remain in go-crud for now, but we should capture the hooks required should we revisit:
  - Ability to register a relation filter provider when compiling metadata.
  - Mechanism to reconcile runtime service-level restrictions with statically extracted relations.

## Deliverables Checklist

- [ ] Option and helper to enable tag metadata inside `GetResourceMetadata`.
- [ ] Logic to choose a `label` field and emit `x-formgen-label-field` in OpenAPI output.
- [ ] Shared OpenAPI components for pagination/filter parameters with updated documentation.
- [ ] Regression tests covering both enhancements.
- [ ] Decision recorded (and hooks identified) regarding whether relation/include metadata belongs in go-router or remains in go-crud.

Keeping this document in the repository will make it easier to open a dedicated implementation plan PR or issue in `go-router` and track work without losing the cross-repo context.
