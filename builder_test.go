package router

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TODO: use gomock and mockgen -source=router.go -destination=mocks_test.go -package=router_test
// MockRouter is a mock implementation of Router[T] for testing.
type MockRouter struct {
	Routes []*MockRouteInfo
	Prefix string
	Mw     []MiddlewareFunc
}

func (m *MockRouter) Handle(method HTTPMethod, path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	// Combine middleware from the router and the route call
	allMw := append(m.Mw, mw...)
	r := &MockRouteInfo{
		Method:     method,
		Path:       m.Prefix + path,
		Handler:    handler,
		Middleware: allMw,
	}
	m.Routes = append(m.Routes, r)
	return r
}

func (m *MockRouter) Group(prefix string) Router[*MockRouter] {
	return &MockRouter{
		Prefix: m.Prefix + prefix,
		Mw:     m.Mw,
	}
}

func (m *MockRouter) Use(mw ...MiddlewareFunc) Router[*MockRouter] {
	m.Mw = append(m.Mw, mw...)
	return m
}

func (m *MockRouter) Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return m.Handle(GET, path, handler, mw...)
}
func (m *MockRouter) Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return m.Handle(POST, path, handler, mw...)
}
func (m *MockRouter) Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return m.Handle(PUT, path, handler, mw...)
}
func (m *MockRouter) Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return m.Handle(DELETE, path, handler, mw...)
}
func (m *MockRouter) Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return m.Handle(PATCH, path, handler, mw...)
}

func (m *MockRouter) PrintRoutes() {
	// No-op for testing
}

// MockRouteInfo implements RouteInfo and stores metadata for verification.
type MockRouteInfo struct {
	Method     HTTPMethod
	Path       string
	Handler    HandlerFunc
	Middleware []MiddlewareFunc

	NameVal        string
	DescriptionVal string
	TagsVal        []string
	ResponsesVal   map[int]string
}

func (r *MockRouteInfo) Name(n string) RouteInfo {
	r.NameVal = n
	return r
}

func (r *MockRouteInfo) Description(d string) RouteInfo {
	r.DescriptionVal = d
	return r
}

func (r *MockRouteInfo) Tags(t ...string) RouteInfo {
	r.TagsVal = append(r.TagsVal, t...)
	return r
}

func (r *MockRouteInfo) Responses(resp map[int]string) RouteInfo {
	if r.ResponsesVal == nil {
		r.ResponsesVal = make(map[int]string)
	}
	for k, v := range resp {
		r.ResponsesVal[k] = v
	}
	return r
}

func TestRouteBuilder_BasicRoute(t *testing.T) {

	mockRouter := &MockRouter{}
	builder := NewRouteBuilder(mockRouter)

	// wasCalled := false
	handler := func(c Context) error {
		// wasCalled = true
		return c.JSON(http.StatusOK, map[string]string{"msg": "hello"})
	}

	builder.
		NewRoute().
		Method(GET).
		Path("/hello").
		Handler(handler).
		Name("hello_route").
		Description("Returns a friendly greeting").
		Tags("greetings", "public").
		Responses(map[int]string{
			http.StatusOK:       "successful response",
			http.StatusNotFound: "not found",
		})

	err := builder.BuildAll()
	require.NoError(t, err, "expected no error building routes")

	require.Len(t, mockRouter.Routes, 1)
	r := mockRouter.Routes[0]
	assert.Equal(t, GET, r.Method)
	assert.Equal(t, "/hello", r.Path)
	assert.Equal(t, "hello_route", r.NameVal)
	assert.Equal(t, "Returns a friendly greeting", r.DescriptionVal)
	assert.Contains(t, r.TagsVal, "greetings")
	assert.Contains(t, r.TagsVal, "public")
	assert.Equal(t, "successful response", r.ResponsesVal[http.StatusOK])
	assert.Equal(t, "not found", r.ResponsesVal[http.StatusNotFound])
}

func TestRouteBuilder_ValidationErrors(t *testing.T) {
	mockRouter := &MockRouter{}
	builder := NewRouteBuilder(mockRouter)

	// Missing method and path, should cause validation error
	builder.NewRoute().Handler(func(c Context) error {
		return nil
	})

	err := builder.BuildAll()
	require.Error(t, err, "expected error due to missing method/path")

	assert.Contains(t, err.Error(), "method is required")
}

func TestRouteBuilder_MiddlewareChain(t *testing.T) {
	mockRouter := &MockRouter{}
	builder := NewRouteBuilder(mockRouter)

	var order []string

	mw1 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "mw1")
			return next(c)
		}
	}

	mw2 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "mw2")
			return next(c)
		}
	}

	handler := func(c Context) error {
		order = append(order, "handler")
		return nil
	}

	builder.
		NewRoute().
		GET().
		Path("/chain").
		Handler(handler).
		Middleware(mw1, mw2)

	err := builder.BuildAll()
	require.NoError(t, err)

	// Simulate a request
	r := mockRouter.Routes[0]
	require.NotNil(t, r.Handler)

	// Build a context mock
	mockCtx := newMockContext()
	err = r.Handler(mockCtx)
	require.NoError(t, err)

	assert.Equal(t, []string{"mw1", "mw2", "handler"}, order)
}

// Example of a minimal mock context just for testing handler calls
type mockContext struct {
	store map[string]any
}

func newMockContext() *mockContext {
	return &mockContext{store: make(map[string]any)}
}

func (m *mockContext) Method() string                 { return "GET" }
func (m *mockContext) Path() string                   { return "/test" }
func (m *mockContext) Param(name string) string       { return "" }
func (m *mockContext) Query(name string) string       { return "" }
func (m *mockContext) Queries() map[string]string     { return map[string]string{} }
func (m *mockContext) Status(code int) ResponseWriter { return m }
func (m *mockContext) Send(body []byte) error         { return nil }
func (m *mockContext) JSON(code int, v any) error     { return nil }
func (m *mockContext) NoContent(code int) error       { return nil }
func (m *mockContext) Bind(v any) error               { return nil }
func (m *mockContext) Context() context.Context {
	// Return a non-nil context. You can return context.Background() or context.TODO() for tests.
	return context.Background()
}
func (m *mockContext) SetContext(ctx context.Context) {
	// Optionally store the context if needed, or just ignore for tests.
}
func (m *mockContext) Header(key string) string                          { return "" }
func (m *mockContext) SetHeader(key string, value string) ResponseWriter { return m }
func (m *mockContext) Next() error                                       { return errors.New("not implemented") }
func (m *mockContext) Set(k string, v any)                               { m.store[k] = v }
func (m *mockContext) Get(k string, def any) any {
	val, ok := m.store[k]
	if !ok {
		return def
	}
	return val
}
func (m *mockContext) GetString(k string, def string) string {
	val := m.Get(k, def)
	if s, ok := val.(string); ok {
		return s
	}
	return def
}
func (m *mockContext) GetInt(k string, def int) int {
	val := m.Get(k, def)
	if i, ok := val.(int); ok {
		return i
	}
	return def
}
func (m *mockContext) GetBool(k string, def bool) bool {
	val := m.Get(k, def)
	if b, ok := val.(bool); ok {
		return b
	}
	return def
}
