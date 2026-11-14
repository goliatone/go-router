# UI Schema Enrichment – Architecture Design Document

## Table of Contents
1. [Overview](#overview)
2. [Design Goals](#design-goals)
3. [Guiding Principles](#guiding-principles)
4. [Problem Statement](#problem-statement)
5. [Target Experience](#target-experience)
6. [Architecture Overview](#architecture-overview)
   1. [Schema Extraction](#schema-extraction)
   2. [Relationship Metadata Model](#relationship-metadata-model)
   3. [OpenAPI Rendering](#openapi-rendering)
   4. [Configuration & Overrides](#configuration--overrides)
   5. [Caching & Performance](#caching--performance)
7. [Data Contracts](#data-contracts)
   1. [SchemaMetadata](#schemametadata)
   2. [RelationshipInfo](#relationshipinfo)
   3. [EndpointHint](#endpointhint)
8. [Integration Points](#integration-points)
9. [Testing Strategy](#testing-strategy)
10. [Implementation Phases](#implementation-phases)
11. [Out of Scope](#out-of-scope)

## Overview

This document captures the technical design for enriching go-router’s metadata pipeline so that generated OpenAPI schemas provide downstream UI builders (e.g., go-formgen, go-crud’s admin UI) with actionable hints. The goal is to match the expectations demonstrated in `author.schema.json` and `go-formgen`’s fixtures while keeping the router’s metadata system modular and efficient.

## Design Goals

- **UI-Ready Metadata**: Emit relationship and endpoint hints (`x-relationships`, `x-endpoint`) that allow clients to render typeahead controls, has-one/has-many widgets, and nested editors without hardcoded knowledge.
- **Zero Manual Context**: After the work is complete, a fresh session can implement features by following TODO tasks and this document alone.
- **Composable Extensions**: Provide hooks for repositories/frameworks (go-crud) to adjust or override metadata without forking core logic.
- **Backward Compatibility**: Preserve existing schema consumers; new extensions must be additive and behind helper structs.

## Guiding Principles

1. **Single Source of Truth** – Relationship metadata is extracted once from structs/Bun tags and reused for both schema properties and relation trees.
2. **Schema-Centric** – The OpenAPI spec is the contract. Runtime responses should not contain more information than the schema.
3. **Configurable Defaults** – Provide useful defaults (e.g., endpoint URLs), but allow overrides via struct tags or programmatic hooks.
4. **Performance Awareness** – Metadata extraction runs during boot. Added work must respect existing caching and avoid per-request penalties.

## Problem Statement

Today’s generated spec (see `schema.example.json`) lacks:

- Properties for relational fields (e.g., `author`, `tags`) in addition to scalar FK fields.
- `x-relationships` extensions describing cardinality, foreign keys, or inverse names.
- `x-endpoint` hints specifying where the UI retrieves options, search params, paging limits, etc.
- Consistent propagation of relation descriptors to both scalar backing fields (`author_id`) and embedded objects (`author`).

The absence of these hints forces go-formgen/go-crud to post-process the schema or inject metadata out-of-band, resulting in divergence between documentation and runtime behavior.

## Target Experience

The generated OpenAPI document should have per-property metadata similar to `author.schema.json`, including:

- `$ref` links to related components.
- `x-relationships` with camelCase relation types (`belongsTo`, `hasOne`, `hasMany`, `manyToMany`), explicit cardinality, and source/target information.
- `x-endpoint` blocks covering URLs, methods, default query params, dynamic param interpolation, and mode hints (e.g., `search`).
- Array items referencing component schemas for collection relations.

Downstream consumers must be able to render feature-rich forms using only the schema.

## Architecture Overview

### Schema Extraction

1. **Relationship Preservation** – Update `ExtractSchemaFromType` to retain relational fields in `SchemaMetadata.Properties` while continuing to populate `SchemaMetadata.Relationships`. Each relational property gets a synthesized `PropertyInfo` capturing type (object vs array) and pointer semantics. *(See §7.1 for struct details.)*

2. **Scalar/Relation Linkage** – Track the source backing field for each relationship (`author_id` → `author`) during extraction. Both entries share the same `RelationshipInfo` reference so they emit consistent extensions. *(See §6.1 and §7.2.)*

3. **Custom Tag Handlers** – Extend the existing tag handler map to read `crud` directives that describe endpoint behavior (e.g., `crud:"endpoint:/api/authors"`). Programmatic overrides are also exposed via options (see §6.4).

### Relationship Metadata Model

A revamped `RelationshipInfo` struct (see §7.2) adds:

- Normalized `Cardinality` (`one`, `many`).
- `Inverse` relation name when known.
- `SourceField` (the FK field name).
- `Endpoint` pointer carrying the `EndpointHint`.
- Optional operator list for filter UIs (reused by `RelationDescriptor`).

`RelationDescriptor` continues to power `x-formgen-relations`, reusing the enriched info to avoid divergence.

### OpenAPI Rendering

Changes to `convertSchemaToOpenAPI` (§6.3):

1. Merge `SchemaMetadata.Properties` with `SchemaMetadata.Relationships` so relational properties emit:
   - `$ref` or `items.$ref` to component schemas.
   - `x-relationships` extension built from `RelationshipInfo`.
   - `x-endpoint` extension when `EndpointHint` is present.

2. Inject matching extensions into scalar FK properties based on `SourceField`.

3. Preserve existing metadata (type/format/description) for compatibility.

### Configuration & Overrides

We surface configuration via a new `UISchemaOptions` struct (§6.4):

- `EndpointDefaults` – function that derives endpoint hints from `RelationshipInfo`.
- `EndpointOverrides` – map keyed by resource + relation for explicit hints (mirrors go-formgen’s override model).
- `RelationFilters` – additional filters applied before rendering extensions.

`MetadataAggregator.WithUISchemaOptions` stores options and reuses them when compiling resources.

### Caching & Performance

- Extraction caches remain type-based. Additional fields are appended to cached `SchemaMetadata`.
- Endpoint defaults are computed once per resource during aggregation.
- No per-request cost; `Compile()` continues to precompute the OpenAPI document.

## Data Contracts

### SchemaMetadata

New/updated fields:

```go
type SchemaMetadata struct {
    Properties      map[string]PropertyInfo
    Relationships   map[string]RelationshipInfo
    RelationAliases map[string]string // optional, scalar field → relation name
}
```

`RelationAliases` prevents double-bookkeeping by linking `author_id` to `author`.

### RelationshipInfo

```go
type RelationshipInfo struct {
    RelationType    string // legacy identifier (has-one, has-many, belongs-to, many-to-many)
    Cardinality     string // "one" or "many"
    RelatedTypeName string
    SourceField     string // FK field name (e.g., author_id)
    ForeignKey      string
    TargetColumn    string
    Inverse         string
    Endpoint        *EndpointHint
    Filters         []RelationFilter // reused for x-formgen-relations
}
```

`RelationType` remains for backward compatibility; new code prefers `Cardinality`.

### EndpointHint

```go
type EndpointHint struct {
    URL           string
    Method        string
    LabelField    string
    ValueField    string
    Params        map[string]string
    DynamicParams map[string]string
    Mode          string // e.g., "search"
    SearchParam   string
    SubmitAs      string // e.g., "json"
}
```

This mirrors go-formgen’s expectations and aligns with the overrides API in `pkg/orchestrator`.

## Integration Points

1. **Struct Tags** – `crud` tags gain optional directives: `endpoint`, `labelField`, `valueField`, `param:<key>=<value>`, `dynamicParam:<key>=<value>`, `mode`, `searchParam`, `submitAs`, `inverse`. Tags are optional; defaults fall back to programmatic options.

2. **Programmatic Options** – Downstream frameworks can register callbacks via `MetadataAggregator.WithUISchemaOptions` to override/augment hints without altering struct tags.

3. **Relation Filters** – Existing `RegisterRelationFilter` hooks remain applicable; they see enriched descriptors and can redact relations before rendering.

4. **Component Registration** – Aggregator ensures related types (e.g., `Author`, `Tag`) are registered as providers so `$ref` targets exist.

## Testing Strategy

1. **Unit Tests**
   - `meta_extract_test.go`: verify relational properties are preserved and linked to scalar fields (§6.1).
   - `meta_openapi_test.go`: assert `x-relationships`/`x-endpoint` serialization (§6.3).
   - `meta_relations_openapi_test.go`: validate relation descriptor still matches `x-formgen-relations`.

2. **Integration Tests**
   - Generate an OpenAPI doc for a demo type mirroring `author.schema.json` and compare against fixtures (permissive comparison on ordering).
   - Ensure go-formgen parsing (mocked) recognizes the emitted extensions.

3. **Docs & Examples**
   - Update `schema.example.json` and README to illustrate new metadata.
   - Provide sample struct definitions showing tag usage and programmatic overrides.

## Implementation Phases

1. **Extraction Enhancements (§6.1)** – Preserve relational properties, track aliases, and capture endpoint directives from tags.
2. **Model Expansion (§6.2 & §7.2)** – Extend `RelationshipInfo`, add `EndpointHint`, and wire aggregation options.
3. **Renderer Updates (§6.3)** – Emit property-level extensions and ensure referenced components exist.
4. **Configuration & Overrides (§6.4)** – Introduce `UISchemaOptions`, default endpoint derivation, and override plumbing.
5. **Testing & Documentation (§9)** – Add unit/integration coverage and refresh sample artifacts.

## Out of Scope

- Runtime response changes (e.g., modifying `/api/...` handlers) – existing responses may continue to include relation metadata but are not part of this effort.
- Authorization-aware metadata pruning – frameworks may layer custom filters via existing hooks but enforcement is not implemented here.
- Automatic discovery of repository/service hints beyond the provided hooks; consumers must supply overrides for domain-specific rules.

