package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/julienschmidt/httprouter"
)

type contextKey struct {
	name string
}

var debugEnabled = true

func debugPrint(format string, args ...interface{}) {
	if debugEnabled {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// HTTPRouterAdapter implements RouterAdapter for julienschmidt/httprouter
type HTTPRouterAdapter struct {
	router *httprouter.Router
	server *http.Server
}

func NewHTTPRouterAdapter(opts ...func(*httprouter.Router) *httprouter.Router) RouterAdapter[*httprouter.Router] {
	router := httprouter.New()

	if len(opts) == 0 {
		opts = append(opts, DefaultHTTPRouterOptions)
	}

	for _, opt := range opts {
		router = opt(router)
	}

	return &HTTPRouterAdapter{router: router}
}

func DefaultHTTPRouterOptions(router *httprouter.Router) *httprouter.Router {
	router.HandleMethodNotAllowed = true
	router.HandleOPTIONS = true
	return router
}

func (a *HTTPRouterAdapter) NewRouter() Router[*httprouter.Router] {
	return &HTTPRouter{router: a.router}
}

func (a *HTTPRouterAdapter) WrapHandler(h HandlerFunc) interface{} {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		ctx := NewHTTPRouterContext(w, r, ps)
		if err := h(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (a *HTTPRouterAdapter) WrappedRouter() *httprouter.Router {
	return a.router
}

func (r *HTTPRouterAdapter) Serve(address string) error {
	srv := &http.Server{
		Addr:    address,
		Handler: r.router,
	}
	r.server = srv
	return srv.ListenAndServe()
}

func (r *HTTPRouterAdapter) Shutdown(ctx context.Context) error {
	return r.server.Shutdown(ctx)
}

// HTTPRouter implements Router for httprouter
type HTTPRouter struct {
	router     *httprouter.Router
	prefix     string
	middleware []HandlerFunc
}

func (r *HTTPRouter) Handle(method HTTPMethod, path string, handlers ...HandlerFunc) RouteInfo {
	fullPath := r.prefix + path
	handler := r.buildHandler(handlers...)

	debugPrint("Registering handler for %s %s", method, fullPath)

	r.router.Handle(string(method), fullPath, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		debugPrint("Router handling request for %s %s", method, fullPath)

		ctx := context.WithValue(req.Context(), httprouter.ParamsKey, params)
		req = req.WithContext(ctx)
		handler.ServeHTTP(w, req)
	})

	return &HTTPRouteInfo{path: fullPath}
}

func (r *HTTPRouter) buildHandler(handlers ...HandlerFunc) http.Handler {
	allHandlers := append(append([]HandlerFunc{}, r.middleware...), handlers...)

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		debugPrint("Starting new request handling for path: %s", req.URL.Path)
		params := httprouter.ParamsFromContext(req.Context())

		ctx := &httpRouterContext{
			w:        w,
			r:        req,
			params:   params,
			handlers: allHandlers,
			index:    -1,
		}

		debugPrint("Initial context values: %+v", contextToString(ctx.r.Context()))

		if err := ctx.Next(); err != nil {
			debugPrint("Error during handler execution: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		debugPrint("Request handling completed. Final context values: %+v",
			contextToString(ctx.r.Context()))
	})
}

func (r *HTTPRouter) Group(prefix string) Router[*httprouter.Router] {
	return &HTTPRouter{
		router:     r.router,
		middleware: r.middleware,
		prefix:     path.Join(r.prefix, prefix),
	}
}

func adaptStandardMiddleware(next func(http.Handler) http.Handler) HandlerFunc {
	return func(c Context) error {
		debugPrint("Executing adapted standard middleware")
		handler := next(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			debugPrint("Standard middleware inner handler")
			c.Next()
		}))
		handler.ServeHTTP(c.(*httpRouterContext).w, c.(*httpRouterContext).r)
		return nil
	}
}

func (r *HTTPRouter) Use(middleware ...any) Router[*httprouter.Router] {
	debugPrint("Adding middleware. Current count: %d", len(r.middleware))

	for _, m := range middleware {
		switch v := m.(type) {
		case HandlerFunc:
			debugPrint("Adding HandlerFunc middleware")
			r.middleware = append(r.middleware, v)
		case func(Context) error:
			debugPrint("Adding func(Context) error middleware")
			r.middleware = append(r.middleware, HandlerFunc(v))
		case func(http.Handler) http.Handler:
			debugPrint("Adding standard http middleware")
			r.middleware = append(r.middleware, adaptStandardMiddleware(v))
		default:
			debugPrint("Warning: Unsupported middleware type: %T", m)
		}
	}

	debugPrint("Total middleware count: %d", len(r.middleware))
	return r
}

// func (r *HTTPRouter) Serve(address string) error {
// 	return http.ListenAndServe(address, r.router)
// }

func (r *HTTPRouter) Get(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(GET, path, handler)
}

func (r *HTTPRouter) Post(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(POST, path, handler)
}

func (r *HTTPRouter) Put(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(PUT, path, handler)
}

func (r *HTTPRouter) Delete(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(DELETE, path, handler)
}

func (r *HTTPRouter) Patch(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(PATCH, path, handler)
}

// HTTPRouterContext implementation
type httpRouterContext struct {
	w          http.ResponseWriter
	r          *http.Request
	params     httprouter.Params
	handlers   []HandlerFunc
	index      int
	statusCode int
}

func NewHTTPRouterContext(w http.ResponseWriter, r *http.Request, ps httprouter.Params) Context {
	return &httpRouterContext{
		w:        w,
		r:        r,
		params:   ps,
		handlers: make([]HandlerFunc, 0),
		index:    -1,
	}
}

func (c *httpRouterContext) Method() string { return c.r.Method }
func (c *httpRouterContext) Path() string   { return c.r.URL.Path }
func (c *httpRouterContext) Param(name string) string {
	return c.params.ByName(name)
}

func (c *httpRouterContext) Query(name string) string {
	return c.r.URL.Query().Get(name)
}

func (c *httpRouterContext) Queries() map[string]string {
	queries := make(map[string]string)
	for k, v := range c.r.URL.Query() {
		if len(v) > 0 {
			queries[k] = v[0]
		}
	}
	return queries
}

func (c *httpRouterContext) Status(code int) Context {
	c.statusCode = code
	return c
}

func (c *httpRouterContext) Send(body []byte) error {
	if c.statusCode > 0 {
		c.w.WriteHeader(c.statusCode)
	}
	_, err := c.w.Write(body)
	return err
}

func (c *httpRouterContext) JSON(code int, v interface{}) error {
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(code)
	return json.NewEncoder(c.w).Encode(v)
}

func (c *httpRouterContext) NoContent(code int) error {
	c.w.WriteHeader(code)
	return nil
}

func (c *httpRouterContext) Bind(v interface{}) error {
	return json.NewDecoder(c.r.Body).Decode(v)
}

func (c *httpRouterContext) SetContext(ctx context.Context) {
	debugPrint("Setting new context. Old values: %+v", contextToString(c.r.Context()))
	c.r = c.r.WithContext(ctx)
	debugPrint("New context values: %+v", contextToString(c.r.Context()))
}

func (c *httpRouterContext) Context() context.Context {
	return c.r.Context()
}

func (c *httpRouterContext) Header(key string) string {
	return c.r.Header.Get(key)
}

func (c *httpRouterContext) SetHeader(key string, value string) {
	c.w.Header().Set(key, value)
}

func (c *httpRouterContext) Next() error {
	c.index++
	debugPrint("Executing handler at index: %d (total handlers: %d)", c.index, len(c.handlers))
	if c.index < len(c.handlers) {
		debugPrint("Context values before handler %d: %+v", c.index, contextToString(c.r.Context()))
		return c.handlers[c.index](c)
	}
	return nil
}

type HTTPRouteInfo struct {
	path string
	name string
}

func (r *HTTPRouteInfo) Name(name string) RouteInfo {
	r.name = name
	return r
}

func contextToString(ctx context.Context) string {
	var values []string
	// Add known keys to check
	keys := []string{"mykey"} // Add other known keys here
	for _, key := range keys {
		if val := ctx.Value(key); val != nil {
			values = append(values, fmt.Sprintf("%s=%v", key, val))
		}
	}
	return strings.Join(values, ", ")
}

func CreateContextMiddleware(key, value string) HandlerFunc {
	return func(c Context) error {
		debugPrint("Executing context middleware for key: %s", key)
		debugPrint("Before setting context value: %+v", contextToString(c.Context()))

		newCtx := context.WithValue(c.Context(), key, value)
		c.SetContext(newCtx)

		debugPrint("After setting context value: %+v", contextToString(c.Context()))
		return c.Next()
	}
}
