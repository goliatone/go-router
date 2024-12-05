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

// HTTPServer implements Server for julienschmidt/httprouter
type HTTPServer struct {
	httpRouter *httprouter.Router
	server     *http.Server
	router     *HTTPRouter
}

func NewHTTPServer(opts ...func(*httprouter.Router) *httprouter.Router) Server[*httprouter.Router] {
	router := httprouter.New()

	if len(opts) == 0 {
		opts = append(opts, DefaultHTTPRouterOptions)
	}

	for _, opt := range opts {
		router = opt(router)
	}

	return &HTTPServer{httpRouter: router}
}

func DefaultHTTPRouterOptions(router *httprouter.Router) *httprouter.Router {
	router.HandleMethodNotAllowed = true
	router.HandleOPTIONS = true
	return router
}

func (a *HTTPServer) Router() Router[*httprouter.Router] {
	if a.router == nil {
		a.router = &HTTPRouter{router: a.httpRouter, Log: &defaultLogger{}}
	}

	return a.router
}

func (a *HTTPServer) WrapHandler(h HandlerFunc) interface{} {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		ctx := NewHTTPRouterContext(w, r, ps)
		if err := h(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (a *HTTPServer) WrappedRouter() *httprouter.Router {
	return a.httpRouter
}

func (a *HTTPServer) WrappedServer() *http.Server {
	return a.server
}

func (r *HTTPServer) Serve(address string) error {
	srv := &http.Server{
		Addr:    address,
		Handler: r.httpRouter,
	}
	r.server = srv

	debugPrint("serving at %s", srv.Addr)

	return srv.ListenAndServe()
}

func (r *HTTPServer) Shutdown(ctx context.Context) error {
	return r.server.Shutdown(ctx)
}

// HTTPRouter implements Router for httprouter
type HTTPRouter struct {
	router     *httprouter.Router
	prefix     string
	middleware []HandlerFunc
	Log        Logger
}

func (r *HTTPRouter) Handle(method HTTPMethod, path string, handlers ...HandlerFunc) RouteInfo {
	fullPath := r.prefix + path
	handler := r.buildHandler(handlers...)

	r.router.Handle(string(method), fullPath, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		ctx := context.WithValue(req.Context(), httprouter.ParamsKey, params)
		req = req.WithContext(ctx)
		handler.ServeHTTP(w, req)
	})

	return &HTTPRouteInfo{path: fullPath}
}

func (r *HTTPRouter) buildHandler(handlers ...HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		params := httprouter.ParamsFromContext(req.Context())
		if params == nil {
			params = make(httprouter.Params, 0)
		}

		ctx := &httpRouterContext{
			w:        w,
			r:        req,
			params:   params,
			handlers: append(r.middleware, handlers...),
			index:    -1,
		}

		if err := ctx.Next(); err != nil {
			r.Log.Error("Error during handler execution: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
	for _, m := range middleware {
		switch v := m.(type) {
		case HandlerFunc:
			r.middleware = append(r.middleware, v)
		case func(Context) error:
			r.middleware = append(r.middleware, HandlerFunc(v))
		case func(http.Handler) http.Handler:
			r.middleware = append(r.middleware, adaptStandardMiddleware(v))
		default:
			r.Log.Error("Warning: Unsupported middleware type: %T", m)
		}
	}
	return r
}

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

func (c *httpRouterContext) Status(code int) ResponseWriter {
	c.statusCode = code
	return c
}

func (c *httpRouterContext) Send(body []byte) error {
	if body == nil {
		return c.NoContent(c.statusCode)
	}
	if c.statusCode > 0 {
		c.w.WriteHeader(c.statusCode)
	}
	_, err := c.w.Write(body)
	return err
}

func (c *httpRouterContext) JSON(code int, v interface{}) error {
	if v == nil {
		return c.NoContent(code)
	}

	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(code)
	return json.NewEncoder(c.w).Encode(v)
}

// NoContent for status codes that shouldn't have response bodies (204, 205, 304).
func (c *httpRouterContext) NoContent(code int) error {
	c.w.WriteHeader(code)
	return nil
}

func (c *httpRouterContext) Bind(v interface{}) error {
	if v == nil {
		return fmt.Errorf("bind: nil interface provided")
	}
	if c.r.Body == nil {
		return fmt.Errorf("bind: request body is nil")
	}
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

func (c *httpRouterContext) SetHeader(key string, value string) ResponseWriter {
	c.w.Header().Set(key, value)
	return c
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
	keys := []string{"mykey"}
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
