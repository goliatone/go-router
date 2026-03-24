package router

import (
	"fmt"
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
