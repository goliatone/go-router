package router

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"sort"

	"github.com/julienschmidt/httprouter"
)

// HTTPServer implements Server for julienschmidt/httprouter
type HTTPServer struct {
	httpRouter   *httprouter.Router
	server       *http.Server
	router       *HTTPRouter
	errorHandler ErrorHandler
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

func (s *HTTPServer) Router() Router[*httprouter.Router] {
	if s.router == nil {
		s.router = &HTTPRouter{
			router:       s.httpRouter,
			errorHandler: s.errorHandler,
		}
	}
	return s.router
}

func (s *HTTPServer) WrapHandler(h HandlerFunc) any {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		ctx := NewHTTPRouterContext(w, r, ps)
		if err := h(ctx); err != nil && s.errorHandler != nil {
			s.errorHandler(ctx, err)
		}
	}
}

func (s *HTTPServer) WrappedRouter() *httprouter.Router {
	return s.httpRouter
}

func (s *HTTPServer) Serve(address string) error {
	srv := &http.Server{
		Addr:    address,
		Handler: s.httpRouter,
	}
	s.server = srv
	return srv.ListenAndServe()
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *HTTPServer) SetErrorHandler(handler ErrorHandler) {
	s.errorHandler = handler
	if s.router != nil {
		s.router.errorHandler = handler
	}
}

type HTTPRouter struct {
	router      *httprouter.Router
	prefix      string
	middlewares []struct {
		priority int
		handler  MiddlewareFunc
	}
	errorHandler ErrorHandler
}

func (r *HTTPRouter) Handle(method HTTPMethod, path string, handlers ...HandlerFunc) RouteInfo {
	fullPath := r.prefix + path
	handler := r.buildChain(handlers...)

	r.router.Handle(string(method), fullPath, func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		ctx := NewHTTPRouterContext(w, req, ps)
		if err := handler(ctx); err != nil && r.errorHandler != nil {
			r.errorHandler(ctx, err)
		}
	})

	return &HTTPRouteInfo{path: fullPath}
}

func (r *HTTPRouter) buildChain(handlers ...HandlerFunc) HandlerFunc {
	sort.Slice(r.middlewares, func(i, j int) bool {
		return r.middlewares[i].priority > r.middlewares[j].priority
	})

	var final HandlerFunc
	if len(handlers) > 0 {
		final = handlers[0]
	} else {
		final = func(c Context) error { return nil }
	}

	for i := len(r.middlewares) - 1; i >= 0; i-- {
		final = r.middlewares[i].handler(final)
	}

	return final
}

func (r *HTTPRouter) Group(prefix string) Router[*httprouter.Router] {
	return &HTTPRouter{
		router:       r.router,
		prefix:       path.Join(r.prefix, prefix),
		middlewares:  r.middlewares,
		errorHandler: r.errorHandler,
	}
}

func (r *HTTPRouter) Use(middleware ...MiddlewareFunc) Router[*httprouter.Router] {
	return r.UseWithPriority(0, middleware...)
}

func (r *HTTPRouter) UseWithPriority(priority int, middleware ...MiddlewareFunc) Router[*httprouter.Router] {
	for _, m := range middleware {
		r.middlewares = append(r.middlewares, struct {
			priority int
			handler  MiddlewareFunc
		}{priority, m})
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

type httpRouterContext struct {
	w        http.ResponseWriter
	r        *http.Request
	params   httprouter.Params
	store    ContextStore
	aborted  bool
	handlers []HandlerFunc
	index    int
}

func NewHTTPRouterContext(w http.ResponseWriter, r *http.Request, ps httprouter.Params) Context {
	return &httpRouterContext{
		w:      w,
		r:      r,
		params: ps,
		store:  NewContextStore(),
		index:  -1,
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
	c.w.WriteHeader(code)
	return c
}

func (c *httpRouterContext) Send(body []byte) error {
	_, err := c.w.Write(body)
	return err
}

func (c *httpRouterContext) JSON(code int, v any) error {
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(code)
	return json.NewEncoder(c.w).Encode(v)
}

func (c *httpRouterContext) NoContent(code int) error {
	c.w.WriteHeader(code)
	return nil
}

func (c *httpRouterContext) Bind(v any) error {
	return json.NewDecoder(c.r.Body).Decode(v)
}

func (c *httpRouterContext) Context() context.Context {
	return c.r.Context()
}

func (c *httpRouterContext) SetContext(ctx context.Context) {
	c.r = c.r.WithContext(ctx)
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
	if c.index < len(c.handlers) {
		return c.handlers[c.index](c)
	}
	return nil
}

func (c *httpRouterContext) Set(key string, value any) {
	c.store.Set(key, value)
}

func (c *httpRouterContext) Get(key string) any {
	return c.store.Get(key)
}

func (c *httpRouterContext) GetString(key string, def string) string {
	return c.store.GetString(key, def)
}

func (c *httpRouterContext) GetInt(key string, def int) int {
	return c.store.GetInt(key, def)
}

func (c *httpRouterContext) GetBool(key string, def bool) bool {
	return c.store.GetBool(key, def)
}

func (c *httpRouterContext) IsAborted() bool {
	return c.aborted
}

func (c *httpRouterContext) Abort() {
	c.aborted = true
}

type HTTPRouteInfo struct {
	path string
	name string
}

func (r *HTTPRouteInfo) Name(name string) RouteInfo {
	r.name = name
	return r
}
