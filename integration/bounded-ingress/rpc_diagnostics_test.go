package ingresscandidate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/goliatone/go-admin/admin"
	"github.com/goliatone/go-admin/quickstart"
	auth "github.com/goliatone/go-auth"
	rpc "github.com/goliatone/go-command/rpc"
	router "github.com/goliatone/go-router"
	"github.com/goliatone/go-router/rpcfiber"
	"github.com/stretchr/testify/require"
)

type faultKey struct{}
type rpcDiagnostic struct {
	Event                                  rpcfiber.FailureEvent
	Actor, Tenant, Organization, RequestID string
}
type panicProjection struct{}

func (panicProjection) MarshalJSON() ([]byte, error) { panic(secret + "-projection") }

// Quickstart v0.145.1 does not forward ErrorObserver. This supported host-owned
// assembly uses the same admin server, policy and auth, with public mount and
// finalized-exposure APIs. It neither patches providers nor replaces callbacks
// on an already-mounted route.
func mountProtectedRPC[T any](t *testing.T, r router.Router[T], adm *admin.Admin, f *candidate, cfg quickstart.RPCTransportConfig) {
	t.Helper()
	path := adm.AdminAPIBasePath() + "/rpc"
	snapshotter, ok := any(r).(interface {
		RegistrationSnapshot() router.RegistrationSnapshot
	})
	require.True(t, ok)
	adm.ConfigureRPCExposure(admin.RPCExposureConfig{Enabled: true, RequireAuth: true, InvokePath: path, Snapshot: func() (router.RegistrationSnapshot, bool) { return snapshotter.RegistrationSnapshot(), true }})
	wrapper := adm.AuthWrapper()
	require.NotNil(t, wrapper)
	err := rpcfiber.Mount(r, adm.RPCServer(),
		rpcfiber.WithInvokePath(path), rpcfiber.WithDiscoveryEnabled(false),
		rpcfiber.WithInvokeMiddlewares(router.MiddlewareFunc(wrapper)),
		rpcfiber.WithMetaMergePolicy(rpcfiber.MetaMergePolicyTransportOverrides),
		rpcfiber.WithMetaExtractor(cfg.MetaExtractor),
		rpcfiber.WithBeforeInvokeHook(func(c router.Context, method string, payload any) error {
			fault := c.Header("X-Candidate-Fault")
			c.SetContext(context.WithValue(c.Context(), faultKey{}, fault))
			if fault == "before-panic" {
				panic(secret + "-before")
			}
			return cfg.BeforeInvoke(c, method, payload)
		}),
		rpcfiber.WithAfterInvokeHook(func(c router.Context, method string, result any, err error) {
			cfg.AfterInvoke(c, method, result, err)
			if c.Context().Value(faultKey{}) == "after-error-panic" {
				panic(secret + "-after-error")
			}
		}),
		rpcfiber.WithErrorMapper(func(ctx context.Context, err error) *rpc.Error {
			id := requestID(ctx)
			f.mu.Lock()
			f.mapperCalls[id]++
			f.mu.Unlock()
			actor, ok := auth.ActorFromContext(ctx)
			if !ok || actor == nil || actor.ActorID != "actor-"+fmt.Sprint(ctx.Value(permissionKey{})) || actor.TenantID != "tenant-owned" || actor.OrganizationID != "org-owned" || id == "" {
				return &rpc.Error{Code: "LOST_CONTEXT", Message: "missing trusted context"}
			}
			switch ctx.Value(faultKey{}) {
			case "mapper-panic", "mapper-observer-panic":
				panic(secret + "-mapper")
			case "invalid-projection":
				return &rpc.Error{}
			case "unserializable":
				return &rpc.Error{Code: "PUBLIC", Message: "safe", Details: map[string]any{"bad": func() {}}}
			case "projection-panic":
				return &rpc.Error{Code: "PUBLIC", Message: "safe", Details: map[string]any{"bad": panicProjection{}}}
			}
			return cfg.ErrorMapper(ctx, err)
		}),
		rpcfiber.WithErrorObserver(func(c router.Context, event rpcfiber.FailureEvent) {
			actor, _ := auth.ActorFromContext(c.Context())
			actorID, tenant, organization := "", "", ""
			if actor != nil {
				actorID, tenant, organization = actor.ActorID, actor.TenantID, actor.OrganizationID
			}
			id := requestID(c.Context())
			f.mu.Lock()
			f.rpcEvents[id] = append(f.rpcEvents[id], rpcDiagnostic{event, actorID, tenant, organization, id})
			f.mu.Unlock()
			// Record once, then fail: the transport must not recursively report it.
			fault, _ := c.Context().Value(faultKey{}).(string)
			if fault == "observer-panic" || fault == "mapper-observer-panic" {
				panic(secret + "-observer")
			}
		}),
	)
	require.NoError(t, err)
	registrar, ok := any(r).(router.StartupValidatorRegistrar)
	require.True(t, ok)
	require.NoError(t, registrar.RegisterStartupValidator("candidate.rpc:"+path, adm.ValidateRPCExposure))
}

func TestProtectedRPCDiagnostics(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, transport := range []string{"test", "socket"} {
			for _, mode := range []string{"native", "reject", "custom"} {
				t.Run(major+"/"+transport+"/"+mode, func(t *testing.T) {
					f := setupCandidate(t, major, transport, mode, true)
					// Establish this assembly's success and protection independently.
					for _, mutation := range []bool{false, true} {
						method := "candidate.query"
						if mutation {
							method = "candidate.change"
						}
						res, b, _ := f.call(t, "POST", f.path, "writer", payload(method, "ok", mutation))
						require.Equal(t, 200, res.StatusCode)
						var envelope rpc.ResponseEnvelope[json.RawMessage]
						require.NoError(t, json.Unmarshal(b, &envelope))
						require.Nil(t, envelope.Error)
						data := envelope.Data
						if mutation {
							var dispatch struct {
								Result json.RawMessage `json:"result"`
							}
							require.NoError(t, json.Unmarshal(data, &dispatch))
							data = dispatch.Result
						}
						var out output
						require.NoError(t, json.Unmarshal(data, &out))
						require.Equal(t, output{"ok", "actor-writer", "tenant-owned", "org-owned", res.Header.Get("X-Request-ID")}, out)
					}
					require.EqualValues(t, 1, f.service.query.Load())
					require.EqualValues(t, 1, f.service.mutation.Load())
					for _, tc := range []struct {
						token, method string
						mutation      bool
						code          string
					}{
						{"", "candidate.change", true, ""}, {"reader", "candidate.change", true, rpc.CodeForbidden}, {"forbidden", "candidate.query", false, rpc.CodeForbidden}, {"writer", "candidate.hidden", true, rpc.CodeNotFound}, {"writer", admin.RPCMethodCommandList, false, rpc.CodeNotFound},
					} {
						res, b, _ := f.call(t, "POST", f.path, tc.token, payload(tc.method, "never", tc.mutation))
						if tc.token == "" {
							require.Equal(t, 401, res.StatusCode)
						} else {
							var out rpc.ResponseEnvelope[any]
							require.NoError(t, json.Unmarshal(b, &out))
							require.NotNil(t, out.Error)
							require.Equal(t, tc.code, out.Error.Code)
						}
					}
					res, _, _ := f.call(t, "GET", f.path+"/endpoints", "writer", "")
					require.Equal(t, 404, res.StatusCode)
					require.EqualValues(t, 1, f.service.query.Load())
					require.EqualValues(t, 1, f.service.mutation.Load())
					require.EqualValues(t, 1, f.policy.Load())
					require.Zero(t, f.service.hidden.Load())
					for _, method := range []string{"X-CANDIDATE-UNKNOWN", "X-DELEGATE"} {
						_, _, event := f.call(t, method, f.path, "writer", payload("candidate.change", "never", true), "mapper-observer-panic")
						want := 0
						if mode == "native" || mode == "custom" && method == "X-DELEGATE" {
							want = 1
						}
						require.EqualValues(t, want, event.Dispatches)
						f.mu.Lock()
						events, mapped := len(f.rpcEvents[event.RequestID]), f.mapperCalls[event.RequestID]
						f.mu.Unlock()
						require.Zero(t, events)
						require.Zero(t, mapped)
					}
					require.EqualValues(t, 1, f.service.mutation.Load())
					for _, fault := range []string{"domain", "observer-panic", "mapper-panic", "mapper-observer-panic", "invalid-projection", "unserializable", "projection-panic", "before-panic", "after-panic", "after-error-panic"} {
						t.Run(fault, func(t *testing.T) { assertRPCFault(t, f, fault) })
					}
					t.Run("concurrent-diagnostics", func(t *testing.T) {
						before := f.service.query.Load()
						var wg sync.WaitGroup
						for n := 0; n < 8; n++ {
							wg.Add(1)
							go func() { defer wg.Done(); assertRPCFaultResponse(t, f, "mapper-observer-panic") }()
						}
						wg.Wait()
						require.EqualValues(t, before+8, f.service.query.Load(), "concurrent failures never replay")
					})
				})
			}
		}
	}
}

func assertRPCFault(t *testing.T, f *candidate, fault string) {
	t.Helper()
	beforeQuery, beforeMutation, beforeAfter := f.service.query.Load(), f.service.mutation.Load(), f.after.Load()
	assertRPCFaultResponse(t, f, fault)
	invokes := int32(1)
	if fault == "before-panic" {
		invokes = 0
	}
	require.Equal(t, beforeQuery+invokes, f.service.query.Load(), "no replay")
	require.Equal(t, beforeMutation, f.service.mutation.Load())
	require.Equal(t, beforeAfter+invokes, f.after.Load(), "one after-invoke per invocation")
}

func assertRPCFaultResponse(t *testing.T, f *candidate, fault string) {
	t.Helper()
	value := "error"
	if fault == "after-panic" {
		value = "after-panic"
	}
	res, b, ingress := f.call(t, "POST", f.path, "writer", payload("candidate.query", value, false), fault)
	wantStatus := 200
	if fault == "before-panic" {
		wantStatus = 400
	}
	require.Equal(t, wantStatus, res.StatusCode)
	require.EqualValues(t, 1, ingress.Dispatches)
	var out rpc.ResponseEnvelope[output]
	require.NoError(t, json.Unmarshal(b, &out), "exactly one complete response, no appended fallback")
	if fault == "after-panic" {
		require.Nil(t, out.Error)
		require.Equal(t, "after-panic", out.Data.Value)
	} else {
		require.NotNil(t, out.Error)
		require.NotEmpty(t, out.Error.Code)
		require.NotEmpty(t, out.Error.Message)
	}
	id := res.Header.Get("X-Request-ID")
	f.mu.Lock()
	events := append([]rpcDiagnostic(nil), f.rpcEvents[id]...)
	mapped := f.mapperCalls[id]
	f.mu.Unlock()
	stages := []rpcfiber.FailureStage{rpcfiber.FailureStageInvoke}
	wantMapped := 1
	switch fault {
	case "mapper-panic", "mapper-observer-panic", "invalid-projection", "unserializable", "projection-panic":
		stages = append(stages, rpcfiber.FailureStageMapping)
	case "before-panic":
		stages = []rpcfiber.FailureStage{rpcfiber.FailureStageBeforeInvoke}
		wantMapped = 0
	case "after-error-panic":
		stages = []rpcfiber.FailureStage{rpcfiber.FailureStageAfterInvoke, rpcfiber.FailureStageInvoke}
	case "after-panic":
		stages = []rpcfiber.FailureStage{rpcfiber.FailureStageAfterInvoke}
		wantMapped = 0
	}
	require.Len(t, events, len(stages), "one observation per failed stage, no recursion")
	require.Equal(t, wantMapped, mapped, "no mapper retry")
	for n, event := range events {
		require.Equal(t, stages[n], event.Event.Stage)
		require.Equal(t, "actor-writer", event.Actor)
		require.Equal(t, "tenant-owned", event.Tenant)
		require.Equal(t, "org-owned", event.Organization)
		require.Equal(t, id, event.RequestID)
		require.Error(t, event.Event.Err)
	}
	// Original private details stay available only in the privileged record.
	original := events[0].Event
	if fault == "after-error-panic" {
		require.Contains(t, fmt.Sprint(original.Panic), secret)
		original = events[1].Event
	}
	if fault == "before-panic" || fault == "after-panic" {
		require.Contains(t, fmt.Sprint(original.Panic), secret)
	} else {
		require.ErrorIs(t, original.Err, domainFailure)
		require.Equal(t, secret, domainFailure.Message)
		require.Equal(t, secret, domainFailure.Metadata["secret"])
	}
	safe, err := json.Marshal(ingress)
	require.NoError(t, err)
	require.NotContains(t, string(safe), secret)
	if fault != "after-panic" {
		code := rpc.CodeConflict
		if fault == "before-panic" {
			code = rpc.CodeInternal
		}
		require.Equal(t, code, out.Error.Code)
	}
	if fault == "mapper-panic" || fault == "mapper-observer-panic" || fault == "projection-panic" {
		require.Contains(t, fmt.Sprint(events[1].Event.Panic), secret)
	}
	// Ensure diagnostic errors remain inspectable, rather than flattened away.
	require.False(t, errors.Is(original.Err, context.Canceled))
}
