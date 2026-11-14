# Protected bounded ingress candidate

Legacy-ingress managed socket controls preserve host-owned correlation and
protected RPC success/denial without enabling bounded policy.

This isolated module composes published admin/quickstart and go-command providers
with the working router candidate. Its only replacement is `go-router => ../..`.
The complete selected versions and checksums are in `go.mod`/`go.sum`; no dependency
on go-admin enters the router core. It is a maintained candidate fixture, not a
published application or evidence of production migration.

The fixture uses real `quickstart.NewAdmin`, admin strict RPC startup/registration,
typed command dispatch, RPC decoding/error mapping, and native v2/v3 adapters.
Identity, permissions and domain services are deterministic fakes. Native adapters
own hosting because quickstart's server wrappers do not forward the new optional
capabilities. Both `app.Test` and managed sockets run Native/Reject/Custom cases.

Coverage includes typed query/result and typed mutation/result, exact permissions,
a registered but unallowlisted command, unauthenticated/forbidden denial without
service calls, disabled HTTP and RPC discovery, forged metadata versus trusted context,
correlation, concurrent query/dispatch isolation, domain-error/panic and hook
containment, and no dispatch from rejected unknown methods. Adversarial dispatch
requests use the published `CommandDispatchRequest.Options` wire model. Tests
verify that the forged fields decode, then check sanitized identity, scope,
permissions and correlation in both the policy hook and actual command execution. HTTP 200 with an RPC error is a failed call. A dispatch receipt is not
used as evidence that the fake domain operation succeeded; its typed result and
exact invocation count are checked separately.

The diagnostic matrix additionally uses a **host-owned RPC mount** on the same
published admin runtime. Quickstart v0.145.1 does not forward `ErrorObserver`.
`rpc_diagnostics_test.go` therefore uses public `rpcfiber.Mount`, `AuthWrapper`,
`ConfigureRPCExposure` and `RegisterStartupValidator` APIs, with the same exact
command policy and disabled discovery. It independently checks success and denial,
then exercises mapper panic, invalid/unserializable/panicking projections, RPC
observer panic, and before/after hook failures under every bounded mode and
transport. It verifies original diagnostic errors, trusted callback context,
nonrecursive fallback, one invocation/response and concurrent isolation. Privileged
RPC diagnostic records remain separate from sanitized ingress events. No provider
patch or new provider release is required; this does not imply quickstart's
transport configuration exposes an observer option.

From this directory, using the minimum declared in this module:

```sh
GOWORK=off go test -mod=readonly -race -count=1 -timeout=120s ./...
GOWORK=off go mod verify
```

The CI nested-module matrix includes this module at its own provider minimum;
existing router/example minimum-toolchain jobs are unchanged. Runtime RPC and
bounded-ingress package checks must also be rerun on this selected graph for a
release candidate. Exact commands and source locks are recorded in the canonical
[feature context](../../.ctx/specs/fiber-hosting-ingress/DESIGN.md).

Before published qualification, select the authorized router release, remove the
replacement, and repeat the candidate/runtime assertions with `GOWORK=off`. Do not
remove the replacement here or label this graph published merely to prepare a
release. Full route-aware ingress still requires B01; T19 remains a separate task.
