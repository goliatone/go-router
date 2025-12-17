package flash

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

type Flash struct {
	config Config
}

const pendingLocalsKey = "__router_flash_pending"

type Config struct {
	Name        string    `json:"name"`
	Value       string    `json:"value"`
	Path        string    `json:"path"`
	Domain      string    `json:"domain"`
	MaxAge      int       `json:"max_age"`
	Expires     time.Time `json:"expires"`
	Secure      bool      `json:"secure"`
	HTTPOnly    bool      `json:"http_only"`
	SameSite    string    `json:"same_site"`
	SessionOnly bool      `json:"session_only"`

	// ClientAccessible controls whether the flash cookie can be read by client-side JavaScript.
	// Leave this false for SSR use-cases where middleware injects flash into templates.
	// Set this true for purely client-rendered UIs that need to read the cookie after redirects.
	ClientAccessible bool `json:"client_accessible"`

	// DefaultMessageTitle and DefaultMessageText are used when setting toast messages with
	// missing Title/Text fields.
	DefaultMessageTitle string `json:"default_message_title"`
	DefaultMessageText  string `json:"default_message_text"`
}

func ToMiddleware(f *Flash, key string) router.MiddlewareFunc {
	panic("deprecated: use new middleware")
}

var DefaultFlash *Flash

func init() {
	Default(Config{
		Name: "router-app-flash",
	})
}

var cookieKeyValueParser = regexp.MustCompile("\x00([^:]*):([^\x00]*)\x00")

func Default(config Config) {
	DefaultFlash = New(config)
}

func New(config Config) *Flash {
	config = normalizeConfig(config)
	return &Flash{
		config: config,
	}
}

func (f *Flash) Get(c router.Context) router.ViewContext {
	cookieValue := c.Cookies(f.config.Name)
	if cookieValue == "" {
		return router.ViewContext{}
	}

	out := router.ViewContext{}
	parseKeyValueCookie(cookieValue, func(key string, val any) {
		out[key] = val
	})

	f.clearCookie(c)
	return out
}

func (f *Flash) Redirect(c router.Context, location string, data any, status ...int) error {
	var flashData router.ViewContext
	switch v := data.(type) {
	case nil:
		flashData = router.ViewContext{}
	case router.ViewContext:
		flashData = v
	case map[string]any:
		flashData = router.ViewContext(v)
	default:
		flashData = router.ViewContext{"error": true, "error_message": fmt.Sprintf("flash.Redirect: unsupported data type %T", data)}
	}

	f.setCookie(c, flashData)
	if len(status) > 0 {
		return c.Redirect(location, status[0])
	} else {
		return c.Redirect(location, fiber.StatusFound)
	}
}

func (f *Flash) RedirectToRoute(c router.Context, routeName string, data router.ViewContext, status ...int) error {
	f.setCookie(c, data)
	if len(status) > 0 {
		return c.RedirectToRoute(routeName, data, status[0])
	} else {
		return c.RedirectToRoute(routeName, data, fiber.StatusFound)
	}
}

func (f *Flash) RedirectBack(c router.Context, fallback string, data router.ViewContext, status ...int) error {
	f.setCookie(c, data)
	if len(status) > 0 {
		return c.RedirectBack(fallback, status[0])
	} else {
		return c.RedirectBack(fallback, fiber.StatusFound)
	}
}

func (f *Flash) WithError(c router.Context, data router.ViewContext) router.Context {
	f.setCookieWithFlag(c, data, "error")
	return c
}

func (f *Flash) WithSuccess(c router.Context, data router.ViewContext) router.Context {
	f.setCookieWithFlag(c, data, "success")
	return c
}

func (f *Flash) WithWarn(c router.Context, data router.ViewContext) router.Context {
	f.setCookieWithFlag(c, data, "warn")
	return c
}

func (f *Flash) WithInfo(c router.Context, data router.ViewContext) router.Context {
	f.setCookieWithFlag(c, data, "info")
	return c
}

func (f *Flash) WithData(c router.Context, data router.ViewContext) router.Context {
	f.setCookie(c, data)
	return c
}

func (f *Flash) setCookieWithFlag(c router.Context, data router.ViewContext, flag string) {
	merged := cloneViewContext(data)
	merged[flag] = true
	f.setCookie(c, merged)
}

func (f *Flash) setCookie(c router.Context, data router.ViewContext) {
	merged := f.mergePending(c, data)

	var flashValue string
	for key, value := range merged {
		flashValue += "\x00" + key + ":" + fmt.Sprintf("%v", value) + "\x00"
	}
	c.Cookie(&router.Cookie{
		Name:        f.config.Name,
		Value:       url.QueryEscape(flashValue),
		SameSite:    f.config.SameSite,
		Secure:      f.config.Secure,
		Path:        f.config.Path,
		Domain:      f.config.Domain,
		MaxAge:      f.config.MaxAge,
		Expires:     f.config.Expires,
		HTTPOnly:    f.config.HTTPOnly && !f.config.ClientAccessible,
		SessionOnly: f.config.SessionOnly,
	})

	// Store payload locally so multiple flash operations in the same request can be merged safely.
	c.Locals(pendingLocalsKey, merged)
}

func (f *Flash) clearCookie(c router.Context) {
	c.Cookie(&router.Cookie{
		Name:     f.config.Name,
		Value:    "",
		Path:     f.config.Path,
		Domain:   f.config.Domain,
		SameSite: f.config.SameSite,
		Secure:   f.config.Secure,
		HTTPOnly: f.config.HTTPOnly && !f.config.ClientAccessible,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	// Clear any staged flash payload for this request.
	c.Locals(pendingLocalsKey, nil)
}

func Get(c router.Context) router.ViewContext {
	return DefaultFlash.Get(c)
}

func Redirect(c router.Context, location string, data any, status ...int) error {
	return DefaultFlash.Redirect(c, location, data, status...)
}

func RedirectToRoute(c router.Context, routeName string, data router.ViewContext, status ...int) error {
	return DefaultFlash.RedirectToRoute(c, routeName, data, status...)
}

func RedirectBack(c router.Context, fallback string, data router.ViewContext, status ...int) error {
	return DefaultFlash.RedirectBack(c, fallback, data, status...)
}

func WithError(c router.Context, data router.ViewContext) router.Context {
	return DefaultFlash.WithError(c, data)
}

func WithSuccess(c router.Context, data router.ViewContext) router.Context {
	return DefaultFlash.WithSuccess(c, data)
}

func WithWarn(c router.Context, data router.ViewContext) router.Context {
	return DefaultFlash.WithWarn(c, data)
}

func WithInfo(c router.Context, data router.ViewContext) router.Context {
	return DefaultFlash.WithInfo(c, data)
}

func WithData(c router.Context, data router.ViewContext) router.Context {
	return DefaultFlash.WithData(c, data)
}

func normalizeConfig(config Config) Config {
	if config.Name == "" {
		config.Name = "router-app-flash"
	}
	if config.Path == "" {
		config.Path = "/"
	}
	if config.SameSite == "" {
		config.SameSite = "Lax"
	}
	// Default to HTTPOnly=true unless explicitly made client-accessible.
	if !config.HTTPOnly && !config.ClientAccessible {
		config.HTTPOnly = true
	}
	return config
}

func cloneViewContext(in router.ViewContext) router.ViewContext {
	out := router.ViewContext{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (f *Flash) mergePending(c router.Context, data router.ViewContext) router.ViewContext {
	merged := router.ViewContext{}
	if c != nil {
		if v := c.Locals(pendingLocalsKey); v != nil {
			if pending, ok := v.(router.ViewContext); ok {
				maps.Copy(merged, pending)
			}
		}
	}
	maps.Copy(merged, data)
	return merged
}

// parseKeyValueCookie takes the raw (escaped) cookie value and parses out key values.
func parseKeyValueCookie(val string, cb func(key string, val any)) {
	val, _ = url.QueryUnescape(val)
	if matches := cookieKeyValueParser.FindAllStringSubmatch(val, -1); matches != nil {
		for _, match := range matches {
			cb(match[1], match[2])
		}
	}
}

func getString(m router.ViewContext, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	switch s := v.(type) {
	case string:
		return s, true
	default:
		return fmt.Sprintf("%v", v), true
	}
}

func getInt(m router.ViewContext, key string) (int, bool) {
	s, ok := getString(m, key)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
