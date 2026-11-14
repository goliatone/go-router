# Opt-in Fiber v3

The additive adapter keeps `NewFiberAdapter`, `NewFiberAdapterWithConfig`, native
Fiber helpers and existing view helpers on Fiber v2. Choose `NewFiberV3Adapter`
explicitly to use Fiber v3. Both adapters share the current module and router
interfaces and can run concurrently. Go 1.26.0 is the minimum.

Version `v0.65.2` contains the corrected managed-hosting runtime, bounded ingress
and adapter-owned native middleware on both majors. Its downloaded artifact passed
the hosting and bounded-ingress runtime checks with `GOWORK=off` and zero module
replacements. Version `v0.65.1` retains the pipelined-drain defects described below.
The `v0.65.2` release omitted this guide; this documentation correction supplies
the missing support information. Final T15/T19 release qualification remains
pending verification of the published documentation correction. Runtime evidence
does not establish production v3 adoption, default cutover or feature acceptance.

```go
import (
    fiber3 "github.com/gofiber/fiber/v3"
    router "github.com/goliatone/go-router"
)

server := router.NewFiberV3AdapterWithConfig(router.FiberV3AdapterConfig{
    FiberAdapterConfig: router.FiberAdapterConfig{
        PathConflictMode: router.PathConflictModePreferStatic,
    },
    ListenConfig: fiber3.ListenConfig{
        ListenerNetwork: "tcp4",
        DisableStartupMessage: true,
    },
})
server.Router().Use(router.MiddlewareFromFiberV3(func(c fiber3.Ctx) error {
    c.Set("X-Backend", "fiber-v3")
    return c.Next()
}))
server.Router().Get("/health", func(c router.Context) error {
    return c.JSON(200, map[string]string{"status": "ok"})
})
err := server.Serve("127.0.0.1:8080")
```

`FiberV3AdapterConfig` embeds the native-free `FiberAdapterConfig` and adds typed
`ListenConfig`. `Serve` applies that listen configuration. `WrappedRouter()`
initializes/seals the route plan and returns `*fiber3.App`; callers using its
native `Listen`/`Listener` methods pass their own `fiber3.ListenConfig` there.
TLS, network and startup hooks are native listen options. Native TLS/prefork
capabilities have not been validated by the coexistence test fixtures.

| Fiber v2 entry point | Explicit v3 entry point |
| --- | --- |
| NewFiberAdapter / NewFiberAdapterWithConfig | NewFiberV3Adapter / NewFiberV3AdapterWithConfig |
| FiberAdapter / FiberRouter | FiberV3Adapter / FiberV3Router |
| NewFiberContext / MiddlewareFromFiber | NewFiberV3Context / MiddlewareFromFiberV3 |
| DefaultFiberOptions / DefaultFiberErrorHandler | DefaultFiberV3Options / DefaultFiberV3ErrorHandler |
| MergeLocalsWithViewData | MergeFiberV3LocalsWithViewData |
| FiberWebSocketHandler / NewFiberWebSocketContext | FiberV3WebSocketHandler / NewFiberV3WebSocketContext |
| FiberWebSocketFactory / NewFiberWebSocketFactory / RegisterFiberWebSocketFactory / GetFiberWebSocketFactory | FiberV3WebSocketFactory / NewFiberV3WebSocketFactory / RegisterFiberV3WebSocketFactory / GetFiberV3WebSocketFactory |
| DefaultViewEngine / InitializeViewEngine | DefaultFiberV3ViewEngine / InitializeFiberV3ViewEngine |

`FiberErrorHandlerConfig`, `DefaultFiberErrorHandlerConfig`, `RenderMergeStrategy`,
`ViewConfigProvider`, HTTP/render/download capabilities and WebSocket contracts
remain shared. The new native context is `fiber3.Ctx`, an interface. Its standard
context uses `Context`/`SetContext`; raw fasthttp access uses `RequestCtx`.
Binding uses manual `Bind().Body`/`Cookie` error handling. Router redirect helpers
explicitly retain 302, including named-route redirects; native v3 redirects
default to 303. No browser protocol or client API change is required.

Use `InitializeFiberV3ViewEngine` with native `fiber3.Config.Views`. Legacy helpers
retain Django v3/template core v1; new helpers retain the template/v2 core interface.
Filesystem overlays, assets, inheritance/layouts and locals precedence remain
available. Buffered rendering and response capture do not commit the live response.

The legacy helper still returns its original Django v3 engine. The v3 helper uses
Django syntax with isolated pongo2/v7 template sets and template/v2's configuration
interface. Escaping defaults to enabled and is scoped to each compiled set.
`SetAutoEscape` or reload publishes a new snapshot; active renders retain their
settings. Template callbacks can render the same or another engine, and unrelated
renders continue while a callback is blocked. Layout bindings do not modify caller
maps. No process-wide rendering lock or legacy escaping flag is involved.

Configure v3 helpers through the template core interface or
`interface{ SetAutoEscape(bool) }`. The renderer pins the upstream prerelease
revision `v7.0.0-alpha.1.0.20260313142248-c0f82578df57`, including its scoped escaping
and per-execution tag-state fixes; it does not use Django v4's shared-global render
path. Custom pongo2 v6 tag/filter registry state belongs to the legacy runtime and
does not configure the isolated v3 runtime. Register v7 filters and tags on the
returned engine through `router.FiberV3ViewExtensions`; each new set retains those
registrations, including after reload. Registration invalidates the next compiled
snapshot; active renders finish with their original set. Extension implementations
must support concurrent execution. For example:

```go
extensions := views.(router.FiberV3ViewExtensions)
err := extensions.RegisterFilter("prefix", func(in, param *pongo2.Value) (*pongo2.Value, error) {
    return pongo2.AsValue("prefix:" + in.String()), nil
}) // pongo2 is github.com/flosch/pongo2/v7
```

V6 `Value` objects and helper signatures are incompatible with this runtime and
fail with a migration error. Convert shared helpers to plain Go inputs/returns or
provide v7-specific helpers. A config may implement the optional
`router.FiberV3ViewFunctionsProvider` with `GetFiberV3TemplateFunctions() map[string]any`;
these overrides win over shared/generated helpers only for the v3 constructors.
The shared function map is unchanged, allowing one config to retain v6 helpers for
legacy constructors and supply equivalent v7 helpers. V7 `AsValue` remains escaped; `AsSafeValue` explicitly
marks trusted content. The renderer does not infer a v6 object's private safety
flag. Bindings and helper results are checked recursively, including values hidden
behind `any`; data containers are copied and helpers execute without engine locks.
Use acyclic view data (at most 64 nested levels) and `map[string]any` or `[]any`
for callable entries. Opaque model callable fields and methods with reachable
dynamic/foreign results require explicit map helpers so their results can be checked.
Validation follows returned fields and methods through pointers and collections;
recursive type graphs are supported when all reachable results are closed. Only
the second error result of a two-result method is treated as error handling. An
`error` returned as view data has an unknown concrete method set and is rejected.
Plain fields, scalar methods and closed concrete error types remain supported.
Types with dynamic mutator results can also require an explicit helper; for
example, pass a `time.Time` value or a scalar helper instead of an opaque
`*time.Time`, whose `Unmarshal*` methods return errors as their sole result. Package-level v7
registration configures its default set, not these private sets.

For a config shared by both backends, keep the existing v6 helper map and supply
v7 ports with the same template names. `SimpleViewConfig` provides fluent helpers:

```go
cfg := router.NewSimpleViewConfig("./views").
    WithFunctions(legacyHelpers).
    WithFiberV3Functions(v3Helpers).
    WithFiberV3Extensions(func(ext router.FiberV3ViewExtensions) error {
        return ext.RegisterFilter("tojson", func(in, _ *pongo2.Value) (*pongo2.Value, error) {
            data, err := json.Marshal(in.Interface())
            if err != nil {
                return nil, err
            }
            return pongo2.AsValue(string(data)), nil
        })
    }) // pongo2 is github.com/flosch/pongo2/v7; json is encoding/json
views, err := router.InitializeFiberV3ViewEngine(cfg)
```

`legacyHelpers` and `v3Helpers` use the same names and equivalent behavior.
For example, a helper returning v6 `AsValue`/`AsSafeValue` gets a v7 equivalent;
preserve that safe/unsafe distinction when porting. Existing v6 registrations and
helpers continue to serve v2. Templates such as `{{ payload|tojson }}` keep their
syntax. Port tag parser/node imports to v7, change `*pongo2.Error` returns to
`error`, and register the port using `ext.RegisterTag` under its existing name.
Block tags can continue using `WrapUntilTag` and `NewChildExecutionContext`.

Custom config types may implement `router.FiberV3ViewConfig` with
`ConfigureFiberV3Views(router.FiberV3ViewExtensions) error`; the v3 constructor
calls it once, outside render locks, and saved registrations survive reload.
Configuration errors propagate from the constructor. Neither optional v3
capability changes `ViewConfigProvider` or runs in the legacy constructors.

`rpcfiber.Mount(router, server, options...)` and
`ssefiber.Mount(router, options...)` accept either Fiber adapter and HTTPRouter.
The existing `MountFiber` wrappers retain their v2 types. SSE owns its framing,
resume cursor and heartbeat; closing a transport stream cancels its subscription.

Shutdown ownership is explicit. Stop application-owned WebSocket readers with
`WebSocketReadInterrupter.InterruptRead`, join readers/writers, and cancel owned
SSE contexts before bounded `server.Shutdown(ctx)`. Native shutdown alone does
not drain hijacked connections. A shutdown deadline returns while a held ordinary
handler may still be running; hosts must complete their own teardown. WebSocket
callbacks receive detached request metadata and standard context. HTTP-only
operations after upgrade fail safely.

Both upgrade paths retain the middleware request store and pre-upgrade hook
mutations. Detached `Locals` supports typed and string keys, writes and merges;
request-backed string/byte buffers are copied. Other reference values remain
application-owned and must outlive the HTTP request, including any native cleanup.
Header lookup remains case-insensitive after detachment. Internal native dispatch
bookkeeping is excluded from the snapshot.

Factory selection uses concrete native context identities. Custom factories may
implement `SupportsContext(router.Context) bool`; legacy supported-type patterns
still work, with longest-pattern and sorted-registration-key tie resolution.
Unknown middleware contexts retain the legacy HTTPRouter fallback.

Run the example:

```sh
go run ./examples/fiber-v3 -address 127.0.0.1:8080
curl http://127.0.0.1:8080/health
curl -N http://127.0.0.1:8080/events
```

The example serves `/echo` over WebSocket and coordinates reader/SSE cancellation
on SIGINT/SIGTERM. The nested WebSocket examples remain v2 compatibility consumers
of the published router versions in their own manifests; CI tests those graphs
with `GOWORK=off`, without checkout replacements. Candidate qualification uses
separate disposable modfiles with an explicit checkout replacement and records
the selected graph. The bounded-ingress integration fixture has its own checked-in
router replacement; its CI job does exercise the checkout.

Shared dependency resolution selects Fiber v3.6.1, contrib v3 WebSocket v1.2.9,
template v2.1.4, the scoped pongo2/v7 revision above, and fasthttp v1.76.0.
Django v4.0.12 remains a native compatibility pin. Legacy Fiber and engine versions
coexist. The intentional Gorilla security pseudo-version remains pinned. Rollback
must restore the prior compatible dependency set; changing only the router
constructor does not undo shared MVS-selected upgrades.


## Managed hosting in v0.65.2

Both native adapters expose the optional `ManagedServer` interface. Configure
routes, then call `StartManaged(parent, ManagedOptions{Address: address,
DrainTimeout: budget})`; no `ConfigureBoundedIngress` call is required for
hosting with legacy ingress. Existing constructors keep v2 as the default.
Serving readiness is distinct from application readiness. Use `Stop` and `Wait`
to collect shutdown and final cleanup results; do not mix managed ownership with
raw native serving or lifecycle calls on the same instance.

Managed mode supports a single-use plain TCP listener. TLS/autocert, Unix sockets,
prefork and restart remain unsupported. Applications own signals, dependency
readiness, authentication and SSE/WebSocket teardown. `OnShutdown` closes and joins
application-owned streams; HTTP drain alone does not close hijacked connections.
The cancellation example and the legacy-ingress protected consumer exercise
startup, real requests and cleanup on both majors. Version `v0.65.1` has managed
drain defects: an admitted response can be lost or truncated when another request
is pipelined during shutdown, including streams under legacy or bounded ingress.
Version `v0.65.2` fixes both handler-return and streamed-response drain loss.
Downloaded-artifact checks cover fixed-length and chunked bodies, normal
keepalive, admission closure, drain deadlines and cleanup on both majors under
legacy and bounded ingress. Supported v2 hosts can upgrade without migrating to
v3. Switching a production host to v3 retains its separate migration gates.

Managed hosting enables fasthttp's `ReduceMemoryUsage` before serving so every
complete response is flushed before the native idle/shutdown boundary. Keepalive
continues to work; pipelined response batching and buffer reuse are reduced, which
can increase CPU usage. Startup hooks cannot disable this setting. The unmanaged
native Serve/Test paths retain their configured behavior.

## Adapter-owned native middleware

Version `v0.65.2` adds the optional `FiberNativeMiddlewareRegistrar` (v2) and
`FiberV3NativeMiddlewareRegistrar` (v3) server capabilities. Use `UseNative` for
global native middleware before declaring any endpoint, static route or miss
handler. Use `UseNativeTerminal` to queue native middleware after those routes,
including deferred routes, and before sealing and startup validation. Neither
method selects raw hosting. Native handlers retain their actual Fiber context,
`Next` and `Route` behavior; terminal middleware is not a logical endpoint in
registration snapshots.

Register during single-owner construction, before `Initialize`, `WrappedRouter`
or any start attempt, including a rejected or pre-canceled start. Both methods
return errors: nil handlers reject the entire call; late calls wrap
`ErrRouterSealed`; early middleware after routes reports `ErrNativeMiddlewareOrder`.
Handler slices are copied, but captured state remains application-owned and must
support concurrent requests. Registration hooks must not reenter registration or
startup. Ownership violations and hook panics become sticky startup failures;
these APIs do not authorize preexisting raw native routes or mutation after
finalization. See the [API contracts](../fiber_native_middleware.go) for both majors.

## Bounded ingress candidate

Bounded ingress was introduced in `v0.65.1`; use `v0.65.2` for the corrected
managed-drain behavior. The maintained candidate fixture continues to test the
working checkout, separately from downloaded-artifact verification.
Existing constructors still select v2 unless an explicit
v3 constructor is used. The router minimum stays in [go.mod](../go.mod); the
protected consumer's published providers have a higher minimum in its
[own manifest](../integration/bounded-ingress/go.mod).

```go
server := router.NewFiberV3Adapter() // NewFiberAdapter retains v2.
ingress := server.(router.BoundedIngressServer)
err := ingress.ConfigureBoundedIngress(router.BoundedIngressOptions{
    UnknownMethod: router.UnknownMethodOptions{Policy: router.UnknownMethodReject},
})
if err != nil { return err }
server.Router().Get("/healthz", healthHandler)
// Initialization finalizes registrations and installs ingress. Configure alone
// neither installs policy nor proves serving readiness.
if err := server.(router.StartupInitializer).Initialize(); err != nil { return err }
run, err := server.(router.ManagedServer).StartManaged(parent, router.ManagedOptions{
    Address: "127.0.0.1:8080", DrainTimeout: 5 * time.Second,
})
if err != nil { return err }
select {
case <-run.Serving(): // Host dependencies may still need their own readiness.
case <-run.Done(): return run.Wait(context.Background())
}
return run.Wait(context.Background())
```

Configuration accepts one copied policy before initialization/start begins. A
failed initial configuration blocks startup until corrected beforehand; successful
configuration cannot be replaced. Initialization failures are sticky. Preflight
`ValidateBoundedIngress` neither enables ingress nor closes route setup. Raw native
`Test`/`Listener` use requires successful adapter initialization; `WrappedRouter`
performs it but selects raw ownership and cannot be mixed with managed hosting.

| Policy | Globally unknown valid method | Known methods, including native misses |
| --- | --- | --- |
| Native (opt-in default) | Delegate once to Fiber, preserving native status/body | Delegate once |
| Reject | No dispatch; default 501 `HTTP_METHOD_NOT_IMPLEMENTED` | Delegate once |
| Custom | Application interface/function returns DelegateNative or a validated rejection | Delegate once; strategy is not called |

Unknown means absent from the finalized native `RequestMethods`, with exact token
case preserved. It does not mean that a known method lacks an endpoint at a path.
Bounded ingress provides no authoritative path existence, exact Allow, synthesized
405 or generic automatic OPTIONS. Full RouteAware ingress remains blocked by B01.
No new RouteAware selector is exposed. Valid authority-form CONNECT is separately
rejected with safe 501/no tunnel; malformed targets reaching ingress receive 400.
Parser/transport failures before ingress have no router correlation guarantee.

`UnknownMethodHandler` and `UnknownMethodHandlerFunc` receive standard cancellation
and owned method/target-form/request-ID facts, and return a decision. They cannot
write a response or dispatch the native handler. See `ApplicationStrategy` in the
[runnable example](../examples/managed-ingress/main.go). Configured/dynamic rejection
statuses are 400–599 except 401, 405, 407 and 426. Messages are bounded to 200 Unicode
characters/800 bytes, codes to 64 ASCII characters, and envelopes to 2,048 bytes.
Invalid decisions/errors/panics fail closed with fixed safe 500; delegated RPC and
application responses retain their own serialization and error semantics.

Fresh server request IDs replace all client `X-Request-ID` casing variants; response
and context identity remain consistent, including disabled native normalization.
Unrelated header fields keep native behavior. IDs grant no identity or permission.
Context replacement preserves application values, cancellation and deadlines while
retaining the managed lifetime bound. Safe observers and privileged diagnostics
receive only the owned cancellation view; raw causes belong only in explicitly
configured diagnostics. Callback state must be concurrency-safe and cooperative.

| Native capability | v2 and v3 runtime support in v0.65.2 |
| --- | --- |
| Bounded ingress with Test, native Listener after initialization | Qualified on the published replacement-free graph |
| Legacy/bounded ingress with managed plain TCP | Corrected pipelined/streamed drain qualified on the published replacement-free graph |
| Owned native middleware and terminal registration | Both-major published runtime qualified; use the optional registrar above |
| Native/Reject/Custom, ordinary streams and adapter WebSocket handoff | Qualified; host owns stream/hijacked-resource teardown |
| Effective GETOnly | Rejected before activation/pre-accept |
| Prefork, custom handler/TimeoutHandler, unmanaged native mutation | Unsupported for bounded ingress |
| v3 custom context types | Rejected during setup; default context supported |
| Managed TLS/autocert, Unix sockets, restart | Unsupported; managed instances are single-use |
| Full route-aware resolver/Allow/automatic OPTIONS | Pending B01; no release claim |

Parent cancellation starts drain and closes admission. Admitted contexts remain
alive until completion or drain expiry. `Stop` bounds its caller's wait; `Wait`
reports eventual completion. A blocked application callback can delay completion,
and HTTP shutdown does not close hijacked connections. Terminal errors can join
parent cancellation with drain, startup or teardown failures. A match from
`errors.Is(err, context.Canceled)` alone does not establish clean shutdown. The
example collects cleanup results on every exit and suppresses only bare parent
cancellation leaves in join containers; phase-wrapped or independent failures
remain inspectable. Use `OnShutdown` to close
and join host-owned SSE/WebSocket work before shared resources are released.

Run a real startup/request/cancellation smoke without sending process signals:

```sh
go run ./examples/managed-ingress -backend=v3 -policy=custom -address=127.0.0.1:0 -smoke
go run ./examples/managed-ingress -backend=v2 -policy=reject -address=127.0.0.1:0 -smoke
```

The qualified protected consumer uses `quickstart.NewAdmin` with the native router adapter
and real `rpcfiber` mounting. Quickstart's `NewFiberServer`/`NewFiberV3Server`
wrappers do not forward the new optional interfaces; those wrappers are not the
qualified assembly for these capabilities. No quickstart production patch or
actual host migration is implied by the fixture. Quickstart v0.145.1 also does not
forward RPC `ErrorObserver`. The downloaded-artifact checks separately qualify a host-owned
`rpcfiber.Mount` using the same admin runtime, auth wrapper and command policy,
with explicit exposure evidence and startup validation. This is the supported
assembly for RPC diagnostics/fallback qualification; it requires no provider patch.
See the [consumer fixture](../integration/bounded-ingress/README.md) for both paths.

V2 support remains owned by goliatone through the existing authorized
cutover/retirement milestone; this adds no indefinite parity/backport commitment.
The `v0.65.2` checks use Fiber v2.52.13/v3.6.1 and fasthttp v1.76.0. The protected
consumer selects admin/quickstart v0.145.1 and go-command v0.27.1 on Go 1.26.5;
router and native example checks retain Go 1.26.0. All qualification graphs have
zero replacements. The published runtime evidence can be reused for a
documentation-only correction after verifying the new artifact's source and
dependency identities; a version bump alone is not evidence of equivalence.
The missing-guide publication check remains open. B01, migration T27, adoption,
default cutover, retirement and explicit acceptance remain unchanged.
