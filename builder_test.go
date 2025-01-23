package router

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteBuilder_BasicRoute(t *testing.T) {

	mockRouter := NewMockRouter()

	builder := NewRouteBuilder(mockRouter)

	handler := func(c Context) error {
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
		Responses([]Response{
			{
				Code:        http.StatusOK,
				Description: "successful response",
			},
			{
				Code:        http.StatusNotFound,
				Description: "not found",
			},
		})

	err := builder.BuildAll()
	require.NoError(t, err, "expected no error building routes")

	require.Len(t, mockRouter.GetRoutes(), 1)
	r := mockRouter.GetRoutes()[0]
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
	mockRouter := NewMockRouter()

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
	mockRouter := NewMockRouter()

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
	r := mockRouter.GetRoutes()[0]
	require.NotNil(t, r.Handler)

	// Build a context mock
	mockCtx := newMockContext()
	err = r.Handler(mockCtx)
	require.NoError(t, err)

	assert.Equal(t, []string{"mw1", "mw2", "handler"}, order)
}

func TestRouteBuilder_GroupRoutes(t *testing.T) {
	mockRouter := NewMockRouter()

	builder := NewRouteBuilder(mockRouter)

	// Create groups and routes at different levels
	handler := func(c Context) error {
		return c.JSON(http.StatusOK, map[string]string{"msg": "ok"})
	}

	// Root level route
	builder.NewRoute().
		GET().
		Path("/").
		Handler(handler).
		Name("root")

	// API group
	api := builder.Group("/api")
	api.NewRoute().
		POST().
		Path("/items").
		Handler(handler).
		Name("api.items.create")

	// Nested group under API
	v1 := api.Group("/v1")
	v1.NewRoute().
		GET().
		Path("/users").
		Handler(handler).
		Name("api.v1.users.list")

	// Another group at root level
	admin := builder.Group("/admin")
	admin.NewRoute().
		DELETE().
		Path("/users").
		Handler(handler).
		Name("admin.users.delete")

	err := builder.BuildAll()
	require.NoError(t, err)

	// Verify all routes were collected and have correct paths
	require.Len(t, mockRouter.GetRoutes(), 4)

	// Map routes by name for easier assertion
	routeMap := make(map[string]*MockRouteInfo)
	for _, r := range mockRouter.GetRoutes() {
		routeMap[r.NameVal] = r
	}

	// Check root route
	root := routeMap["root"]
	require.NotNil(t, root)
	assert.Equal(t, GET, root.Method)
	assert.Equal(t, "/", root.Path)

	// Check API route
	apiRoute := routeMap["api.items.create"]
	require.NotNil(t, apiRoute)
	assert.Equal(t, POST, apiRoute.Method)
	assert.Equal(t, "/api/items", apiRoute.Path)

	// Check nested V1 route
	v1Route := routeMap["api.v1.users.list"]
	require.NotNil(t, v1Route)
	assert.Equal(t, GET, v1Route.Method)
	assert.Equal(t, "/api/v1/users", v1Route.Path)

	// Check admin route
	adminRoute := routeMap["admin.users.delete"]
	require.NotNil(t, adminRoute)
	assert.Equal(t, DELETE, adminRoute.Method)
	assert.Equal(t, "/admin/users", adminRoute.Path)
}

func TestRouteBuilder_GroupMiddleware(t *testing.T) {
	mockRouter := NewMockRouter()

	builder := NewRouteBuilder(mockRouter)

	var order []string

	// Middleware for different levels
	rootMw := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "root")
			return next(c)
		}
	}

	apiMw := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "api")
			return next(c)
		}
	}

	v1Mw := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			order = append(order, "v1")
			return next(c)
		}
	}

	handler := func(c Context) error {
		order = append(order, "handler")
		return nil
	}

	// Add root middleware
	builder.NewRoute().
		GET().
		Path("/").
		Handler(handler).
		Middleware(rootMw)

	// Create nested groups with middleware
	api := builder.Group("/api")
	api.NewRoute().
		GET().
		Path("/").
		Handler(handler).
		Middleware(apiMw)

	v1 := api.Group("/v1")
	v1.NewRoute().
		GET().
		Path("/test").
		Handler(handler).
		Middleware(v1Mw)

	// Build routes
	err := builder.BuildAll()
	require.NoError(t, err)

	// Verify middleware execution order
	require.Len(t, mockRouter.GetRoutes(), 3)

	// Reset order slice before testing each route
	order = nil

	// Test root route
	rootRoute := mockRouter.GetRoutes()[0]
	mockCtx := newMockContext()
	err = rootRoute.Handler(mockCtx)
	require.NoError(t, err)
	assert.Equal(t, []string{"root", "handler"}, order)

	// Test api route
	order = nil
	apiRoute := mockRouter.GetRoutes()[1]
	err = apiRoute.Handler(mockCtx)
	require.NoError(t, err)
	assert.Equal(t, []string{"api", "handler"}, order)

	// Test v1 route which should have its middleware
	order = nil
	v1Route := mockRouter.GetRoutes()[2]
	err = v1Route.Handler(mockCtx)
	require.NoError(t, err)
	assert.Equal(t, []string{"v1", "handler"}, order)
}

func TestRouteBuilder_BuildFromDifferentLevels(t *testing.T) {
	mockRouter := NewMockRouter()

	builder := NewRouteBuilder(mockRouter)

	handler := func(c Context) error {
		return c.JSON(http.StatusOK, map[string]string{"msg": "ok"})
	}

	// Create nested structure
	api := builder.Group("/api")
	v1 := api.Group("/v1")

	// Add routes at different levels
	builder.NewRoute().GET().Path("/").Handler(handler).Name("root")
	api.NewRoute().GET().Path("/items").Handler(handler).Name("items")
	v1.NewRoute().GET().Path("/users").Handler(handler).Name("users")

	// Try building from different levels - should all work the same
	err := v1.BuildAll()
	require.NoError(t, err)
	require.Len(t, mockRouter.GetRoutes(), 3, "Building from v1 should include all routes")

	// Clear routes and try building from api level
	mockRouter.Clear()
	err = api.BuildAll()
	require.NoError(t, err)
	require.Len(t, mockRouter.GetRoutes(), 3, "Building from api should include all routes")

	// Clear routes and try building from root level
	mockRouter.Clear()
	err = builder.BuildAll()
	require.NoError(t, err)
	require.Len(t, mockRouter.GetRoutes(), 3, "Building from root should include all routes")

	// Verify paths are correct regardless of build point
	paths := make(map[string]bool)
	for _, r := range mockRouter.GetRoutes() {
		paths[r.Path] = true
	}

	assert.True(t, paths["/"])
	assert.True(t, paths["/api/items"])
	assert.True(t, paths["/api/v1/users"])
}

// /////
// TODO: use gomock and mockgen -source=router.go -destination=mocks_test.go -package=router_test
// MockRouter is a mock implementation of Router[T] for testing.
type MockRouter struct {
	rootRouter *MockRouter // Reference to root router
	routes     []*MockRouteInfo
	Prefix     string
	Mw         []MiddlewareFunc
}

func NewMockRouter() *MockRouter {
	m := &MockRouter{
		routes: make([]*MockRouteInfo, 0),
		Mw:     make([]MiddlewareFunc, 0),
	}
	m.rootRouter = m // Root router points to itself
	return m
}

func (m *MockRouter) Handle(method HTTPMethod, path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	allMw := append(m.Mw, mw...)
	r := &MockRouteInfo{
		Method:     method,
		Path:       m.Prefix + path,
		Handler:    handler,
		Middleware: allMw,
	}
	// Always add to root router's routes
	m.rootRouter.routes = append(m.rootRouter.routes, r)
	return r
}

func (m *MockRouter) Group(prefix string) Router[*MockRouter] {
	return &MockRouter{
		rootRouter: m.rootRouter, // Pass reference to root router
		Prefix:     m.Prefix + prefix,
		Mw:         append([]MiddlewareFunc{}, m.Mw...),
		routes:     m.rootRouter.routes, // Share root's routes slice
	}
}

func (m *MockRouter) WithGroup(path string, cb func(r Router[*MockRouter])) Router[*MockRouter] {
	g := m.Group(path)
	cb(g)
	return m
}

func (m *MockRouter) Clear() {
	m.rootRouter.routes = m.rootRouter.routes[:0]
}

func (m *MockRouter) GetRoutes() []*MockRouteInfo {
	return m.rootRouter.routes
}

func (m *MockRouter) Routes() []RouteDefinition {
	return []RouteDefinition{}
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
	SummaryVal     string
	TagsVal        []string
	ResponsesVal   map[int]string
}

func (r *MockRouteInfo) SetName(n string) RouteInfo {
	r.NameVal = n
	return r
}

func (r *MockRouteInfo) SetDescription(d string) RouteInfo {
	r.DescriptionVal = d
	return r
}

func (r *MockRouteInfo) SetSummary(d string) RouteInfo {
	r.SummaryVal = d
	return r
}

func (r *MockRouteInfo) AddTags(t ...string) RouteInfo {
	r.TagsVal = append(r.TagsVal, t...)
	return r
}

func (r *MockRouteInfo) AddParameter(name, in string, required bool, schema map[string]any) RouteInfo {
	return r
}

func (r *MockRouteInfo) SetRequestBody(desc string, required bool, content map[string]any) RouteInfo {
	return r
}

func (r *MockRouteInfo) AddResponse(code int, desc string, content map[string]any) RouteInfo {
	if r.ResponsesVal == nil {
		r.ResponsesVal = make(map[int]string)
	}
	r.ResponsesVal[code] = desc
	return r
}

// Example of a minimal mock context just for testing handler calls
type mockContext struct {
	store map[string]any
}

func newMockContext() *mockContext {
	return &mockContext{store: make(map[string]any)}
}

func (m *mockContext) Method() string                     { return "GET" }
func (m *mockContext) Path() string                       { return "/test" }
func (m *mockContext) Param(name, def string) string      { return "" }
func (m *mockContext) ParamsInt(name string, def int) int { return 0 }
func (m *mockContext) Query(name, def string) string      { return "" }
func (m *mockContext) QueryInt(name string, def int) int  { return 0 }
func (m *mockContext) Queries() map[string]string         { return map[string]string{} }
func (m *mockContext) Status(code int) ResponseWriter     { return m }
func (m *mockContext) Send(body []byte) error             { return nil }
func (m *mockContext) JSON(code int, v any) error         { return nil }
func (m *mockContext) NoContent(code int) error           { return nil }
func (m *mockContext) Bind(v any) error                   { return nil }
func (m *mockContext) Body() []byte                       { return nil }
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
