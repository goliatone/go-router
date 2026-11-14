# WebSocket Lifecycle Hooks Contract

Use one package-managed upgrade entry point per route. Current implementations
support both `WebSocketUpgrade(config)` middleware and adapter-specific handlers;
context factories construct contexts and do not independently run connect hooks.

## Hook ownership

- `OnPreUpgrade` runs before the protocol upgrade, while an HTTP rejection can
  still carry status, headers and a body. Rejected handshakes do not run
  `OnConnect`. Preserve typed/prepared rejection responses through the package
  handler rather than overwriting them with an unconditional 500.
- `OnConnect` runs after a successful upgrade, once for the chosen entry path.
  Middleware owns it for middleware upgrades; `FiberWebSocketHandler` and
  `HTTPRouterWebSocketHandler` own it for their direct-handler paths.
- Adapter `ReadMessage` invokes the configured message callbacks after a
  successful data read. Callbacks are not a second independent receive loop.
- Adapter `Close` owns disconnect notification and guards repeated closure.
  Keep cleanup idempotent when application shutdown can also trigger it.

`WebSocketConfig.OnError` is not a universal exactly-once error-delivery hook in
the current adapter paths. Handle returned read/write/handler errors explicitly;
the high-level hub has its own `OnError(WSErrorHandler)` registration. Do not
infer one callback contract from the other API.

## Integration rules

Do not compose a direct handler and upgrade middleware that both own the same
connection's upgrade/hooks. For a high-level session, use `NewWSHub`, `WSClient`,
and `NewWSHandler` with their matching handler/middleware types. For low-level
read/write access, use `WebSocketContext`; the APIs are not interchangeable.

Configure `OnPreUpgrade` for HTTP admission decisions. A failure after upgrade
must use WebSocket error/close handling; it cannot change the HTTP status that
has already been sent. Await required host route registration before router
initialization seals the route plan.

## Verification and source

```sh
go test -mod=readonly . -run 'TestOnConnect|TestWebSocketPreUpgradeResponses|TestRegressionBug2'
```

- [Upgrade middleware](../websocket_middleware.go)
- [Fiber adapter and direct handler](../fiber_websocket.go)
- [HTTPRouter adapter and direct handler](../httprouter_websocket.go)
- [Connect regression tests](../websocket_onconnect_test.go)
- [Handshake regression tests](../websocket_preupgrade_test.go)
- [API guide](WEBSOCKET_API.md)

Historical critical-bug regressions remain in
[websocket_critical_bugs_regression_test.go](../websocket_critical_bugs_regression_test.go).
