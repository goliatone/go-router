package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/gofiber/utils"
	"github.com/julienschmidt/httprouter"
)

type HTTPServer struct {
	httpRouter        *httprouter.Router
	server            *http.Server
	router            *HTTPRouter
	views             Views
	passLocalsToViews bool
	initialized       bool
}

func NewHTTPServer(opts ...func(*httprouter.Router) *httprouter.Router) Server[*httprouter.Router] {
	router := httprouter.New()

	if len(opts) == 0 {
		opts = append(opts, DefaultHTTPRouterOptions)
	}

	for _, opt := range opts {
		router = opt(router)
	}

	// engine := django.New("./views", ".html")

	return &HTTPServer{
		httpRouter: router,
		// views:      engine,
	}
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
			BaseRouter: BaseRouter{
				logger:      &defaultLogger{},
				routes:      []*RouteDefinition{},
				middlewares: []namedMiddleware{},
				root:        &routerRoot{routes: []*RouteDefinition{}},
				views:       a.views,
			},
		}
	}
	return a.router
}

func (a *HTTPServer) WrapHandler(h HandlerFunc) any {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		c := newHTTPRouterContext(w, r, ps, a.views)
		c.router = a.router
		c.passLocalsToViews = a.passLocalsToViews
		if err := h(c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (a *HTTPServer) WrappedRouter() *httprouter.Router {
	return a.httpRouter
}

func (a *HTTPServer) Init() {
	if a.initialized {
		return
	}

	for _, route := range a.router.lateRoutes {
		a.router.Handle(
			route.method,
			route.path,
			route.handler,
			route.mw...,
		).SetName(route.name)
	}

	a.router.lateRoutes = make([]*lateRoute, 0)

	a.initialized = true
}

func (a *HTTPServer) Serve(address string) error {

	a.Init()

	if a.views != nil {
		if err := a.views.Load(); err != nil {
			return err
		}
	}

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
	BaseRouter
	router *httprouter.Router
}

func (r *HTTPRouter) Static(prefix, root string, config ...Static) Router[*httprouter.Router] {
	path, handler := r.makeStaticHandler(prefix, root, config...)
	r.addLateRoute(GET, path+"/*", handler, "static.get", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.get Next")
			return ctx.Next()
		}
	})
	r.addLateRoute(HEAD, path+"/*", handler, "static.head", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.head Next")
			return ctx.Next()
		}
	})
	return r
}

func (r *HTTPRouter) GetPrefix() string {
	return r.prefix
}

func (r *HTTPRouter) Group(prefix string) Router[*httprouter.Router] {
	return &HTTPRouter{
		router: r.router,
		BaseRouter: BaseRouter{
			prefix:            r.joinPath(r.prefix, prefix),
			middlewares:       append([]namedMiddleware{}, r.middlewares...),
			logger:            r.logger,
			routes:            r.routes,
			root:              r.root,
			views:             r.views,
			passLocalsToViews: r.passLocalsToViews,
		},
	}
}

func (r *HTTPRouter) Mount(prefix string) Router[*httprouter.Router] {

	return &HTTPRouter{
		router: r.router,
		BaseRouter: BaseRouter{
			prefix:            r.joinPath(r.prefix, prefix),
			middlewares:       append([]namedMiddleware{}, r.middlewares...),
			logger:            r.logger,
			routes:            r.routes,
			root:              r.root,
			views:             r.views,
			passLocalsToViews: r.passLocalsToViews,
		},
	}
}

func (r *HTTPRouter) WithGroup(path string, cb func(r Router[*httprouter.Router])) Router[*httprouter.Router] {
	g := r.Group(path)
	cb(g)
	return r
}

func (r *HTTPRouter) WithLogger(logger Logger) Router[*httprouter.Router] {
	r.logger = logger
	return r
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
	fullPath := r.joinPath(r.prefix, pathStr)
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
		ctx := newHTTPRouterContext(w, req, params, r.views)
		ctx.router = r
		ctx.passLocalsToViews = r.passLocalsToViews
		ctx.setHandlers(route.Handlers)

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

func (r *HTTPRouter) Head(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(HEAD, path, handler, mw...)
}

func (r *HTTPRouter) PrintRoutes() {
	r.BaseRouter.PrintRoutes()
}

// httpRouterContext implements Context for httprouter
type httpRouterContext struct {
	w                 http.ResponseWriter
	r                 *http.Request
	params            httprouter.Params
	handlers          []NamedHandler
	index             int
	store             ContextStore
	views             Views
	passLocalsToViews bool
	locals            ViewContext
	router            *HTTPRouter
	written           bool
}

func NewHTTPRouterContext(w http.ResponseWriter, r *http.Request, ps httprouter.Params, views Views) Context {
	return newHTTPRouterContext(w, r, ps, views)
}

func newHTTPRouterContext(w http.ResponseWriter, r *http.Request, ps httprouter.Params, views Views) *httpRouterContext {
	return &httpRouterContext{
		w:      w,
		r:      r,
		params: ps,
		index:  -1,
		store:  NewContextStore(),
		views:  views,
		locals: make(ViewContext),
	}
}

func (c *httpRouterContext) setHandlers(h []NamedHandler) {
	c.handlers = h
}

func (c *httpRouterContext) Locals(key any, value ...any) any {
	if len(value) == 0 {
		return c.locals[fmt.Sprint(key)]
	}

	c.locals[fmt.Sprint(key)] = value[0]
	return value[0]
}

func (c *httpRouterContext) Body() []byte {
	if c.r.ContentLength == 0 {
		return []byte{}
	}

	b, err := io.ReadAll(c.r.Body)
	if err != nil {
		return []byte{}
	}

	c.r.Body = io.NopCloser(bytes.NewBuffer(b))

	return b
}

func (c *httpRouterContext) Render(name string, bind any, layouts ...string) error {
	if c.views == nil {
		return fmt.Errorf("no template engine registered")
	}

	if bind == nil {
		bind = make(ViewContext)
	}

	data, err := SerializeAsContext(bind)
	if err != nil {
		return fmt.Errorf("render: error serializing vars: %w", err)
	}

	buf := new(bytes.Buffer)

	if c.views != nil {
		if err := c.views.Render(buf, name, data, layouts...); err != nil {
			return err
		}
	} else {
		// Render raw template using 'name' as filepath if no engine is set
		var tmpl *template.Template
		if _, err := readContent(buf, name); err != nil {
			return err
		}
		// Parse template
		tmpl, err := template.New("").Parse(string(buf.Bytes()))
		if err != nil {
			return fmt.Errorf("failed to parse: %w", err)
		}
		buf.Reset()
		if err := tmpl.Execute(buf, bind); err != nil {
			return fmt.Errorf("failed to execute: %w", err)
		}
	}

	c.SetHeader("Content-Type", "text/html; charset=utf-8")
	_, err = buf.WriteTo(c.w)

	return err
}

func readContent(rf io.ReaderFrom, name string) (int64, error) {
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return 0, fmt.Errorf("failed to open: %w", err)
	}
	if n, err := rf.ReadFrom(f); err != nil {
		return n, fmt.Errorf("failed to read: %w", err)
	}
	return 0, nil
}

func (c *httpRouterContext) Method() string {
	m := c.r.Method
	if m == "" {
		m = "GET"
	}
	return strings.ToUpper(m)
}

func (c *httpRouterContext) Path() string { return c.r.URL.Path }

func (c *httpRouterContext) Param(name string, defaultValue ...string) string {
	if out := c.params.ByName(name); out != "" {
		return out
	}

	if len(defaultValue) == 0 {
		return ""
	}

	return defaultValue[0]
}

func (c *httpRouterContext) Cookie(cookie *Cookie) {
	if cookie == nil {
		return
	}
	// Create a standard library cookie
	stdCookie := &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		Secure:   cookie.Secure,
		HttpOnly: cookie.HTTPOnly,
	}

	// Only set MaxAge and Expires if SessionOnly is false
	if !cookie.SessionOnly {
		if cookie.MaxAge > 0 {
			stdCookie.MaxAge = cookie.MaxAge
		}
		if !cookie.Expires.IsZero() {
			stdCookie.Expires = cookie.Expires
		}
	}

	// Handle SameSite string
	switch cookie.SameSite {
	case CookieSameSiteStrictMode:
		stdCookie.SameSite = http.SameSiteStrictMode
	case CookieSameSiteNoneMode:
		stdCookie.SameSite = http.SameSiteNoneMode
	case CookieSameSiteDisabled:
		stdCookie.SameSite = http.SameSiteDefaultMode
	default:
		stdCookie.SameSite = http.SameSiteLaxMode
	}

	http.SetCookie(c.w, stdCookie)
}

func (c *httpRouterContext) Cookies(key string, defaultValue ...string) string {
	cookie, err := c.r.Cookie(key)
	if err != nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return cookie.Value
}

// CookieParser is a simple implementation that collects all cookies into a map
// and then decodes them into 'out' via JSON. Adjust parsing logic as needed.
func (c *httpRouterContext) CookieParser(out any) error {
	if out == nil {
		return fmt.Errorf("CookieParser: out is nil")
	}

	// Gather all cookies into a map
	cookieMap := make(map[string]string)
	for _, cookie := range c.r.Cookies() {
		cookieMap[cookie.Name] = cookie.Value
	}

	// Marshal that map to JSON, then unmarshal into 'out'
	data, err := json.Marshal(cookieMap)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// Redirect sets the Location header and writes an HTTP redirect status code.
func (c *httpRouterContext) Redirect(location string, status ...int) error {
	code := http.StatusFound // default 302
	if len(status) > 0 {
		code = status[0]
	}
	http.Redirect(c.w, c.r, location, code)
	return nil
}

func (c *httpRouterContext) RedirectToRoute(routeName string, params ViewContext, status ...int) error {

	route := c.router.GetRoute(routeName)
	if route == nil {
		return fmt.Errorf("route not found: %s", routeName)
	}

	path := route.Path

	// replace :param placeholders
	for key, val := range params {
		if key == "queries" {
			continue
		}
		placeholder := ":" + key
		path = strings.ReplaceAll(path, placeholder, fmt.Sprintf("%v", val))
	}

	// handle "queries" as a map that becomes a query string
	if qs, ok := params["queries"].(map[string]string); ok {
		q := url.Values{}
		for k, v := range qs {
			q.Set(k, v)
		}
		queryString := q.Encode()
		if queryString != "" {
			path += "?" + queryString
		}
	}

	return c.Redirect(path, status...)
}

// RedirectBack attempts to redirect to the 'Referer' header, falling back
// to the given 'fallback' if the header is empty.
func (c *httpRouterContext) RedirectBack(fallback string, status ...int) error {
	referer := c.Header("Referer")
	if referer == "" {
		referer = fallback
	}
	return c.Redirect(referer, status...)
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

func (c *httpRouterContext) Query(name string, defaultValue ...string) string {
	if out := c.r.URL.Query().Get(name); out != "" {
		return out
	}
	def := ""
	if len(defaultValue) > 0 {
		def = defaultValue[0]
	}
	return def
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

func (c *httpRouterContext) Status(code int) Context {
	if code > 0 {
		c.w.WriteHeader(code)
	}
	return c
}

func (c *httpRouterContext) SendString(body string) error {
	return c.Send([]byte(body))
}

// SendStatus sets the HTTP status code and if the response body is empty,
// it sets the correct status message in the body.
func (c *httpRouterContext) SendStatus(status int) error {
	c.Status(status)

	// Only set status body when there is no response body
	if !c.written {
		return c.SendString(utils.StatusMessage(status))
	}

	return nil
}

func (c *httpRouterContext) Send(body []byte) error {
	if body == nil {
		return c.NoContent(http.StatusNoContent)
	}
	c.written = true
	_, err := c.w.Write(body)
	return err
}

func (c *httpRouterContext) JSON(code int, v any) error {
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

func (c *httpRouterContext) Bind(v any) error {
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

func (c *httpRouterContext) Referer() string {
	if referrer := c.Header("Referer"); referrer != "" {
		return referrer
	}
	return c.Header("Referrer")
}

func (c *httpRouterContext) OriginalURL() string {
	return c.r.RequestURI
}

func (c *httpRouterContext) SetHeader(key string, value string) Context {
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
	if c.index >= len(c.handlers) {
		return nil
	}
	return c.handlers[c.index].Handler(c)
}
