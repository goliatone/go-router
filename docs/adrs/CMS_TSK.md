# CMS Task Plan – go-crud Enhancements

This document captures the architectural context and implementation roadmap for evolving `github.com/goliatone/go-crud` so it can serve the CMS vertical slices defined in `/Users/goliatone/Development/GO/src/github.com/goliatone/go-cms/docs/CMS_TDD.md`, `/Users/goliatone/Development/GO/src/github.com/goliatone/go-cms/docs/CMS_IMP.md`, and `/Users/goliatone/Development/GO/src/github.com/goliatone/go-cms/docs/CMS_ENTITIES.md`. The tasks below extend the CRUD controller while preserving backward compatibility for existing consumers. Each section details the rationale (**Why**), target behaviour (**What**), implementation outline (**How**), and discrete tasks (**Tasks**) required to deliver the feature.

## 1. Delegate CRUD Operations to a Service Layer

- **Why**
  The CMS services orchestrate domain rules (publish windows, translation sync, audit attribution) that cannot live inside generic repository calls. Direct repository invocation in `Controller[T]` prevents those invariants.

- **What**
  Introduce an optional service interface that mirrors CRUD operations, allowing controllers to hand off requests to richer domain logic while maintaining the current repository-based fallback.

- **How**
  Define a `Service[T]` contract with methods like `Create`, `Update`, `Delete`, and `List`, each accepting the CRUD `Context`. Add a `WithService` option to bind an implementation. In each handler, prefer the service when present; otherwise use the existing repository call path. Ensure ID assignment continues to leverage `Repo.Handlers()` and preserve response handling semantics.

- **Tasks**
  - Design `Service[T]` interface and wire `WithService` option.
  - Update controller handlers (`Create`, `Update`, `Delete`, batch variants, `Index`, `Show`) to call the service when supplied.
  - Provide default service implementation that wraps the repository to satisfy legacy behaviour.
  - Extend controller tests to validate delegation and fallback paths.

## 2. Extend Query Builder for Nested Relations and Locale Filters

- [x] 2. Extend Query Builder for Nested Relations and Locale Filters

- **Why**
  CMS entities rely on deep includes (e.g., `Page.Blocks.Translations`) and locale-scoped fetches. Current `include` parsing only supports a single relation with simple equality filters, limiting API usefulness.

- **What**
  Enhance `BuildQueryCriteria` to parse dotted relation chains, multiple relation clauses, and structured predicates (including locale code filters) while safeguarding against injection.

- **How**
  Expand `parseRelation` to recursively build relation trees and attach filter descriptors (field, operator, value). For each relation level, apply Bun `Relation` selectors with nested `SelectRelation` helpers, and allow binding query parameters like `include=Blocks.Translations.locale__eq=es`. Document supported syntax and validation rules.

- **Tasks**
  - Refactor relation parsing into a tree structure with validation of allowed relations.
  - Update criteria assembly to traverse relation trees and register nested `SelectCriteria`.
  - Add locale-aware helper that resolves locale codes to joined tables when available.
  - Write unit tests covering multi-level includes, filtered relations, and invalid input handling.

## 3. Make Allowed Field Map Configurable

- [x] 3. Make Allowed Field Map Configurable

- **Why**
  CMS responses must expose columns that live outside the root struct (e.g., translation fields, JSONB attributes) and allow renaming. Hard-coded reflection in `getAllowedFields` limits exposed filters and select clauses.

- **What**
  Allow users to supply field metadata (map of request field → database column) per controller, extending or overriding the default reflection-derived map.

- **How**
  Introduce an optional `FieldMapProvider` option that merges with the reflective defaults. Ensure query builders consult the combined map for `select`, `order`, and filter parsing, and expose a helper for modules to register locale/JSONB fields.

- **Tasks**
  - Define option/config struct to accept custom field mappings and alias rules.
  - Adjust `getAllowedFields` to merge overrides at controller construction.
  - Update query parser to read from the merged map in all branches.
  - Add tests validating override behaviour and duplicate key handling.

## 4. Replace Console Debug Output with Structured Logger
- [x] Replace Console Debug Output with Structured Logger

- **Why**
  The CMS relies on consistent logging for diagnosis. `BuildQueryCriteria` currently prints debugging information via `fmt.Println`, which is noisy and uncontrollable in production environments.

- **What**
  Route diagnostic output through the existing `Logger` interface and provide log-level guards so consumers can opt in to query tracing.

- **How**
  Extend `Logger` with a `WithFields` or `Trace` helper (or reuse `Debug`) to capture query parameters and filters. Remove direct `fmt.Println` usage and respect the `LoggerEnabled` flag or contextual toggles.

- **Tasks**
  - Update `Logger` interface and default implementation to support trace/debug logging.
  - Replace `fmt.Println` statements in query builder with logger calls.
  - Add configuration flag or option to enable verbose query logging per controller.
  - Adjust tests (or add new ones) to ensure logging invocations do not affect output.

## 5. Add Route/Operation Toggles
- [x] 5. Add Route/Operation Toggles

- **Why**
  CMS services might restrict batch operations or require PATCH semantics. Consumers need to disable or customize default routes without forking the controller.

- **What**
  Provide configuration to enable/disable specific routes (e.g., batch create, delete) and optionally swap HTTP methods (PUT vs PATCH).

- **How**
  Introduce a `RouteConfig` struct passed during controller construction. Update `RegisterRoutes` to consult this config before registering each route, and allow method overrides through supplied route definitions.

- **Tasks**
  - Define route configuration options covering enable/disable and method overrides.
  - Modify `RegisterRoutes` to honour the configuration, including metadata application.
  - Ensure OpenAPI metadata generation reflects the active route set.
  - Add integration-style tests verifying route registration with various configs.

## 6. Provide CRUD Lifecycle Hooks
- [x] 6. Provide CRUD Lifecycle Hooks

- **Why**
  CMS tables require audit stamps (created_by/updated_by), publish window adjustments, and event dispatch around mutations. Hooks offer a clean seam without polluting controller logic.

- **What**
  Expose before/after lifecycle callbacks for create/update/delete (single and batch) that receive the payload and context.

- **How**
  Define hook signatures (e.g., `BeforeCreate(ctx Context, record T) error`) and integrate them into controller flow. Execute hooks prior to service/repository calls and post-hooks after successful operations, short-circuiting on errors.

- **Tasks**
  - Specify hook interface(s) and controller options to register them.
  - Wire hooks into CRUD handlers, ensuring batch operations iterate safely.
  - Document hook execution order and error handling strategy.
  - Add tests confirming hooks fire at expected stages and propagate errors.

---

**Next Steps**
- Align on sequencing (service delegation first to unblock CMS service wiring).
- Estimate effort per section and assign owners.
- Update project board with task breakdowns and dependencies.
