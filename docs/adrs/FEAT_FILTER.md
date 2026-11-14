# Feature Proposal – Route Filtering for ServeOpenAPI

## Summary
Expand `go-router`’s OpenAPI support so callers can exclude non-API routes (e.g., HTML pages) without rewriting application code. The new functionality should let users keep the current one-line usage (`router.ServeOpenAPI(...)`) while selectively filtering which routes get emitted into the generated OpenAPI document.

## Background
Recent updates make `ServeOpenAPI` automatically harvest route metadata from the router supplied by the caller. In `go-crud`’s web demo all JSON endpoints and HTML views share the root router. When `ServeOpenAPI(app.Router(), ...)` runs, it now includes front-end routes such as `/users/:id/edit` alongside true REST endpoints, leading to confusing duplicates in Stoplight Elements.

We want the experience to remain “call `ServeOpenAPI` once,” yet allow consumers to keep front-end routes out of the API spec. Pushing that responsibility to every app breaks the DX goal.

## Requirements
1. Provide an opt-in mechanism to filter route definitions before they’re merged into the OpenAPI document.
2. Preserve the existing defaults (no filter → include all routes).
3. Avoid forcing callers to add extra wiring elsewhere; configuration belongs in `ServeOpenAPI` options.
4. Avoid public API churn wherever possible; build atop the existing option pattern.

## Proposed API

Introduce a new OpenAPI option:

```go
type RouteFilter func(RouteDefinition) bool

func WithRouteFilter(filter RouteFilter) OpenAPIOption
```

Usage example (from `go-crud` demo):

```go
renderer := router.NewOpenAPIRenderer(router.OpenAPIRenderer{ /* title, version, etc. */ })

router.ServeOpenAPI(
    app.Router(),
    renderer,
    router.WithRouteFilter(func(def router.RouteDefinition) bool {
        return strings.HasPrefix(def.Path, "/api/")
    }),
)
```

Possible convenience helper:

```go
func WithRoutePrefix(prefix string) OpenAPIOption {
    return WithRouteFilter(func(def RouteDefinition) bool {
        return strings.HasPrefix(def.Path, prefix)
    })
}
```

## Implementation Details

1. **Config struct**  
   Extend `openAPIConfig` (in `openapi.go`) with a new field:
   ```go
   routeFilter RouteFilter
   ```

2. **Option wiring**  
   Implement `WithRouteFilter` (and optionally `WithRoutePrefix`) to populate `cfg.routeFilter`.

3. **ServeOpenAPI integration**  
   In the logic that gathers routes from the router (prior to `AppenRouteInfo`), apply the filter:
   ```go
   defs := r.Routes()
   if cfg.routeFilter != nil {
       filtered := make([]RouteDefinition, 0, len(defs))
       for _, def := range defs {
           if cfg.routeFilter(def) {
               filtered = append(filtered, def)
           }
       }
       defs = filtered
   }
   renderer.AppenRouteInfo(defs)
   ```
   Ensure this occurs on every request or before each `GenerateOpenAPI()` invocation, consistent with the current lazy-generation flow.

4. **Documentation**  
   Update `README.md` or OpenAPI usage docs to mention `WithRouteFilter` / `WithRoutePrefix`.

5. **Testing**  
   - Unit tests for `WithRouteFilter`: confirm filtered definitions are excluded.
   - Integration coverage to assert that front-end routes vanish when the filter is applied but remain when no filter is provided.

## Open Questions
- Should we supply more advanced filters (regex, method-based, tag-based)? For now, path filtering covers the immediate need.
- Do we need to expose multiple filters? We can accept multiple options (each `WithRouteFilter` override the previous) or allow chaining if necessary.

## Impact
This change keeps the “one-call” API intact while letting applications with mixed route types present a clean OpenAPI document. `go-crud` can invoke `ServeOpenAPI(app.Router(), renderer, router.WithRoutePrefix("/api"))` and retain root-level HTML routes without duplicate entries in docs.
