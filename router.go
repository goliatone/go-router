package router

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"time"

	goerrors "github.com/goliatone/go-errors"
)

const (
	HeaderAuthorization = "Authorization"
	HeaderContentType   = "Content-Type"
)

// Context keys for route information
type contextKey int

const (
	contextKeyRouteName contextKey = iota
	contextKeyRouteParams
)

// HTTPMethod represents HTTP request methods
type HTTPMethod string

type HandlerFunc func(Context) error

func (h HandlerFunc) AsMiddlware() MiddlewareFunc {
	return ToMiddleware(h)
}

type ErrorHandler = func(Context, error) error

type MiddlewareFunc func(HandlerFunc) HandlerFunc

const (
	GET    HTTPMethod = "GET"
	POST   HTTPMethod = "POST"
	PUT    HTTPMethod = "PUT"
	DELETE HTTPMethod = "DELETE"
	PATCH  HTTPMethod = "PATCH"
	HEAD   HTTPMethod = "HEAD"
)

// ViewContext provide template values
type ViewContext map[string]any

// Views is the interface that wraps the Render function.
type Views interface {
	Load() error
	Render(io.Writer, string, any, ...string) error
}

type Serializer interface {
	Serialize() ([]byte, error)
}

type RequestContext interface {
	Method() string
	Path() string

	Param(name string, defaultValue ...string) string
	ParamsInt(key string, defaultValue int) int

	Query(name string, defaultValue ...string) string
	QueryValues(name string) []string
	QueryInt(name string, defaultValue int) int
	Queries() map[string]string

	Body() []byte

	// BodyRaw() []byte

	Locals(key any, value ...any) any
	LocalsMerge(key any, value map[string]any) map[string]any
	Render(name string, bind any, layouts ...string) error

	Cookie(cookie *Cookie)
	Cookies(key string, defaultValue ...string) string
	CookieParser(out any) error
	Redirect(location string, status ...int) error
	RedirectToRoute(routeName string, params ViewContext, status ...int) error
	RedirectBack(fallback string, status ...int) error

	Header(string) string
	Referer() string
	OriginalURL() string

	FormFile(key string) (*multipart.FileHeader, error)
	FormValue(key string, defaultValue ...string) string

	IP() string

	// GetRouteURL(routeName string, params Map) (string, error)
	// RedirectToRoute(routeName string, params Map, status ...int) error
	// Redirect(location string, status ...int) error
	// BindVars(vars Map) error
	// Path(override ...string) string
	// AllParams() map[string]string
	// ParamsParser(out any) error

	// QueryBool(key string, defaultValue ...bool) bool
	// QueryFloat(key string, defaultValue ...float64) float64
	// QueryParser(out any) error
	// SendFile(file string, compress ...bool) error
	// IsSecure() bool
	// IsFromLocal() bool
	// SendString(body string) error
	// SendStream(stream io.Reader, size ...int) error
}

type ResponseWriter interface {
	Status(code int) Context
	Send(body []byte) error
	SendString(body string) error
	SendStatus(code int) error
	JSON(code int, v any) error
	SendStream(r io.Reader) error
	// NoContent for status codes that shouldn't have response bodies (204, 205, 304).
	NoContent(code int) error
	SetHeader(string, string) Context
	// Download(file string, filename ...string) error
	// SendFile(file string, compress ...bool) error
}

// HTTPContext exposes net/http request/response primitives for adapters that support them.
type HTTPContext interface {
	Request() *http.Request
	Response() http.ResponseWriter
}

// ContextStore is a request scoped, in-memoroy
// store to pass data between middleware/handlers
// in the same request in a fremework agnostic
// way.
// If you need persistence between requests use
// Store e.g. for authentication middleware.
type ContextStore interface {
	Set(key string, value any)
	Get(key string, def any) any
	GetString(key string, def string) string
	GetInt(key string, def int) int
	GetBool(key string, def bool) bool
}

// Context represents a generic HTTP context
type Context interface {
	RequestContext
	ResponseWriter
	ContextStore
	// Body parsing
	Bind(v any) error // TODO: Myabe rename to ParseBody

	// Context methods
	Context() context.Context
	SetContext(context.Context)
	Next() error

	// Route context methods
	RouteName() string
	RouteParams() map[string]string
}

// WebSocketContext extends Context for WebSocket operations
type WebSocketContext interface {
	Context

	// Connection state
	IsWebSocket() bool
	WebSocketUpgrade() error

	// Message operations
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	WriteJSON(v any) error
	ReadJSON(v any) error
	WritePing(data []byte) error
	WritePong(data []byte) error

	// Connection management
	Close() error
	CloseWithStatus(code int, reason string) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPingHandler(handler func(data []byte) error)
	SetPongHandler(handler func(data []byte) error)
	SetCloseHandler(handler func(code int, text string) error)

	// WebSocket-specific headers/info
	Subprotocol() string
	Extensions() []string
	RemoteAddr() string
	LocalAddr() string

	// Connection properties
	IsConnected() bool
	ConnectionID() string

	// Pre-upgrade data access
	UpgradeData(key string) (any, bool)
}

// NamedHandler is a handler with a name for debugging/printing
type NamedHandler struct {
	Name    string
	Handler HandlerFunc
}

type RouteInfo interface {
	SetName(string) RouteInfo
	SetDescription(string) RouteInfo
	SetSummary(s string) RouteInfo
	AddTags(...string) RouteInfo
	AddParameter(name, in string, required bool, schema map[string]any) RouteInfo
	SetRequestBody(desc string, required bool, content map[string]any) RouteInfo
	AddResponse(code int, desc string, content map[string]any) RouteInfo
	// FromRouteDefinition(r2 *RouteDefinition) RouteInfo
}

// Static configuration options
type Static struct {
	FS             fs.FS               // Optional filesystem implementation
	Root           string              // Root directory
	Browse         bool                // Enable directory browsing
	Index          string              // Index file (default: index.html)
	MaxAge         int                 // Max-Age for cache control header
	Download       bool                // Force download
	Compress       bool                // Enable compression
	ModifyResponse func(Context) error // Hook to modify response
	// ModifyResponse func(Context, []byte) ([]byte, error) // Hook to modify response
}

// Router represents a generic router interface
type Router[T any] interface {
	Handle(method HTTPMethod, path string, handler HandlerFunc, middlewares ...MiddlewareFunc) RouteInfo
	Group(prefix string) Router[T]
	Mount(prefix string) Router[T]
	WithGroup(path string, cb func(r Router[T])) Router[T]
	Use(m ...MiddlewareFunc) Router[T]
	Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Head(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo

	Static(prefix, root string, config ...Static) Router[T]

	// WebSocket handling
	WebSocket(path string, config WebSocketConfig, handler func(WebSocketContext) error) RouteInfo

	// TODO: Move to a different interface e.g. MetaRouter
	Routes() []RouteDefinition
	ValidateRoutes() []error
	// For debugging: Print a table of routes and their middleware chain
	PrintRoutes()
	WithLogger(logger Logger) Router[T]
}

// TODO: Maybe incorporate into Router[T]
type PrefixedRouter interface {
	GetPrefix() string
}

// Server represents a generic server interface
type Server[T any] interface {
	Init()
	Router() Router[T]
	WrapHandler(HandlerFunc) any
	WrappedRouter() T
	Serve(address string) error
	Shutdown(ctx context.Context) error
}

// WrapHandler function to wrap handlers that return error
func WrapHandler(handler func(Context) error) HandlerFunc {
	return HandlerFunc(handler)
}

// ToMiddleware function to wrap handlers and run them as a middleware
func ToMiddleware(h HandlerFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			if err := h(c); err != nil {
				return err
			}
			return c.Next()
		}
	}
}

// MiddlewareFromHTTP that transforms a standard Go HTTP middleware
// which takes and returns http.Handler, into a MiddlewareFunc suitable
// for use with our router.
// This function essentially adapts the http.Handler pattern to the
// HandlerFunc (Context) error interface
func MiddlewareFromHTTP(mw func(next http.Handler) http.Handler) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			// c should be *httpRouterContext
			ctx, ok := c.(*httpRouterContext)
			if !ok {
				return fmt.Errorf("context is not httpRouterContext")
			}

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origW, origR := ctx.w, ctx.r
				defer func() {
					ctx.w = origW
					ctx.r = origR
				}()
				ctx.w = w
				ctx.r = r
				if err := ctx.Next(); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			})

			finalHandler := mw(nextHandler)
			finalHandler.ServeHTTP(ctx.w, ctx.r)
			return nil
		}
	}
}

type finalizeWriter interface {
	Finalize()
}

type headResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *headResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *headResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return len(p), nil
}

func (w *headResponseWriter) Finalize() {
	if fw, ok := w.ResponseWriter.(finalizeWriter); ok {
		fw.Finalize()
		return
	}
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
}

func newHTTPAdapterError(code int, message string) error {
	if message == "" {
		message = http.StatusText(code)
	}
	return goerrors.New(message, goerrors.HTTPStatusToCategory(code)).
		WithCode(code).
		WithTextCode(goerrors.HTTPStatusToTextCode(code))
}

// HandlerFromHTTP adapts a net/http handler to a go-router HandlerFunc.
// Works with any Context that also implements HTTPContext.
func HandlerFromHTTP(h http.Handler) HandlerFunc {
	return func(c Context) error {
		if h == nil {
			return newHTTPAdapterError(http.StatusInternalServerError, "handler_from_http: nil handler")
		}

		httpCtx, ok := c.(HTTPContext)
		if !ok {
			return newHTTPAdapterError(http.StatusNotImplemented, "handler_from_http: context does not implement HTTPContext")
		}

		req := httpCtx.Request()
		res := httpCtx.Response()
		if req == nil || res == nil {
			return newHTTPAdapterError(http.StatusInternalServerError, "handler_from_http: nil request/response")
		}

		req = req.WithContext(c.Context())

		if req.Method == http.MethodHead {
			if _, ok := res.(*fasthttpResponseWriter); ok {
				res = &headResponseWriter{ResponseWriter: res}
			}
		}

		h.ServeHTTP(res, req)

		if fw, ok := res.(finalizeWriter); ok {
			fw.Finalize()
		}

		if ctx, ok := c.(*httpRouterContext); ok && !ctx.written {
			ctx.written = true
		}

		return nil
	}
}

// AsHTTPContext returns the HTTPContext if the adapter supports it.
func AsHTTPContext(c Context) (HTTPContext, bool) {
	if c == nil {
		return nil, false
	}
	ctx, ok := c.(HTTPContext)
	return ctx, ok
}

// Helper functions for type-safe context operations
func WithRouteName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, contextKeyRouteName, name)
}

func WithRouteParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, contextKeyRouteParams, params)
}

func RouteNameFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(contextKeyRouteName).(string)
	return name, ok
}

func RouteParamsFromContext(ctx context.Context) (map[string]string, bool) {
	params, ok := ctx.Value(contextKeyRouteParams).(map[string]string)
	return params, ok
}
