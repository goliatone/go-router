package router

import (
	"context"
	"fmt"
	"net/http"
)

const (
	HeaderAuthorization = "Authorization"
	HeaderContentType   = "Content-Type"
)

// HTTPMethod represents HTTP request methods
type HTTPMethod string

type HandlerFunc func(Context) error

func (h HandlerFunc) AsMiddlware() MiddlewareFunc {
	return ToMiddleware(h)
}

type MiddlewareFunc func(HandlerFunc) HandlerFunc

const (
	GET    HTTPMethod = "GET"
	POST   HTTPMethod = "POST"
	PUT    HTTPMethod = "PUT"
	DELETE HTTPMethod = "DELETE"
	PATCH  HTTPMethod = "PATCH"
)

type Logger interface {
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Error(format string, args ...any)
}

type RequestContext interface {
	Method() string
	Path() string

	Param(name string, defaultValue string) string
	ParamsInt(key string, defaultValue int) int

	Query(name string, defaultValue string) string
	QueryInt(name string, defaultValue int) int
	Queries() map[string]string

	Body() []byte

	// BodyRaw() []byte

	// GetRouteURL(routeName string, params Map) (string, error)
	// RedirectToRoute(routeName string, params Map, status ...int) error
	// Render(name string, bind interface{}, layouts ...string) error
	// Redirect(location string, status ...int) error
	// BindVars(vars Map) error
	// Path(override ...string) string
	// AllParams() map[string]string
	// ParamsParser(out interface{}) error

	// QueryBool(key string, defaultValue ...bool) bool
	// QueryFloat(key string, defaultValue ...float64) float64
	// QueryParser(out interface{}) error
	// SendFile(file string, compress ...bool) error
	// IsSecure() bool
	// IsFromLocal() bool
	// SendString(body string) error
	// SendStream(stream io.Reader, size ...int) error
}

type ResponseWriter interface {
	Status(code int) ResponseWriter
	Send(body []byte) error
	JSON(code int, v any) error
	// NoContent for status codes that shouldn't have response bodies (204, 205, 304).
	NoContent(code int) error
	Header(string) string
	SetHeader(string, string) ResponseWriter
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
	Bind(any) error // TODO: Myabe rename to ParseBody

	// Context methods
	Context() context.Context
	SetContext(context.Context)
	Next() error
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

// Router represents a generic router interface
type Router[T any] interface {
	Handle(method HTTPMethod, path string, handler HandlerFunc, middlewares ...MiddlewareFunc) RouteInfo
	Group(prefix string) Router[T]
	Use(m ...MiddlewareFunc) Router[T]
	Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo
	Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo

	// TODO: Move to a different interface e.g. MetaRouter
	// For debugging: Print a table of routes and their middleware chain
	Routes() []RouteDefinition // New method to retrieve registered routes
	PrintRoutes()
}

// TODO: Maybe incorporate into Router[T]
type PrefixedRouter interface {
	GetPrefix() string
}

// Server represents a generic server interface
type Server[T any] interface {
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
				ctx.handlers = []NamedHandler{{Name: "adapted_next", Handler: next}}
				ctx.index = -1
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
