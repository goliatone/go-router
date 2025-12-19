package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net"
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
	errorHandler      func(Context, error) error
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
		httpRouter:   router,
		errorHandler: DefaultHTTPErrorHandler(DefaultHTTPErrorHandlerConfig()),
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
		if a.errorHandler == nil {
			a.errorHandler = DefaultHTTPErrorHandler(DefaultHTTPErrorHandlerConfig())
		}
		a.router = &HTTPRouter{
			router:       a.httpRouter,
			errorHandler: a.errorHandler,
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

		// Get path pattern from request and inject route context
		if pathPattern := a.getRoutePattern(r.Method, r.URL.Path); pathPattern != "" {
			if routeName, ok := a.router.RouteNameFromPath(r.Method, pathPattern); ok {
				goCtx := c.Context()
				goCtx = WithRouteName(goCtx, routeName)

				// Convert httprouter.Params to map[string]string
				params := make(map[string]string, len(ps))
				for _, p := range ps {
					params[p.Key] = p.Value
				}
				goCtx = WithRouteParams(goCtx, params)
				c.SetContext(goCtx)
			}
		}

		if err := h(c); err != nil {
			if a.errorHandler == nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if handleErr := a.errorHandler(c, err); handleErr != nil {
				if a.router != nil && a.router.logger != nil {
					a.router.logger.Error("error handler failed: %v", handleErr)
				}
			}
		}
	}
}

func (a *HTTPServer) WrappedRouter() *httprouter.Router {
	a.Init()
	return a.httpRouter
}

// getRoutePattern tries to find the route pattern for a given method and path
// by looking up the registered routes in the BaseRouter
func (a *HTTPServer) getRoutePattern(method, path string) string {
	// Try to find matching route pattern by iterating through registered routes
	for _, route := range a.router.root.routes {
		if string(route.Method) == method {
			// For exact matches (static routes)
			if route.Path == path {
				return route.Path
			}

			// For parameterized routes, we need to check if the pattern would match
			// This is a simple heuristic - we can improve it if needed
			if strings.Contains(route.Path, ":") || strings.Contains(route.Path, "*") {
				// Use httprouter's lookup to see if this path would match
				if handler, _, _ := a.httpRouter.Lookup(method, path); handler != nil {
					// If httprouter found a match, check if our route pattern segments match
					if pathMatchesPattern(route.Path, path) {
						return route.Path
					}
				}
			}
		}
	}
	return ""
}

// pathMatchesPattern checks if a request path could match a route pattern
func pathMatchesPattern(pattern, path string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	// Handle wildcard case - wildcard can match remaining path segments
	hasWildcard := false
	wildcardIndex := -1
	for i, part := range patternParts {
		if strings.Contains(part, "*") {
			hasWildcard = true
			wildcardIndex = i
			break
		}
	}

	if hasWildcard {
		// For wildcard patterns, we need at least as many path parts as non-wildcard pattern parts
		if len(pathParts) < wildcardIndex {
			return false
		}

		// Check parts before wildcard
		for i := 0; i < wildcardIndex; i++ {
			patternPart := patternParts[i]
			if strings.HasPrefix(patternPart, ":") {
				// Parameter - matches anything
				continue
			}
			if patternPart != pathParts[i] {
				return false
			}
		}

		return true // Wildcard matches remaining path
	}

	// For non-wildcard patterns, parts count must match exactly
	if len(patternParts) != len(pathParts) {
		return false
	}

	for i, patternPart := range patternParts {
		if strings.HasPrefix(patternPart, ":") {
			// Parameter - matches anything
			continue
		}
		if patternPart != pathParts[i] {
			return false
		}
	}

	return true
}

func (a *HTTPServer) Init() {
	if a.initialized {
		return
	}

	if a.router == nil {
		a.Router()
	}

	a.router.registerLateRoutes(a.router)

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
	router       *httprouter.Router
	errorHandler func(Context, error) error
}

func (r *HTTPRouter) Static(prefix, root string, config ...Static) Router[*httprouter.Router] {
	fullPrefix := r.joinPath(r.prefix, prefix)
	path, handler := r.makeStaticHandler(fullPrefix, root, config...)
	wildcard := path + "/*filepath"
	r.addLateRoute(GET, path, handler, "static.get", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.get Next")
			return ctx.Next()
		}
	})
	r.addLateRoute(GET, wildcard, handler, "static.get", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.get Next")
			return ctx.Next()
		}
	})
	r.addLateRoute(HEAD, path, handler, "static.head", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.head Next")
			return ctx.Next()
		}
	})
	r.addLateRoute(HEAD, wildcard, handler, "static.head", func(hf HandlerFunc) HandlerFunc {
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
		router:       r.router,
		errorHandler: r.errorHandler,
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
		router:       r.router,
		errorHandler: r.errorHandler,
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

		// Inject route context
		goCtx := ctx.Context()

		// Always inject route name (empty string for unnamed routes)
		goCtx = WithRouteName(goCtx, route.Name)

		// Always inject route parameters
		paramMap := make(map[string]string, len(params))
		for _, p := range params {
			paramMap[p.Key] = p.Value
		}
		goCtx = WithRouteParams(goCtx, paramMap)
		ctx.SetContext(goCtx)

		if err := ctx.Next(); err != nil {
			r.logger.Error("handler chain error: %v", err)
			if r.errorHandler == nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if handleErr := r.errorHandler(ctx, err); handleErr != nil {
				r.logger.Error("error handler failed: %v", handleErr)
			}
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

func (r *HTTPRouter) WebSocket(path string, config WebSocketConfig, handler func(WebSocketContext) error) RouteInfo {
	fullPath := r.joinPath(r.prefix, path)

	r.logger.Info("registering websocket route", "path", path, "fullPath", fullPath)

	// Use HTTPRouterWebSocketHandler internally
	httpHandler := HTTPRouterWebSocketHandler(config, handler, r.views)

	// Add to HTTPRouter
	r.router.Handle("GET", fullPath, httpHandler)

	// Create route info for consistency
	route := r.addRoute(GET, fullPath, nil, "websocket", nil)

	return route
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

func (c *httpRouterContext) Request() *http.Request {
	return c.r
}

func (c *httpRouterContext) Response() http.ResponseWriter {
	return c.w
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

func (c *httpRouterContext) LocalsMerge(key any, value map[string]any) map[string]any {
	keyStr := fmt.Sprint(key)
	existing, exists := c.locals[keyStr]

	if !exists {
		c.locals[keyStr] = value
		return value
	}

	if existingMap, ok := existing.(map[string]any); ok {
		// merge overriding existing vals
		merged := make(map[string]any)
		maps.Copy(merged, existingMap)
		maps.Copy(merged, value)
		c.locals[keyStr] = merged
		return merged
	}

	c.locals[keyStr] = value
	return value
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
		tmpl, err := template.New("").Parse(buf.String())
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

func (c *httpRouterContext) IP() string {
	// check proxy header
	if xff := c.r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	if xri := c.r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	if host, _, err := net.SplitHostPort(c.r.RemoteAddr); err == nil {
		return host
	}
	return c.r.RemoteAddr
}

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
		// Preserve negative MaxAge for cookie deletion semantics.
		if cookie.MaxAge != 0 {
			stdCookie.MaxAge = cookie.MaxAge
		}
		if !cookie.Expires.IsZero() {
			stdCookie.Expires = cookie.Expires
		}
	}

	// Handle SameSite string
	switch strings.ToLower(cookie.SameSite) {
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

func (c *httpRouterContext) QueryValues(name string) []string {
	values, ok := c.r.URL.Query()[name]
	if !ok || len(values) == 0 {
		return []string{}
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
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

func (c *httpRouterContext) SendStream(r io.Reader) error {
	if r == nil {
		return c.NoContent(http.StatusNoContent)
	}
	c.written = true
	_, err := io.Copy(c.w, r)
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

func (c *httpRouterContext) FormFile(key string) (*multipart.FileHeader, error) {
	if c.r.MultipartForm == nil {
		err := c.r.ParseMultipartForm(32 << 20) // 32MB default memory buffer
		if err != nil {
			return nil, fmt.Errorf("formfile: failed to parse multipart form: %w", err)
		}
	}

	_, header, err := c.r.FormFile(key)
	if err != nil {
		return nil, fmt.Errorf("formfile: cannot get file for key '%s': %w", key, err)
	}

	return header, nil
}

func (c *httpRouterContext) FormValue(key string, defaultValues ...string) string {
	if c.r.Form == nil {
		if err := c.r.ParseForm(); err != nil {
			// should we log?
		}
	}

	value := c.r.FormValue(key)
	if value != "" {
		return value
	}

	if len(defaultValues) > 0 {
		return defaultValues[0]
	}

	return ""
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

// RouteName returns the route name from context
func (c *httpRouterContext) RouteName() string {
	if name, ok := RouteNameFromContext(c.Context()); ok {
		return name
	}
	return ""
}

// RouteParams returns all route parameters as a map
func (c *httpRouterContext) RouteParams() map[string]string {
	if params, ok := RouteParamsFromContext(c.Context()); ok {
		return params
	}
	return make(map[string]string)
}
