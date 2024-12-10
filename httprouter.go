package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strconv"

	"github.com/julienschmidt/httprouter"
)

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
		a.router = &HTTPRouter{
			router: a.httpRouter,
			baseRouter: baseRouter{
				logger:      &defaultLogger{},
				routes:      []*RouteDefinition{},
				middlewares: []namedMiddleware{},
				root:        &routerRoot{routes: []*RouteDefinition{}},
			},
		}
	}
	return a.router
}

func (a *HTTPServer) WrapHandler(h HandlerFunc) interface{} {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		c := NewHTTPRouterContext(w, r, ps)
		if err := h(c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (a *HTTPServer) WrappedRouter() *httprouter.Router {
	return a.httpRouter
}

func (a *HTTPServer) Serve(address string) error {
	srv := &http.Server{
		Addr:    address,
		Handler: a.httpRouter,
	}
	a.server = srv
	return srv.ListenAndServe()
}

func (a *HTTPServer) Shutdown(ctx context.Context) error {
	if a.server != nil {
		return a.server.Shutdown(ctx)
	}
	return nil
}

// HTTPRouter implements Router for httprouter
type HTTPRouter struct {
	baseRouter
	router *httprouter.Router
}

func (r *HTTPRouter) Group(prefix string) Router[*httprouter.Router] {
	return &HTTPRouter{
		router: r.router,
		baseRouter: baseRouter{
			prefix:      path.Join(r.prefix, prefix),
			middlewares: append([]namedMiddleware{}, r.middlewares...),
			logger:      r.logger,
			routes:      r.routes,
			root:        r.root,
		},
	}
}

func (r *HTTPRouter) Use(m ...MiddlewareFunc) Router[*httprouter.Router] {
	for _, mw := range m {
		r.middlewares = append(r.middlewares, namedMiddleware{
			Name: funcName(mw),
			Mw:   mw,
		})
	}
	return r
}

func (r *HTTPRouter) Handle(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
	fullPath := r.prefix + pathStr

	// Check for duplicates, since the behavior between fiber and httprouter differs
	// we need to decide how to handle this case...
	for _, route := range r.root.routes {
		if route.Method == method && route.Path == fullPath {
			// Decide how to handle duplicates:
			// return a RouterError or just log and skip
			panic(fmt.Sprintf("duplicate route %s %s already registered", method, pathStr))
		}
	}

	allMw := append([]namedMiddleware{}, r.middlewares...)
	for _, mw := range m {
		allMw = append(allMw, namedMiddleware{
			Name: funcName(mw),
			Mw:   mw,
		})
	}

	route := r.addRoute(method, fullPath, handler, "", allMw)

	// Register final handler with httprouter
	r.router.Handle(string(method), fullPath, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		ctx := NewHTTPRouterContext(w, req, params)
		if rh, ok := ctx.(*httpRouterContext); ok {
			rh.setHandlers(route.Handlers)
		}

		if err := ctx.Next(); err != nil {
			r.logger.Error("handler chain error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	return route
}

func (r *HTTPRouter) Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(GET, path, handler, mw...)
}
func (r *HTTPRouter) Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(POST, path, handler, mw...)
}
func (r *HTTPRouter) Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(PUT, path, handler, mw...)
}
func (r *HTTPRouter) Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(DELETE, path, handler, mw...)
}
func (r *HTTPRouter) Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(PATCH, path, handler, mw...)
}

func (r *HTTPRouter) PrintRoutes() {
	r.baseRouter.PrintRoutes()
}

// httpRouterContext implements Context for httprouter
type httpRouterContext struct {
	w        http.ResponseWriter
	r        *http.Request
	params   httprouter.Params
	handlers []NamedHandler
	index    int
	store    ContextStore
}

func NewHTTPRouterContext(w http.ResponseWriter, r *http.Request, ps httprouter.Params) Context {
	return &httpRouterContext{
		w:      w,
		r:      r,
		params: ps,
		index:  -1,
		store:  NewContextStore(),
	}
}

func (c *httpRouterContext) setHandlers(h []NamedHandler) {
	c.handlers = h
}

func (c *httpRouterContext) Method() string { return c.r.Method }
func (c *httpRouterContext) Path() string   { return c.r.URL.Path }

func (c *httpRouterContext) Param(name, defaultValue string) string {
	if out := c.params.ByName(name); out != "" {
		return out
	}
	return defaultValue
}

func (c *httpRouterContext) ParamsInt(name string, defaultValue int) int {
	p := ""
	if p = c.Param(name, ""); p == "" {
		return defaultValue
	}

	v, err := strconv.ParseInt(p, 0, 64)
	if err != nil {
		return defaultValue
	}

	return int(v)
}

func (c *httpRouterContext) Query(name, defaultValue string) string {
	if out := c.r.URL.Query().Get(name); out != "" {
		return out
	}
	return defaultValue
}

func (c *httpRouterContext) QueryInt(name string, defaultValue int) int {
	q := ""
	if q = c.r.URL.Query().Get(name); q == "" {
		return defaultValue
	}

	v, err := strconv.ParseInt(q, 0, 64)
	if err != nil {
		return defaultValue
	}

	return int(v)
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
	if code > 0 {
		c.w.WriteHeader(code)
	}
	return c
}
func (c *httpRouterContext) Send(body []byte) error {
	if body == nil {
		return c.NoContent(http.StatusNoContent)
	}
	_, err := c.w.Write(body)
	return err
}

func (c *httpRouterContext) JSON(code int, v interface{}) error {
	c.w.Header().Set("Content-Type", "application/json")
	c.w.WriteHeader(code)
	if v == nil {
		return nil
	}
	return json.NewEncoder(c.w).Encode(v)
}

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
	c.r = c.r.WithContext(ctx)
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

func (c *httpRouterContext) Set(key string, value any) {
	c.store.Set(key, value)
}

func (c *httpRouterContext) Get(key string, def any) any {
	return c.store.Get(key, def)
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

func (c *httpRouterContext) Next() error {
	c.index++
	if c.index < len(c.handlers) {
		return c.handlers[c.index].Handler(c)
	}
	return nil
}
