package ingresscandidate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fiber2 "github.com/gofiber/fiber/v2"
	fiber3 "github.com/gofiber/fiber/v3"
	"github.com/goliatone/go-admin/admin"
	"github.com/goliatone/go-admin/quickstart"
	auth "github.com/goliatone/go-auth"
	command "github.com/goliatone/go-command"
	registry "github.com/goliatone/go-command/registry"
	rpc "github.com/goliatone/go-command/rpc"
	router "github.com/goliatone/go-router"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const secret = "private-candidate-sentinel"

var domainFailure = admin.NewDomainError(admin.TextCodeConflict, secret, map[string]any{"secret": secret})

type permissionKey struct{}
type legacyIDKey struct{}

func requestID(ctx context.Context) string {
	if id, ok := router.IngressRequestIDFromContext(ctx); ok {
		return id
	}
	id, _ := ctx.Value(legacyIDKey{}).(string)
	return id
}
func legacyCorrelation(next router.HandlerFunc) router.HandlerFunc {
	return func(c router.Context) error {
		id := "legacy-" + uuid.NewString()
		c.SetContext(context.WithValue(c.Context(), legacyIDKey{}, id))
		c.SetHeader("X-Request-ID", id)
		return next(c)
	}
}

type identity struct{}

func (identity) Wrap(c router.Context) error {
	token := strings.TrimPrefix(c.Header("Authorization"), "Bearer ")
	if token != "writer" && token != "reader" && token != "forbidden" {
		return c.JSON(401, map[string]string{"code": "UNAUTHENTICATED"})
	}
	c.SetContext(context.WithValue(auth.WithActorContext(c.Context(), &auth.ActorContext{ActorID: "actor-" + token, Subject: "actor-" + token, TenantID: "tenant-owned", OrganizationID: "org-owned"}), permissionKey{}, token))
	return nil
}
func (i identity) WrapHandler(next router.HandlerFunc) router.HandlerFunc {
	return func(c router.Context) error {
		if c.Header("Authorization") != "Bearer writer" && c.Header("Authorization") != "Bearer reader" && c.Header("Authorization") != "Bearer forbidden" {
			return i.Wrap(c)
		}
		if err := i.Wrap(c); err != nil {
			return err
		}
		return next(c)
	}
}

type permissions struct{}

func (permissions) ResolvedPermissions(ctx context.Context) []string {
	switch ctx.Value(permissionKey{}) {
	case "writer":
		return []string{"admin.commands.dispatch", "items.read", "items.write"}
	case "reader":
		return []string{"admin.commands.dispatch", "items.read"}
	}
	return []string{"admin.commands.dispatch"}
}
func (p permissions) Can(ctx context.Context, action, resource string) bool {
	if resource != "items" {
		return false
	}
	for _, v := range p.ResolvedPermissions(ctx) {
		if v == action {
			return true
		}
	}
	return false
}

type input struct {
	Value string `json:"value"`
}
type output struct {
	Value        string `json:"value"`
	Actor        string `json:"actor"`
	Tenant       string `json:"tenant"`
	Organization string `json:"organization"`
	RequestID    string `json:"requestId"`
}
type change struct{ Input input }

func (change) Type() string { return "candidate.change" }

type hiddenChange struct{}

func (hiddenChange) Type() string { return "candidate.hidden" }

type fakeService struct{ query, mutation, hidden atomic.Int32 }

func (s *fakeService) result(ctx context.Context, in input) (output, error) {
	actor, ok := auth.ActorFromContext(ctx)
	id := requestID(ctx)
	if !ok || id == "" || actor.TenantID != "tenant-owned" || actor.OrganizationID != "org-owned" || !strings.HasPrefix(actor.ActorID, "actor-") {
		return output{}, errors.New("lost trusted context")
	}
	switch in.Value {
	case "error":
		return output{}, domainFailure
	case "panic":
		panic(secret)
	}
	return output{in.Value, actor.ActorID, actor.TenantID, actor.OrganizationID, id}, nil
}
func (s *fakeService) Execute(ctx context.Context, m change) error {
	s.mutation.Add(1)
	opts, ok := command.DispatchOptionsFromContext(ctx)
	if !ok {
		return errors.New("missing dispatch options")
	}
	if err := validateTrustedDispatch(ctx, opts); err != nil {
		return err
	}
	out, err := s.result(ctx, m.Input)
	if err == nil {
		command.ResultFromContext[output](ctx).Store(out)
	}
	return err
}

type candidate struct {
	request                 func(*http.Request) (*http.Response, error)
	base, path              string
	service                 *fakeService
	strategy, after, policy atomic.Int32
	mu                      sync.Mutex
	events                  map[string]router.IngressEvent
	rpcEvents               map[string][]rpcDiagnostic
	mapperCalls             map[string]int
}

func setup(t *testing.T, major, transport, mode string) *candidate {
	return setupCandidate(t, major, transport, mode, false)
}

func setupCandidate(t *testing.T, major, transport, mode string, hostRPC bool) *candidate {
	t.Helper()
	resetRegistry := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, registry.Stop(ctx))
	}
	resetRegistry()
	t.Cleanup(resetRegistry)
	f := &candidate{service: &fakeService{}, events: make(map[string]router.IngressEvent), rpcEvents: make(map[string][]rpcDiagnostic), mapperCalls: make(map[string]int)}
	cfg := quickstart.NewAdminConfig("/admin", "Candidate", "en")
	transportConfig := quickstart.RPCTransportConfig{
		Enabled: true, StrictStartup: true, PermissionMode: admin.RPCCommandPermissionModeExact,
		CommandRules: map[string]admin.RPCCommandRule{"candidate.change": {Permission: "items.write", Resource: "items"}},
		Authorize: func(ctx context.Context, p admin.RPCCommandPolicyInput) error {
			f.policy.Add(1)
			if p.CommandName != "candidate.change" || p.Rule.Permission != "items.write" || p.ActorID != "actor-writer" || p.TenantID != "tenant-owned" {
				return errors.New("wrong policy inputs")
			}
			if p.OrganizationID != "org-owned" || p.Subject != "actor-writer" || p.RequestID != requestID(ctx) || p.CorrelationID != requestID(ctx) {
				return errors.New("wrong policy identity/correlation")
			}
			if err := validateTrustedDispatch(ctx, p.Dispatch); err != nil {
				return err
			}
			for key, want := range map[string]any{"actor_id": "actor-writer", "user_id": "actor-writer", "tenant_id": "tenant-owned", "organization_id": "org-owned"} {
				if p.Payload[key] != want {
					return fmt.Errorf("wrong enriched payload %s", key)
				}
			}
			if !reflect.DeepEqual(p.Metadata, p.Dispatch.Metadata) {
				return errors.New("policy metadata differs from dispatch")
			}
			return nil
		},
		ErrorMapper: admin.DefaultRPCErrorMapper,
		MetaExtractor: func(c router.Context, m *rpc.RequestMeta) {
			id := requestID(c.Context())
			m.RequestID = id
			m.CorrelationID = id
			m.Tenant = "tenant-owned"
		},
		BeforeInvoke: func(c router.Context, method string, _ any) error {
			if method == "candidate.query" && !(permissions{}).Can(c.Context(), "items.read", "items") {
				return admin.ErrForbidden
			}
			return nil
		},
		AfterInvoke: func(_ router.Context, _ string, result any, _ error) {
			f.after.Add(1)
			if out, ok := result.(rpc.ResponseEnvelope[output]); ok && out.Data.Value == "after-panic" {
				panic(secret)
			}
		},
	}
	deps := admin.Dependencies{Authenticator: identity{}, Authorizer: permissions{}, RPCCommandPolicyHook: transportConfig.Authorize}
	cfg.Commands.RPC = admin.RPCCommandConfig{PermissionMode: transportConfig.PermissionMode, Commands: transportConfig.CommandRules}
	adminOptions := []quickstart.AdminOption{quickstart.WithFeatureSet(map[string]bool{"commands": true}), quickstart.WithAdminDependencies(deps)}
	if !hostRPC {
		adminOptions = append(adminOptions, quickstart.WithRPCTransport(transportConfig))
	}
	adm, _, err := quickstart.NewAdmin(cfg, quickstart.AdapterHooks{}, adminOptions...)

	require.NoError(t, err)
	t.Cleanup(adm.Commands().Close)
	set, err := adm.Commands().NewRegistrationSet("bounded-candidate")
	require.NoError(t, err)
	require.NoError(t, admin.RegisterSetCommand(set, f.service))
	require.NoError(t, admin.RegisterSetCommand(set, command.CommandFunc[hiddenChange](func(context.Context, hiddenChange) error { f.service.hidden.Add(1); return nil })))
	require.NoError(t, admin.RegisterSetMessageFactory(set, "candidate.hidden", func(map[string]any, []string) (hiddenChange, error) { return hiddenChange{}, nil }))
	binding, err := admin.NewRPCCommandBinding[input, output](rpc.CommandContractFor[input, output]("candidate.change"), rpc.ExportOptions{Overrides: map[reflect.Type]rpc.WireSchema{reflect.TypeFor[any](): {Kind: "json"}}, Bounds: map[reflect.Type]rpc.IntegerBounds{reflect.TypeFor[time.Duration](): {Minimum: 0, Maximum: rpc.MaxSafeInteger}}})
	require.NoError(t, err)
	require.NoError(t, admin.RegisterSetTypedRPCCommand(set, binding, func(_ context.Context, in input, _ []string) (change, error) { return change{in}, nil }))
	handle, err := set.Commit()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handle.Close()) })
	require.NoError(t, adm.RPCServer().RegisterEndpoint(rpc.NewEndpoint[input, output](rpc.EndpointSpec{Method: "candidate.query", Kind: rpc.MethodKindQuery, Permissions: []string{"items.read"}}, func(ctx context.Context, req rpc.RequestEnvelope[input]) (rpc.ResponseEnvelope[output], error) {
		f.service.query.Add(1)
		id := requestID(ctx)
		if req.Meta.RequestID != id || req.Meta.CorrelationID != id || req.Meta.Tenant != "tenant-owned" {
			return rpc.ResponseEnvelope[output]{}, errors.New("untrusted metadata")
		}
		out, err := f.service.result(ctx, req.Data)
		return rpc.ResponseEnvelope[output]{Data: out}, err
	})))
	opts := router.BoundedIngressOptions{Observer: func(_ router.IngressCancellation, e router.IngressEvent) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, exists := f.events[e.RequestID]; exists {
			t.Error("duplicate ingress observation")
		}
		f.events[e.RequestID] = e
		return nil
	}}
	switch mode {
	case "reject":
		opts.UnknownMethod.Policy = router.UnknownMethodReject
	case "custom":
		opts.UnknownMethod = router.UnknownMethodOptions{Policy: router.UnknownMethodCustom, Handler: router.UnknownMethodHandlerFunc(func(_ context.Context, r router.UnknownMethodRequest) (router.UnknownMethodDecision, error) {
			f.strategy.Add(1)
			if r.Method == "X-DELEGATE" {
				return router.UnknownMethodDecision{Action: router.DelegateNative}, nil
			}
			return router.UnknownMethodDecision{Action: router.RejectUnknownMethod, Rejection: router.UnknownMethodRejection{Status: 429, Code: "METHOD_REJECTED", Message: "method rejected"}}, nil
		})}
	}
	var managed router.ManagedServer
	if major == "v2" {
		server := router.NewFiberAdapter(func(*fiber2.App) *fiber2.App { return fiber2.New(fiber2.Config{DisableStartupMessage: true}) })
		if mode == "legacy" {
			server.Router().Use(legacyCorrelation)
		} else {
			require.NoError(t, server.(router.BoundedIngressServer).ConfigureBoundedIngress(opts))
		}
		require.NoError(t, adm.Initialize(server.Router()))
		if hostRPC {
			mountProtectedRPC(t, server.Router(), adm, f, transportConfig)
		}
		require.NoError(t, server.(router.StartupInitializer).Initialize())
		managed = server.(router.ManagedServer)
		if transport == "test" {
			app := server.WrappedRouter()
			f.request = func(r *http.Request) (*http.Response, error) { return app.Test(r, 3000) }
		}
	} else {
		server := router.NewFiberV3AdapterWithConfig(router.FiberV3AdapterConfig{ListenConfig: fiber3.ListenConfig{DisableStartupMessage: true}})
		if mode == "legacy" {
			server.Router().Use(legacyCorrelation)
		} else {
			require.NoError(t, server.(router.BoundedIngressServer).ConfigureBoundedIngress(opts))
		}
		require.NoError(t, adm.Initialize(server.Router()))
		if hostRPC {
			mountProtectedRPC(t, server.Router(), adm, f, transportConfig)
		}
		require.NoError(t, server.(router.StartupInitializer).Initialize())
		managed = server.(router.ManagedServer)
		if transport == "test" {
			app := server.WrappedRouter()
			f.request = func(r *http.Request) (*http.Response, error) {
				return app.Test(r, fiber3.TestConfig{Timeout: 3 * time.Second})
			}
		}
	}
	require.True(t, adm.RPCExposureReadiness().Ready)
	f.base = "http://example.test"
	f.path = adm.AdminAPIBasePath() + "/rpc"
	if transport == "socket" {
		run, err := managed.StartManaged(context.Background(), router.ManagedOptions{Address: "127.0.0.1:0", DrainTimeout: time.Second})
		require.NoError(t, err)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, run.Stop(ctx))
			require.NoError(t, run.Wait(ctx))
		})
		select {
		case <-run.Serving():
		case <-run.Done():
			t.Fatalf("startup: %v", run.Wait(context.Background()))
		case <-time.After(3 * time.Second):
			t.Fatal("startup timeout")
		}
		tr := &http.Transport{}
		t.Cleanup(tr.CloseIdleConnections)
		client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
		f.request = client.Do
		f.base = "http://" + run.Address()
	}
	return f
}
func (f *candidate) call(t *testing.T, method, path, token, body string, fault ...string) (*http.Response, []byte, router.IngressEvent) {
	t.Helper()
	req, err := http.NewRequest(method, f.base+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-ID", "forged-id")
	req.Header.Set("X-Tenant-ID", "forged-tenant")
	req.Header.Set("X-Correlation-ID", "forged-correlation")
	if len(fault) > 0 {
		req.Header.Set("X-Candidate-Fault", fault[0])
	}
	res, err := f.request(req)
	require.NoError(t, err)
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	ids := res.Header.Values("X-Request-ID")
	require.Len(t, ids, 1)
	require.NotEmpty(t, ids[0])
	require.NotEqual(t, "forged-id", ids[0])
	require.NotContains(t, string(b), secret)
	f.mu.Lock()
	event, ok := f.events[ids[0]]
	f.mu.Unlock()
	require.True(t, ok)
	require.Equal(t, res.StatusCode, event.Status)
	return res, b, event
}
func payload(method, value string, mutation bool) string {
	data := any(input{Value: value})
	if mutation {
		data = command.CommandDispatchRequest{Name: method, Payload: map[string]any{"value": value}, Options: command.DispatchOptions{
			CorrelationID: "forged-options-correlation",
			Metadata:      map[string]any{"actor_id": "forged-actor", "subject": "forged-subject", "tenant_id": "forged-tenant", "organization_id": "forged-org", "request_id": "forged-id", "correlation_id": "forged-correlation", "roles": []string{"admin"}, "permissions": []string{"*"}, "scope": map[string]any{"tenant_id": "forged-tenant", "organization_id": "forged-org"}},
		}}
		method = admin.RPCMethodCommandDispatch
	}
	wire := struct {
		Method string                   `json:"method"`
		Params rpc.RequestEnvelope[any] `json:"params"`
	}{
		method, rpc.RequestEnvelope[any]{Data: data, Meta: rpc.RequestMeta{RequestID: "forged-id", CorrelationID: "forged-correlation", Tenant: "forged-tenant", ActorID: "forged-actor", Roles: []string{"admin"}, Permissions: []string{"*"}, Scope: map[string]any{"organization_id": "forged-org"}}},
	}
	b, err := json.Marshal(wire)
	if err != nil {
		panic(err)
	} // All fixture values must have a valid wire representation.
	return string(b)
}

func validateTrustedDispatch(ctx context.Context, opts command.DispatchOptions) error {
	id := requestID(ctx)
	if id == "" || opts.CorrelationID != id {
		return errors.New("untrusted dispatch correlation")
	}
	for key, want := range map[string]any{"actor_id": "actor-writer", "subject": "actor-writer", "tenant_id": "tenant-owned", "organization_id": "org-owned", "request_id": id, "correlation_id": id} {
		if opts.Metadata[key] != want {
			return fmt.Errorf("untrusted dispatch metadata %s", key)
		}
	}
	if !reflect.DeepEqual(opts.Metadata["scope"], map[string]any{"tenant_id": "tenant-owned", "organization_id": "org-owned"}) {
		return errors.New("untrusted dispatch scope")
	}
	for _, key := range []string{"roles", "permissions"} {
		if _, ok := opts.Metadata[key]; ok {
			return fmt.Errorf("untrusted dispatch %s", key)
		}
	}
	raw, err := json.Marshal(opts.Metadata)
	if err != nil || strings.Contains(string(raw), "forged-") {
		return errors.New("forged dispatch metadata survived")
	}
	return nil
}

func TestAdversarialDispatchWire(t *testing.T) {
	var wire struct {
		Method string                                              `json:"method"`
		Params rpc.RequestEnvelope[command.CommandDispatchRequest] `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(payload("candidate.change", "ok", true)), &wire))
	require.Equal(t, admin.RPCMethodCommandDispatch, wire.Method)
	require.Equal(t, "forged-options-correlation", wire.Params.Data.Options.CorrelationID)
	for key, want := range map[string]any{"actor_id": "forged-actor", "subject": "forged-subject", "tenant_id": "forged-tenant", "organization_id": "forged-org", "request_id": "forged-id", "correlation_id": "forged-correlation"} {
		require.Equal(t, want, wire.Params.Data.Options.Metadata[key])
	}
	require.Equal(t, []any{"admin"}, wire.Params.Data.Options.Metadata["roles"])
	require.Equal(t, []any{"*"}, wire.Params.Data.Options.Metadata["permissions"])
	require.Equal(t, map[string]any{"tenant_id": "forged-tenant", "organization_id": "forged-org"}, wire.Params.Data.Options.Metadata["scope"])
}

func TestProtectedCandidate(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		for _, transport := range []string{"test", "socket"} {
			for _, mode := range []string{"native", "reject", "custom"} {
				t.Run(major+"/"+transport+"/"+mode, func(t *testing.T) {
					f := setup(t, major, transport, mode)
					for _, mutation := range []bool{false, true} {
						method := "candidate.query"
						if mutation {
							method = "candidate.change"
						}
						res, b, event := f.call(t, "POST", f.path, "writer", payload(method, "ok", mutation))
						require.Equal(t, 200, res.StatusCode)
						require.EqualValues(t, 1, event.Dispatches)
						var envelope struct {
							Data  json.RawMessage `json:"data"`
							Error *rpc.Error      `json:"error"`
						}
						require.NoError(t, json.Unmarshal(b, &envelope))
						require.Nil(t, envelope.Error, "HTTP 200 does not establish RPC success: %s", b)
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
					require.EqualValues(t, 1, f.policy.Load())
					require.Zero(t, f.service.hidden.Load())
					for _, tc := range []struct {
						name, token string
						mutation    bool
						method      string
					}{{"unauthenticated", "", true, "candidate.change"}, {"forbidden", "reader", true, "candidate.change"}, {"query-forbidden", "forbidden", false, "candidate.query"}, {"unallowlisted", "writer", true, "candidate.hidden"}} {
						t.Run(tc.name, func(t *testing.T) {
							res, b, _ := f.call(t, "POST", f.path, tc.token, payload(tc.method, "ok", tc.mutation))
							if tc.token == "" {
								require.Equal(t, 401, res.StatusCode)
							} else {
								var out rpc.ResponseEnvelope[any]
								require.NoError(t, json.Unmarshal(b, &out))
								require.NotNil(t, out.Error, "denial returned no RPC error: %s", b)
								if tc.name == "unallowlisted" {
									require.Equal(t, rpc.CodeNotFound, out.Error.Code)
								} else {
									require.Equal(t, rpc.CodeForbidden, out.Error.Code)
								}
							}
							require.EqualValues(t, 1, f.service.query.Load())
							require.EqualValues(t, 1, f.service.mutation.Load())
							require.EqualValues(t, 1, f.policy.Load())
							require.Zero(t, f.service.hidden.Load())
						})
					}
					res, _, _ := f.call(t, "GET", f.path+"/endpoints", "writer", "")
					require.Equal(t, 404, res.StatusCode)
					_, discoveryBody, _ := f.call(t, "POST", f.path, "writer", payload(admin.RPCMethodCommandList, "", false))
					var discovery rpc.ResponseEnvelope[any]
					require.NoError(t, json.Unmarshal(discoveryBody, &discovery))
					require.NotNil(t, discovery.Error)
					require.Equal(t, rpc.CodeNotFound, discovery.Error.Code)
					require.EqualValues(t, 1, f.service.query.Load())
					require.EqualValues(t, 1, f.service.mutation.Load())

					for _, value := range []string{"error", "panic"} {
						_, b, _ := f.call(t, "POST", f.path, "writer", payload("candidate.query", value, false))
						var out rpc.ResponseEnvelope[any]
						require.NoError(t, json.Unmarshal(b, &out))
						require.NotNil(t, out.Error)
						if value == "error" {
							require.Equal(t, rpc.CodeConflict, out.Error.Code)
						}
					}
					beforeHook := f.after.Load()
					_, hookBody, _ := f.call(t, "POST", f.path, "writer", payload("candidate.query", "after-panic", false))
					var hookOut rpc.ResponseEnvelope[output]
					require.NoError(t, json.Unmarshal(hookBody, &hookOut))
					require.Nil(t, hookOut.Error)
					require.Equal(t, "after-panic", hookOut.Data.Value)
					require.EqualValues(t, beforeHook+1, f.after.Load())
					beforeQuery, beforeMutation, beforeAfter := f.service.query.Load(), f.service.mutation.Load(), f.after.Load()
					for _, path := range []string{f.path, "/missing"} {
						for _, method := range []string{"X-CANDIDATE-UNKNOWN", "X-DELEGATE"} {
							res, b, event := f.call(t, method, path, "writer", payload("candidate.change", "never", true))
							if mode == "native" || mode == "custom" && method == "X-DELEGATE" {
								require.EqualValues(t, 1, event.Dispatches)
							} else {
								want := 501
								if mode == "custom" {
									want = 429
								}
								require.Equal(t, want, res.StatusCode)
								require.EqualValues(t, 0, event.Dispatches)
								var out router.IngressErrorEnvelope
								require.NoError(t, json.Unmarshal(b, &out))
								require.Equal(t, res.Header.Get("X-Request-ID"), out.RequestID)
								require.Empty(t, res.Header.Get("Allow"))
							}
						}
					}
					require.Equal(t, beforeQuery, f.service.query.Load())
					require.Equal(t, beforeMutation, f.service.mutation.Load())
					require.Equal(t, beforeAfter, f.after.Load())
					if mode == "custom" {
						require.EqualValues(t, 4, f.strategy.Load())
					} else {
						require.Zero(t, f.strategy.Load())
					}

					t.Run("concurrent-context", func(t *testing.T) {
						before := f.service.query.Load()
						var wg sync.WaitGroup
						var ids sync.Map
						for n := 0; n < 16; n++ {
							wg.Add(1)
							go func() {
								defer wg.Done()
								token := "writer"
								if n%2 == 0 {
									token = "reader"
								}
								res, b, _ := f.call(t, "POST", f.path, token, payload("candidate.query", strconv.Itoa(n), false))
								var out rpc.ResponseEnvelope[output]
								require.NoError(t, json.Unmarshal(b, &out))
								require.Nil(t, out.Error)
								require.Equal(t, output{strconv.Itoa(n), "actor-" + token, "tenant-owned", "org-owned", res.Header.Get("X-Request-ID")}, out.Data)
								_, duplicate := ids.LoadOrStore(out.Data.RequestID, true)
								require.False(t, duplicate)
							}()
						}
						wg.Wait()
						require.EqualValues(t, before+16, f.service.query.Load())
					})
					t.Run("concurrent-dispatch-options", func(t *testing.T) {
						before, policies := f.service.mutation.Load(), f.policy.Load()
						var wg sync.WaitGroup
						var ids sync.Map
						for n := 0; n < 8; n++ {
							wg.Add(1)
							go func() {
								defer wg.Done()
								res, b, _ := f.call(t, "POST", f.path, "writer", payload("candidate.change", strconv.Itoa(n), true))
								var out rpc.ResponseEnvelope[struct {
									Result output `json:"result"`
								}]
								require.NoError(t, json.Unmarshal(b, &out))
								require.Nil(t, out.Error)
								require.Equal(t, output{strconv.Itoa(n), "actor-writer", "tenant-owned", "org-owned", res.Header.Get("X-Request-ID")}, out.Data.Result)
								_, duplicate := ids.LoadOrStore(out.Data.Result.RequestID, true)
								require.False(t, duplicate)
							}()
						}
						wg.Wait()
						require.EqualValues(t, before+8, f.service.mutation.Load())
						require.EqualValues(t, policies+8, f.policy.Load())
					})
					f.mu.Lock()
					safeEvents, encodeErr := json.Marshal(f.events)
					f.mu.Unlock()
					require.NoError(t, encodeErr)
					require.NotContains(t, string(safeEvents), secret)
					require.NotContains(t, string(safeEvents), "forged-")
				})
			}
		}
	}
}

// D01 compatibility on the same bytes: legacy ingress stays host-owned. This
// smoke does not claim that bounded correlation exists when it is not enabled.
func TestLegacyHostingCandidate(t *testing.T) {
	for _, major := range []string{"v2", "v3"} {
		t.Run(major, func(t *testing.T) {
			f := setup(t, major, "socket", "legacy")
			for _, mutation := range []bool{false, true} {
				name := "candidate.query"
				if mutation {
					name = "candidate.change"
				}
				req, err := http.NewRequest("POST", f.base+f.path, strings.NewReader(payload(name, "legacy", mutation)))
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer writer")
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Request-ID", "client")
				res, err := f.request(req)
				require.NoError(t, err)
				body, err := io.ReadAll(res.Body)
				res.Body.Close()
				require.NoError(t, err)
				require.Equal(t, 200, res.StatusCode)
				var envelope rpc.ResponseEnvelope[json.RawMessage]
				require.NoError(t, json.Unmarshal(body, &envelope))
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
				require.Equal(t, output{"legacy", "actor-writer", "tenant-owned", "org-owned", res.Header.Get("X-Request-ID")}, out)
				require.True(t, strings.HasPrefix(out.RequestID, "legacy-"))
			}
			for _, tc := range []struct{ token, name string }{{"", "candidate.change"}, {"reader", "candidate.change"}, {"writer", "candidate.hidden"}} {
				req, err := http.NewRequest("POST", f.base+f.path, strings.NewReader(payload(tc.name, "never", true)))
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+tc.token)
				req.Header.Set("Content-Type", "application/json")
				res, err := f.request(req)
				require.NoError(t, err)
				body, err := io.ReadAll(res.Body)
				res.Body.Close()
				require.NoError(t, err)
				if tc.token == "" {
					require.Equal(t, 401, res.StatusCode)
				} else {
					var out rpc.ResponseEnvelope[any]
					require.NoError(t, json.Unmarshal(body, &out))
					require.NotNil(t, out.Error)
				}
			}
			req, err := http.NewRequest("GET", f.base+f.path+"/endpoints", nil)
			require.NoError(t, err)
			res, err := f.request(req)
			require.NoError(t, err)
			res.Body.Close()
			require.Equal(t, 404, res.StatusCode)
			require.EqualValues(t, 1, f.service.query.Load())
			require.EqualValues(t, 1, f.service.mutation.Load())
			require.Zero(t, f.service.hidden.Load())
			require.Empty(t, f.events)
			require.Zero(t, f.strategy.Load())
		})
	}
}
