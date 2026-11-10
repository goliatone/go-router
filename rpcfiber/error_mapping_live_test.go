package rpcfiber_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cmdrpc "github.com/goliatone/go-command/rpc"
	gerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/rpcfiber"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rpcActorKey struct{}

func mountLiveErrors[T any](t *testing.T, r router.Router[T], count *atomic.Int64) {
	t.Helper()
	srv := cmdrpc.NewServer()
	conflict := gerrors.New("private-database-conflict", gerrors.CategoryConflict)
	require.NoError(t, srv.RegisterEndpoint(cmdrpc.NewEndpoint[echoData, map[string]any](cmdrpc.EndpointSpec{Method: "protected"}, func(ctx context.Context, req cmdrpc.RequestEnvelope[echoData]) (cmdrpc.ResponseEnvelope[map[string]any], error) {
		count.Add(1)
		if ctx.Value(rpcActorKey{}) != "trusted-actor" || req.Meta.CorrelationID != "trusted-correlation" {
			return cmdrpc.ResponseEnvelope[map[string]any]{}, fmt.Errorf("lost authenticated request context")
		}
		if req.Data.Name == "fail" {
			return cmdrpc.ResponseEnvelope[map[string]any]{}, conflict
		}
		if req.Data.Name == "public-error" {
			return cmdrpc.ResponseEnvelope[map[string]any]{Error: &cmdrpc.Error{Code: "HANDLER_PUBLIC", Message: "Explicit handler error."}}, nil
		}
		return cmdrpc.ResponseEnvelope[map[string]any]{Data: map[string]any{"receipt": map[string]any{"Accepted": true, "Mode": "queued"}, "result": map[string]any{"failed": true}}}, nil
	})))
	auth := func(next router.HandlerFunc) router.HandlerFunc {
		return func(c router.Context) error {
			if c.Header("Authorization") != "Bearer fixture" {
				return c.JSON(401, map[string]any{"auth": "denied"})
			}
			ctx := context.WithValue(c.Context(), rpcActorKey{}, "trusted-actor")
			ctx = context.WithValue(ctx, "rpc.correlationId", "trusted-correlation")
			c.SetContext(ctx)
			return next(c)
		}
	}
	for _, mode := range []string{"legacy", "structured"} {
		opts := []rpcfiber.Option{rpcfiber.WithInvokePath("/" + mode), rpcfiber.WithDiscoveryEnabled(false), rpcfiber.WithInvokeMiddlewares(auth), rpcfiber.WithMetaMergePolicy(rpcfiber.MetaMergePolicyTransportOverrides)}
		if mode == "structured" {
			opts = append(opts, rpcfiber.WithErrorMapper(func(ctx context.Context, err error) *cmdrpc.Error {
				if ctx.Value(rpcActorKey{}) != "trusted-actor" {
					return &cmdrpc.Error{Code: "LOST_CONTEXT", Message: "Missing context."}
				}
				return cmdrpc.DefaultErrorMapper(ctx, err)
			}))
		}
		require.NoError(t, rpcfiber.Mount(r, srv, opts...))
	}
	r.Get("/ready", func(c router.Context) error { return c.SendStatus(204) })
}

func TestRPCErrorLiveAuthenticatedAdapters(t *testing.T) {
	for _, backend := range []string{"v2", "v3"} {
		t.Run(backend, func(t *testing.T) {
			var count atomic.Int64
			var serve func(net.Listener) error
			var shutdown func(context.Context) error
			if backend == "v2" {
				a := router.NewFiberAdapter()
				mountLiveErrors(t, a.Router(), &count)
				serve = a.WrappedRouter().Listener
				shutdown = a.WrappedRouter().ShutdownWithContext
			} else {
				a := router.NewFiberV3Adapter()
				mountLiveErrors(t, a.Router(), &count)
				serve = func(l net.Listener) error { return a.WrappedRouter().Listener(l) }
				shutdown = a.WrappedRouter().ShutdownWithContext
			}
			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() { done <- serve(listener) }()
			client := &http.Client{Timeout: 2 * time.Second}
			base := "http://" + listener.Addr().String()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				assert.NoError(t, shutdown(ctx))
				client.CloseIdleConnections()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, http.ErrServerClosed) {
						t.Error(err)
					}
				case <-time.After(3 * time.Second):
					t.Error("RPC listener did not stop")
				}
			})
			// Poll an unrelated route before starting semantic requests; do not retry a mutation.
			require.Eventually(t, func() bool {
				resp, e := client.Get(base + "/ready")
				if e != nil {
					return false
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				return resp.StatusCode == 204
			}, 2*time.Second, 10*time.Millisecond)
			for _, mode := range []string{"legacy", "structured"} {
				for _, name := range []string{"denied", "fail", "success", "public-error"} {
					body := fmt.Sprintf(`{"method":"protected","params":{"data":{"name":%q},"meta":{"actorId":"untrusted-actor","correlationId":"untrusted-correlation"}}}`, name)
					req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/"+mode, strings.NewReader(body))
					require.NoError(t, err)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Correlation-ID", "header-correlation")
					if name != "denied" {
						req.Header.Set("Authorization", "Bearer fixture")
					}
					before := count.Load()
					resp, err := client.Do(req)
					require.NoError(t, err)
					raw, err := io.ReadAll(resp.Body)
					require.NoError(t, resp.Body.Close())
					require.NoError(t, err)
					assert.NotContains(t, string(raw), "private-")
					if name == "denied" {
						assert.Equal(t, 401, resp.StatusCode)
						assert.Equal(t, before, count.Load())
						continue
					}
					require.Equal(t, 200, resp.StatusCode)
					assert.Equal(t, before+1, count.Load(), "one invocation, no retry")
					var out cmdrpc.ResponseEnvelope[map[string]any]
					require.NoError(t, json.Unmarshal(raw, &out))
					switch name {
					case "fail":
						require.NotNil(t, out.Error)
						want := "RPC_INVOKE_FAILED"
						if mode == "structured" {
							want = cmdrpc.CodeConflict
						}
						assert.Equal(t, want, out.Error.Code)
						assert.NotContains(t, out.Error.Details, "cause")
					case "public-error":
						require.NotNil(t, out.Error)
						assert.Equal(t, "HANDLER_PUBLIC", out.Error.Code)
					default:
						assert.Nil(t, out.Error)
						assert.Equal(t, map[string]any{"receipt": map[string]any{"Accepted": true, "Mode": "queued"}, "result": map[string]any{"failed": true}}, out.Data)
					}
				}
			}
		})
	}
}
