package router

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	CookieSameSiteDisabled   = "disabled" // not in RFC, just control "SameSite" attribute will not be set.
	CookieSameSiteLaxMode    = "lax"
	CookieSameSiteStrictMode = "strict"
	CookieSameSiteNoneMode   = "none"
)

// Cookie data for c.Cookie
type Cookie struct {
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

func NormalizeCookieSameSite(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", CookieSameSiteDisabled:
		return CookieSameSiteDisabled
	case CookieSameSiteLaxMode:
		return CookieSameSiteLaxMode
	case CookieSameSiteStrictMode:
		return CookieSameSiteStrictMode
	case CookieSameSiteNoneMode:
		return CookieSameSiteNoneMode
	default:
		return ""
	}
}

func ValidateCookie(cookie Cookie) error {
	sameSite := NormalizeCookieSameSite(cookie.SameSite)
	if sameSite == "" {
		return fmt.Errorf("invalid cookie SameSite value %q", cookie.SameSite)
	}
	if sameSite == CookieSameSiteNoneMode && !cookie.Secure {
		return fmt.Errorf("SameSite=None cookies must also set Secure")
	}
	return nil
}

func FirstPartySessionCookie(name, value string) Cookie {
	return Cookie{
		Name:        strings.TrimSpace(name),
		Value:       value,
		Path:        "/",
		HTTPOnly:    true,
		SameSite:    CookieSameSiteLaxMode,
		SessionOnly: true,
	}
}

func EmbeddedThirdPartySessionCookie(name, value string) Cookie {
	return Cookie{
		Name:        strings.TrimSpace(name),
		Value:       value,
		Path:        "/",
		HTTPOnly:    true,
		Secure:      true,
		SameSite:    CookieSameSiteNoneMode,
		SessionOnly: true,
	}
}

func routerCookieToHTTP(cookie *Cookie) *http.Cookie {
	if cookie == nil {
		return nil
	}

	stdCookie := &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		Secure:   cookie.Secure,
		HttpOnly: cookie.HTTPOnly,
	}

	if !cookie.SessionOnly {
		if cookie.MaxAge != 0 {
			stdCookie.MaxAge = cookie.MaxAge
		}
		if !cookie.Expires.IsZero() {
			stdCookie.Expires = cookie.Expires
		}
	}

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

	return stdCookie
}
