package flash

import (
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

type Flash struct {
	data   router.ViewContext
	config Config
}

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
	if config.SameSite == "" {
		config.SameSite = "Lax"
	}
	return &Flash{
		config: config,
		data:   router.ViewContext{},
	}
}

func (f *Flash) Get(c router.Context) router.ViewContext {
	t := router.ViewContext{}
	f.data = nil
	cookieValue := c.Cookies(f.config.Name)
	if cookieValue != "" {
		parseKeyValueCookie(cookieValue, func(key string, val any) {
			t[key] = val
		})
		f.data = t
	}
	c.Set("Set-Cookie", f.config.Name+"=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/; HttpOnly; SameSite="+f.config.SameSite)
	if f.data == nil {
		f.data = router.ViewContext{}
	}
	return f.data
}

func (f *Flash) Redirect(c router.Context, location string, data any, status ...int) error {
	f.data = data.(router.ViewContext)
	if len(status) > 0 {
		return c.Redirect(location, status[0])
	} else {
		return c.Redirect(location, fiber.StatusFound)
	}
}

func (f *Flash) RedirectToRoute(c router.Context, routeName string, data router.ViewContext, status ...int) error {
	f.data = data
	if len(status) > 0 {
		return c.RedirectToRoute(routeName, data, status[0])
	} else {
		return c.RedirectToRoute(routeName, data, fiber.StatusFound)
	}
}

func (f *Flash) RedirectBack(c router.Context, fallback string, data router.ViewContext, status ...int) error {
	f.data = data
	if len(status) > 0 {
		return c.RedirectBack(fallback, status[0])
	} else {
		return c.RedirectBack(fallback, fiber.StatusFound)
	}
}

func (f *Flash) WithError(c router.Context, data router.ViewContext) router.Context {
	f.data = data
	f.error(c)
	return c
}

func (f *Flash) WithSuccess(c router.Context, data router.ViewContext) router.Context {
	f.data = data
	f.success(c)
	return c
}

func (f *Flash) WithWarn(c router.Context, data router.ViewContext) router.Context {
	f.data = data
	f.warn(c)
	return c
}

func (f *Flash) WithInfo(c router.Context, data router.ViewContext) router.Context {
	f.data = data
	f.info(c)
	return c
}

func (f *Flash) WithData(c router.Context, data router.ViewContext) router.Context {
	f.data = data
	f.setCookie(c)
	return c
}

func (f *Flash) error(c router.Context) {
	f.data["error"] = true
	f.setCookie(c)
}

func (f *Flash) success(c router.Context) {
	f.data["success"] = true
	f.setCookie(c)
}

func (f *Flash) warn(c router.Context) {
	f.data["warn"] = true
	f.setCookie(c)
}

func (f *Flash) info(c router.Context) {
	f.data["info"] = true
	f.setCookie(c)
}

func (f *Flash) setCookie(c router.Context) {
	var flashValue string
	for key, value := range f.data {
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
		HTTPOnly:    f.config.HTTPOnly,
		SessionOnly: f.config.SessionOnly,
	})
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

// parseKeyValueCookie takes the raw (escaped) cookie value and parses out key values.
func parseKeyValueCookie(val string, cb func(key string, val any)) {
	val, _ = url.QueryUnescape(val)
	if matches := cookieKeyValueParser.FindAllStringSubmatch(val, -1); matches != nil {
		for _, match := range matches {
			cb(match[1], match[2])
		}
	}
}
