package rpcfiber_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	cmdrpc "github.com/goliatone/go-command/rpc"
	gerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/rpcfiber"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The outer test guard exposes escaped panics as failures instead of allowing
// Fiber's request goroutine to terminate the test process.
func mountErrorFixture(t *testing.T, backend string, srv rpcfiber.MethodServer, opts ...rpcfiber.Option) func(string) *http.Response {
	t.Helper()
	guard := func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) (err error) {
			defer func() {
				if recover() != nil {
					err = c.JSON(http.StatusInternalServerError, map[string]any{"escaped_panic": true})
				}
			}()
			return next(c)
		}
	}
	if backend == "v2" {
		a := router.NewFiberAdapter()
		r := a.Router()
		r.Use(guard)
		require.NoError(t, rpcfiber.MountFiber(r, srv, opts...))
		return func(body string) *http.Response {
			return testRequest(t, a.WrappedRouter(), http.MethodPost, "/api/rpc", body, nil)
		}
	}
	a := router.NewFiberV3Adapter()
	r := a.Router()
	r.Use(guard)
	require.NoError(t, rpcfiber.Mount(r, srv, opts...))
	return func(body string) *http.Response {
		return testRequestV3(t, a.WrappedRouter(), http.MethodPost, "/api/rpc", body, nil)
	}
}

func errorServer(t *testing.T, run func(context.Context) error, opts ...cmdrpc.Option) *cmdrpc.Server {
	t.Helper()
	s := cmdrpc.NewServer(opts...)
	require.NoError(t, s.RegisterEndpoint(cmdrpc.NewEndpoint[echoData, map[string]any](cmdrpc.EndpointSpec{Method: "example.fail", Kind: cmdrpc.MethodKindCommand},
		func(ctx context.Context, _ cmdrpc.RequestEnvelope[echoData]) (cmdrpc.ResponseEnvelope[map[string]any], error) {
			return cmdrpc.ResponseEnvelope[map[string]any]{Data: map[string]any{"receipt": map[string]any{"Accepted": true}, "result": map[string]any{"failed": true}}}, run(ctx)
		})))
	return s
}

const failureBody = `{"method":"example.fail","params":{"data":{"name":"Ada"}}}`

func TestMountErrorRedaction(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, tc := range []struct {
			name, body, code string
			status           int
			before           bool
		}{
			{"parse", `{"method":`, "RPC_INVALID_REQUEST", 400, false},
			{"method", `{}`, "RPC_METHOD_REQUIRED", 400, false},
			{"resolve", `{"method":"missing"}`, "RPC_METHOD_NOT_FOUND", 404, false},
			{"decode", `{"method":"example.fail","params":{"data":"wrong"}}`, "RPC_INVALID_PARAMS", 400, false},
			{"before", failureBody, "RPC_PREINVOKE_REJECTED", 400, true},
			{"invoke", failureBody, "RPC_INVOKE_FAILED", 200, false},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				sentinel := errors.New("private-secret-sentinel")
				after := 0
				options := []rpcfiber.Option{rpcfiber.WithAfterInvokeHook(func(_ router.Context, _ string, _ any, err error) { after++; require.ErrorIs(t, err, sentinel) })}
				if tc.before {
					options = append(options, rpcfiber.WithBeforeInvokeHook(func(router.Context, string, any) error { return sentinel }))
				}
				call := mountErrorFixture(t, backend, errorServer(t, func(context.Context) error { return sentinel }), options...)
				resp := call(tc.body)
				require.Equal(t, tc.status, resp.StatusCode)
				var out cmdrpc.ResponseEnvelope[any]
				decodeResponse(t, resp, &out)
				require.NotNil(t, out.Error)
				assert.Equal(t, tc.code, out.Error.Code)
				assert.NotContains(t, out.Error.Details, "cause")
				assert.NotContains(t, out.Error.Message, "private-secret-sentinel")
				if tc.name == "invoke" {
					assert.Equal(t, 1, after)
				} else {
					assert.Zero(t, after, "existing AfterInvoke cannot observe pre-invocation failures")
				}
			})
		}
	}
}

func TestMountInvocationPanicBoundary(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		t.Run(backend, func(t *testing.T) {
			after := 0
			srv := errorServer(t, func(context.Context) error { panic("private-panic-sentinel") })
			call := mountErrorFixture(t, backend, srv, rpcfiber.WithAfterInvokeHook(func(_ router.Context, _ string, _ any, err error) { after++; require.Error(t, err) }))
			resp := call(failureBody)
			require.Equal(t, 200, resp.StatusCode)
			var out cmdrpc.ResponseEnvelope[any]
			decodeResponse(t, resp, &out)
			require.NotNil(t, out.Error)
			assert.Equal(t, "RPC_INVOKE_FAILED", out.Error.Code)
			assert.False(t, strings.Contains(out.Error.Message, "private-panic-sentinel"))
			assert.Equal(t, 1, after)
		})
	}
}

func TestMountStructuredErrors(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, tc := range []struct {
			name string
			err  error
			code string
		}{
			{"validation", gerrors.New("private-value", gerrors.CategoryValidation), cmdrpc.CodeInvalidArgument},
			{"forbidden", gerrors.New("private-policy", gerrors.CategoryAuthz), cmdrpc.CodeForbidden},
			{"conflict", fmt.Errorf("private-wrapper: %w", gerrors.New("private-key", gerrors.CategoryConflict)), cmdrpc.CodeConflict},
			{"unavailable", gerrors.New("private-upstream", gerrors.CategoryExternal), cmdrpc.CodeUnavailable},
			{"canceled", fmt.Errorf("private-cancel: %w", context.Canceled), cmdrpc.CodeCanceled},
			{"deadline", fmt.Errorf("private-deadline: %w", context.DeadlineExceeded), cmdrpc.CodeDeadlineExceeded},
			{"unknown", errors.New("private-secret"), cmdrpc.CodeInternal},
		} {
			for _, before := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/before=%v", backend, tc.name, before), func(t *testing.T) {
					calls, after := 0, 0
					var events []rpcfiber.FailureEvent
					opts := []rpcfiber.Option{
						rpcfiber.WithErrorMapper(cmdrpc.DefaultErrorMapper),
						rpcfiber.WithErrorObserver(func(_ router.Context, e rpcfiber.FailureEvent) { events = append(events, e) }),
						rpcfiber.WithAfterInvokeHook(func(_ router.Context, _ string, _ any, err error) { after++; assert.ErrorIs(t, err, tc.err) }),
					}
					if before {
						opts = append(opts, rpcfiber.WithBeforeInvokeHook(func(router.Context, string, any) error { return tc.err }))
					}
					call := mountErrorFixture(t, backend, errorServer(t, func(context.Context) error { calls++; return tc.err }), opts...)
					resp := call(failureBody)
					wantStatus := 200
					wantStage := rpcfiber.FailureStageInvoke
					if before {
						wantStatus = 400
						wantStage = rpcfiber.FailureStageBeforeInvoke
					}
					require.Equal(t, wantStatus, resp.StatusCode)
					var out cmdrpc.ResponseEnvelope[any]
					decodeResponse(t, resp, &out)
					require.NotNil(t, out.Error)
					expected := cmdrpc.DefaultErrorMapper(context.Background(), tc.err)
					assert.Equal(t, expected, out.Error)
					assert.Equal(t, tc.code, out.Error.Code)
					require.Len(t, events, 1)
					assert.Equal(t, wantStage, events[0].Stage)
					assert.Same(t, tc.err, events[0].Err)
					if before {
						assert.Zero(t, calls)
						assert.Zero(t, after)
					} else {
						assert.Equal(t, 1, calls)
						assert.Equal(t, 1, after)
					}
				})
			}
		}
	}
}

func TestMountStructuredTransportStages(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, tc := range []struct {
			body, code string
			status     int
			stage      rpcfiber.FailureStage
		}{
			{`{`, "RPC_INVALID_REQUEST", 400, rpcfiber.FailureStageParse},
			{`{}`, "RPC_METHOD_REQUIRED", 400, rpcfiber.FailureStageResolve},
			{`{"method":"absent"}`, "RPC_METHOD_NOT_FOUND", 404, rpcfiber.FailureStageResolve},
			{`{"method":"example.fail","params":{"data":true}}`, "RPC_INVALID_PARAMS", 400, rpcfiber.FailureStageDecode},
		} {
			t.Run(backend+"/"+tc.code, func(t *testing.T) {
				mapped, after := 0, 0
				var events []rpcfiber.FailureEvent
				call := mountErrorFixture(t, backend, errorServer(t, func(context.Context) error { t.Error("must not invoke"); return nil }),
					rpcfiber.WithErrorMapper(func(context.Context, error) *cmdrpc.Error {
						mapped++
						return &cmdrpc.Error{Code: "OVERRIDE", Message: "bad override"}
					}),
					rpcfiber.WithErrorObserver(func(_ router.Context, e rpcfiber.FailureEvent) { events = append(events, e) }),
					rpcfiber.WithAfterInvokeHook(func(router.Context, string, any, error) { after++ }))
				resp := call(tc.body)
				require.Equal(t, tc.status, resp.StatusCode)
				var out cmdrpc.ResponseEnvelope[any]
				decodeResponse(t, resp, &out)
				require.NotNil(t, out.Error)
				assert.Equal(t, tc.code, out.Error.Code)
				assert.NotContains(t, out.Error.Details, "cause")
				assert.Zero(t, mapped)
				assert.Zero(t, after)
				require.Len(t, events, 1)
				assert.Equal(t, tc.stage, events[0].Stage)
				assert.Error(t, events[0].Err)
			})
		}
	}
}

type failingPublicJSON struct{}

func (failingPublicJSON) MarshalJSON() ([]byte, error) { panic("private-marshaler-panic") }

type singlePublicJSON struct{ calls *int }

func (v singlePublicJSON) MarshalJSON() ([]byte, error) {
	*v.calls++
	if *v.calls > 1 {
		panic("encoded twice")
	}
	return []byte(`"safe"`), nil
}

func TestMountMapperFallbackAndProjection(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, name := range []string{"custom", "decline", "empty-code", "empty-message", "panic", "invalid-details", "cycle", "marshal-panic", "single-marshal", "nil-option"} {
			t.Run(backend+"/"+name, func(t *testing.T) {
				original := gerrors.New("private-conflict", gerrors.CategoryConflict).WithMetadata(map[string]any{"secret": "private-secret"})
				calls := 0
				var events []rpcfiber.FailureEvent
				shared := &cmdrpc.Error{Code: "PUBLIC_CONFLICT", Message: "Resource changed.", Category: "conflict", Retryable: true, Details: map[string]any{"field": "revision"}}
				mapper := func(context.Context, error) *cmdrpc.Error {
					switch name {
					case "decline":
						return nil
					case "empty-code":
						return &cmdrpc.Error{Message: "present"}
					case "empty-message":
						return &cmdrpc.Error{Code: "present"}
					case "panic":
						panic("private-mapper-panic")
					case "invalid-details":
						return &cmdrpc.Error{Code: "CUSTOM", Message: "safe", Details: map[string]any{"bad": func() {}}}
					case "cycle":
						m := map[string]any{}
						m["self"] = m
						return &cmdrpc.Error{Code: "CUSTOM", Message: "safe", Details: m}
					case "marshal-panic":
						return &cmdrpc.Error{Code: "CUSTOM", Message: "safe", Details: map[string]any{"bad": failingPublicJSON{}}}
					case "single-marshal":
						return &cmdrpc.Error{Code: "CUSTOM", Message: "safe", Details: map[string]any{"value": singlePublicJSON{&calls}}}
					default:
						return shared
					}
				}
				opts := []rpcfiber.Option{rpcfiber.WithErrorMapper(mapper), rpcfiber.WithErrorObserver(func(_ router.Context, e rpcfiber.FailureEvent) { events = append(events, e) })}
				if name == "nil-option" {
					opts = append(opts, rpcfiber.WithErrorMapper(nil))
				}
				call := mountErrorFixture(t, backend, errorServer(t, func(context.Context) error { return original }), opts...)
				resp := call(failureBody)
				require.Equal(t, 200, resp.StatusCode)
				var out cmdrpc.ResponseEnvelope[any]
				decodeResponse(t, resp, &out)
				require.NotNil(t, out.Error)
				switch name {
				case "custom":
					assert.Equal(t, shared, out.Error)
					assert.Len(t, events, 1)
				case "single-marshal":
					assert.Equal(t, "safe", out.Error.Details["value"])
					assert.Equal(t, 1, calls)
					assert.Len(t, events, 1)
				case "nil-option":
					assert.Equal(t, "RPC_INVOKE_FAILED", out.Error.Code)
					assert.Len(t, events, 1)
				default:
					assert.Equal(t, cmdrpc.DefaultErrorMapper(context.Background(), original), out.Error)
					if name == "decline" {
						assert.Len(t, events, 1)
					} else {
						require.Len(t, events, 2)
						assert.Equal(t, rpcfiber.FailureStageMapping, events[1].Stage)
						assert.Error(t, events[1].Err)
					}
				}
				assert.Same(t, original, events[0].Err)
				assert.Equal(t, map[string]any{"field": "revision"}, shared.Details)
			})
		}
	}
}

func TestMountCallbackPanicContainment(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		for _, stage := range []string{"metadata", "before", "invoke", "after-success", "after-error", "observer", "mapper-observer"} {
			t.Run(backend+"/"+stage, func(t *testing.T) {
				original := errors.New("private-original")
				panicValue := errors.New("private-panic")
				after := 0
				var events []rpcfiber.FailureEvent
				opts := []rpcfiber.Option{rpcfiber.WithErrorMapper(cmdrpc.DefaultErrorMapper),
					rpcfiber.WithErrorObserver(func(_ router.Context, e rpcfiber.FailureEvent) {
						events = append(events, e)
						if stage == "observer" || stage == "mapper-observer" {
							panic("private-observer")
						}
					}),
					rpcfiber.WithAfterInvokeHook(func(_ router.Context, _ string, _ any, err error) {
						after++
						if stage == "invoke" {
							assert.ErrorIs(t, err, panicValue)
						}
						if strings.HasPrefix(stage, "after-") {
							panic(panicValue)
						}
					}),
				}
				if stage == "metadata" {
					opts = append(opts, rpcfiber.WithMetaExtractor(func(router.Context, *cmdrpc.RequestMeta) { panic(panicValue) }))
				}
				if stage == "before" {
					opts = append(opts, rpcfiber.WithBeforeInvokeHook(func(router.Context, string, any) error { panic(panicValue) }))
				}
				if stage == "mapper-observer" {
					opts = append(opts, rpcfiber.WithErrorMapper(func(context.Context, error) *cmdrpc.Error { panic(panicValue) }))
				}
				srv := errorServer(t, func(context.Context) error {
					if stage == "invoke" {
						panic(panicValue)
					}
					if stage == "after-success" {
						return nil
					}
					return original
				})
				resp := mountErrorFixture(t, backend, srv, opts...)(failureBody)
				wantStatus := 200
				if stage == "before" || stage == "metadata" {
					wantStatus = 400
				}
				require.Equal(t, wantStatus, resp.StatusCode)
				var out cmdrpc.ResponseEnvelope[map[string]any]
				decodeResponse(t, resp, &out)
				if stage == "after-success" {
					assert.Nil(t, out.Error)
					assert.NotEmpty(t, out.Data)
				} else {
					require.NotNil(t, out.Error)
					assert.Equal(t, cmdrpc.CodeInternal, out.Error.Code)
					assert.Empty(t, out.Error.Details)
				}
				if stage == "before" || stage == "metadata" {
					assert.Zero(t, after)
				} else {
					assert.Equal(t, 1, after)
				}
				require.NotEmpty(t, events)
				if stage == "after-error" || stage == "mapper-observer" {
					assert.Len(t, events, 2)
				} else {
					assert.Len(t, events, 1)
				}
				if stage == "invoke" || stage == "before" || stage == "metadata" {
					assert.Same(t, panicValue, events[0].Panic)
					assert.ErrorIs(t, events[0].Err, panicValue)
				}
				if stage == "after-success" || stage == "after-error" {
					assert.Equal(t, rpcfiber.FailureStageAfterInvoke, events[0].Stage)
				}
			})
		}
	}
}

func TestMountServerFailureModesRemainLocal(t *testing.T) {
	for _, mode := range []cmdrpc.FailureMode{cmdrpc.FailureModeReject, cmdrpc.FailureModeRecover, cmdrpc.FailureModeLogAndContinue} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			srv := errorServer(t, func(context.Context) error { panic("private-server-panic") }, cmdrpc.WithFailureMode(mode))
			if mode == cmdrpc.FailureModeReject {
				assert.Panics(t, func() { _, _ = srv.Invoke(t.Context(), "example.fail", &cmdrpc.RequestEnvelope[echoData]{}) })
			} else {
				_, err := srv.Invoke(t.Context(), "example.fail", &cmdrpc.RequestEnvelope[echoData]{})
				require.Error(t, err)
				assert.Contains(t, err.Error(), "private-server-panic")
			}
			for _, backend := range []string{"v2", "v3"} {
				after := 0
				resp := mountErrorFixture(t, backend, srv, rpcfiber.WithErrorMapper(cmdrpc.DefaultErrorMapper), rpcfiber.WithAfterInvokeHook(func(_ router.Context, _ string, _ any, err error) { after++; require.Error(t, err) }))(failureBody)
				var out cmdrpc.ResponseEnvelope[any]
				decodeResponse(t, resp, &out)
				require.NotNil(t, out.Error)
				assert.Equal(t, cmdrpc.CodeInternal, out.Error.Code)
				assert.Equal(t, 1, after)
			}
		})
	}
}
