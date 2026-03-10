package rpcfiber_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmdrpc "github.com/goliatone/go-command/rpc"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/rpcfiber"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type echoData struct {
	Name string `json:"name"`
}

type strictValueRequest struct {
	Data echoData           `json:"data"`
	Meta cmdrpc.RequestMeta `json:"meta,omitempty"`
}

type strictValueMethodServer struct {
	payload any
}

func (s *strictValueMethodServer) Invoke(_ context.Context, method string, payload any) (any, error) {
	if method != "example.value" {
		return nil, fmt.Errorf("unexpected method %q", method)
	}
	req, ok := payload.(strictValueRequest)
	if !ok {
		return nil, fmt.Errorf("unexpected payload type %T", payload)
	}
	s.payload = payload
	return cmdrpc.ResponseEnvelope[map[string]any]{
		Data: map[string]any{
			"name":    req.Data.Name,
			"actorId": req.Meta.ActorID,
		},
	}, nil
}

func (s *strictValueMethodServer) NewRequestForMethod(method string) (any, error) {
	if method != "example.value" {
		return nil, errors.New("not found")
	}
	return strictValueRequest{}, nil
}

func (s *strictValueMethodServer) EndpointsMeta() []cmdrpc.Endpoint {
	return []cmdrpc.Endpoint{{Method: "example.value"}}
}

func TestMountFiber_DefaultRoutesAndMethodAwareDecode(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()
	srv := cmdrpc.NewServer()

	var capturedMeta cmdrpc.RequestMeta
	err := srv.RegisterEndpoint(cmdrpc.NewEndpoint[echoData, map[string]any](
		cmdrpc.EndpointSpec{
			Method: "example.echo",
			Kind:   cmdrpc.MethodKindQuery,
		},
		func(_ context.Context, req cmdrpc.RequestEnvelope[echoData]) (cmdrpc.ResponseEnvelope[map[string]any], error) {
			capturedMeta = req.Meta
			return cmdrpc.ResponseEnvelope[map[string]any]{
				Data: map[string]any{
					"name": req.Data.Name,
				},
			}, nil
		},
	))
	require.NoError(t, err)
	require.NoError(t, rpcfiber.MountFiber(r, srv))

	app := adapter.WrappedRouter()

	resp := testRequest(t, app, http.MethodGet, "/api/rpc/endpoints", "", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var discovery struct {
		Endpoints []cmdrpc.Endpoint `json:"endpoints"`
	}
	decodeResponse(t, resp, &discovery)
	require.Len(t, discovery.Endpoints, 1)
	assert.Equal(t, "example.echo", discovery.Endpoints[0].Method)

	body := `{"method":"example.echo","params":{"data":{"name":"Ada"},"meta":{"requestId":"payload-req"}}}`
	headers := map[string]string{
		"X-Actor-ID":       "header-actor",
		"X-Correlation-ID": "corr-123",
	}
	resp = testRequest(t, app, http.MethodPost, "/api/rpc?tenant=query-tenant&roles=reader&roles=writer", body, headers)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var output cmdrpc.ResponseEnvelope[map[string]any]
	decodeResponse(t, resp, &output)
	require.Nil(t, output.Error)
	assert.Equal(t, "Ada", output.Data["name"])

	assert.Equal(t, "header-actor", capturedMeta.ActorID)
	assert.Equal(t, "query-tenant", capturedMeta.Tenant)
	assert.Equal(t, "payload-req", capturedMeta.RequestID)
	assert.Equal(t, "corr-123", capturedMeta.CorrelationID)
	assert.Equal(t, []string{"reader", "writer"}, capturedMeta.Roles)
	assert.Equal(t, []string{"query-tenant"}, capturedMeta.Query["tenant"])
	assert.Equal(t, []string{"reader", "writer"}, capturedMeta.Query["roles"])
	assert.Equal(t, "header-actor", headerValue(capturedMeta.Headers, "x-actor-id"))

	invalidBody := `{"method":"example.echo","params":{"data":"wrong-shape"}}`
	resp = testRequest(t, app, http.MethodPost, "/api/rpc", invalidBody, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var invalidOut cmdrpc.ResponseEnvelope[any]
	decodeResponse(t, resp, &invalidOut)
	require.NotNil(t, invalidOut.Error)
	assert.Equal(t, "RPC_INVALID_PARAMS", invalidOut.Error.Code)
}

func TestMountFiber_CustomMetaExtractorsAndHooks(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()

	r.Use(router.ToMiddleware(func(c router.Context) error {
		ctx := context.WithValue(c.Context(), "rpc.actorId", "actor-from-context")
		ctx = context.WithValue(ctx, "rpc.requestId", "request-from-context")
		c.SetContext(ctx)
		return c.Next()
	}))

	srv := cmdrpc.NewServer()
	var capturedMeta cmdrpc.RequestMeta
	err := srv.RegisterEndpoint(cmdrpc.NewEndpoint[echoData, map[string]any](
		cmdrpc.EndpointSpec{
			Method: "example.custom",
			Kind:   cmdrpc.MethodKindQuery,
		},
		func(_ context.Context, req cmdrpc.RequestEnvelope[echoData]) (cmdrpc.ResponseEnvelope[map[string]any], error) {
			capturedMeta = req.Meta
			return cmdrpc.ResponseEnvelope[map[string]any]{
				Data: map[string]any{
					"tenant": req.Meta.Tenant,
				},
			}, nil
		},
	))
	require.NoError(t, err)

	beforeCalled := false
	require.NoError(t, rpcfiber.MountFiber(
		r,
		srv,
		rpcfiber.WithInvokePath("/api/:tenant/rpc"),
		rpcfiber.WithEndpointsPath("/api/:tenant/rpc/endpoints"),
		rpcfiber.WithMetaExtractor(func(_ router.Context, meta *cmdrpc.RequestMeta) {
			meta.Scope = map[string]any{"source": "custom-hook"}
		}),
		rpcfiber.WithBeforeInvokeHook(func(_ router.Context, _ string, _ any) error {
			beforeCalled = true
			return nil
		}),
	))

	app := adapter.WrappedRouter()

	resp := testRequest(t, app, http.MethodGet, "/api/acme/rpc/endpoints", "", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var discovery struct {
		Endpoints []cmdrpc.Endpoint `json:"endpoints"`
	}
	decodeResponse(t, resp, &discovery)
	require.Len(t, discovery.Endpoints, 1)
	assert.Equal(t, "example.custom", discovery.Endpoints[0].Method)

	body := `{"method":"example.custom","params":{"data":{"name":"ok"}}}`
	resp = testRequest(t, app, http.MethodPost, "/api/acme/rpc", body, map[string]string{
		"X-Actor-ID": "header-actor",
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out cmdrpc.ResponseEnvelope[map[string]any]
	decodeResponse(t, resp, &out)
	require.Nil(t, out.Error)
	assert.True(t, beforeCalled)
	assert.Equal(t, "acme", out.Data["tenant"])

	assert.Equal(t, "actor-from-context", capturedMeta.ActorID)
	assert.Equal(t, "acme", capturedMeta.Tenant)
	assert.Equal(t, "request-from-context", capturedMeta.RequestID)
	assert.Equal(t, "acme", capturedMeta.Params["tenant"])
	assert.Equal(t, "custom-hook", capturedMeta.Scope["source"])
}

func TestMountFiber_ReturnsRPCErrorEnvelopeWithoutAPIErrWrapper(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()
	srv := cmdrpc.NewServer()

	err := srv.RegisterEndpoint(cmdrpc.NewEndpoint[echoData, string](
		cmdrpc.EndpointSpec{
			Method: "example.fail",
			Kind:   cmdrpc.MethodKindCommand,
		},
		func(_ context.Context, _ cmdrpc.RequestEnvelope[echoData]) (cmdrpc.ResponseEnvelope[string], error) {
			return cmdrpc.ResponseEnvelope[string]{}, errors.New("boom")
		},
	))
	require.NoError(t, err)
	require.NoError(t, rpcfiber.MountFiber(r, srv))

	app := adapter.WrappedRouter()
	body := `{"method":"example.fail","params":{"data":{"name":"Ada"}}}`
	resp := testRequest(t, app, http.MethodPost, "/api/rpc", body, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out map[string]any
	decodeResponse(t, resp, &out)
	_, hasStatus := out["status"]
	assert.False(t, hasStatus)

	errorObj, ok := out["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "RPC_INVOKE_FAILED", errorObj["code"])
	assert.Equal(t, "rpc invocation failed", errorObj["message"])
}

func TestMountFiber_PreservesValuePayloadTypeWhenInjectingMeta(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()
	srv := &strictValueMethodServer{}

	require.NoError(t, rpcfiber.MountFiber(r, srv))

	app := adapter.WrappedRouter()
	body := `{"method":"example.value","params":{"data":{"name":"Ada"}}}`
	resp := testRequest(t, app, http.MethodPost, "/api/rpc", body, map[string]string{
		"X-Actor-ID": "header-actor",
	})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out cmdrpc.ResponseEnvelope[map[string]any]
	decodeResponse(t, resp, &out)
	require.Nil(t, out.Error)
	assert.Equal(t, "Ada", out.Data["name"])
	assert.Equal(t, "header-actor", out.Data["actorId"])

	_, ok := srv.payload.(strictValueRequest)
	assert.True(t, ok, "payload type should remain a value type")
}

func TestMountFiber_EmptyBodyMethodFallbackAndValidation(t *testing.T) {
	adapter := router.NewFiberAdapter()
	r := adapter.Router()
	srv := cmdrpc.NewServer()

	err := srv.RegisterEndpoint(cmdrpc.NewEndpoint[struct{}, map[string]string](
		cmdrpc.EndpointSpec{
			Method: "example.ping",
			Kind:   cmdrpc.MethodKindQuery,
		},
		func(_ context.Context, _ cmdrpc.RequestEnvelope[struct{}]) (cmdrpc.ResponseEnvelope[map[string]string], error) {
			return cmdrpc.ResponseEnvelope[map[string]string]{
				Data: map[string]string{"ok": "pong"},
			}, nil
		},
	))
	require.NoError(t, err)
	require.NoError(t, rpcfiber.MountFiber(r, srv))

	app := adapter.WrappedRouter()

	resp := testRequest(t, app, http.MethodPost, "/api/rpc?method=example.ping", "", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out cmdrpc.ResponseEnvelope[map[string]string]
	decodeResponse(t, resp, &out)
	require.Nil(t, out.Error)
	assert.Equal(t, "pong", out.Data["ok"])

	resp = testRequest(t, app, http.MethodPost, "/api/rpc", "", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var missingMethod cmdrpc.ResponseEnvelope[any]
	decodeResponse(t, resp, &missingMethod)
	require.NotNil(t, missingMethod.Error)
	assert.Equal(t, "RPC_METHOD_REQUIRED", missingMethod.Error.Code)

	resp = testRequest(t, app, http.MethodPost, "/api/rpc?method=example.ping", "{", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var invalid cmdrpc.ResponseEnvelope[any]
	decodeResponse(t, resp, &invalid)
	require.NotNil(t, invalid.Error)
	assert.Equal(t, "RPC_INVALID_REQUEST", invalid.Error.Code)
}

func testRequest(
	t *testing.T,
	app interface {
		Test(req *http.Request, msTimeout ...int) (*http.Response, error)
	},
	method string,
	target string,
	body string,
	headers map[string]string,
) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func decodeResponse(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, target), string(raw))
}

func headerValue(headers map[string]string, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	for current, value := range headers {
		if strings.ToLower(current) == key {
			return value
		}
	}
	return ""
}
