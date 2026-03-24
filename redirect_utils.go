package router

import (
	"net/url"
	"strings"
)

func resolveRedirectBackTarget(c Context, fallback string) string {
	return ResolveRedirectBackTarget(c, fallback)
}

func ResolveRedirectBackTarget(c Context, fallback string) string {
	referer := strings.TrimSpace(c.Referer())
	if referer == "" {
		return fallback
	}

	if target, ok := SanitizeRedirectTarget(referer, requestScheme(c), requestHost(c)); ok {
		return target
	}

	return fallback
}

func sanitizeRedirectTarget(raw, scheme, host string) (string, bool) {
	return SanitizeRedirectTarget(raw, scheme, host)
}

func SanitizeRedirectTarget(raw, scheme, host string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}

	if u.IsAbs() {
		if !originMatchesRequest(raw, scheme, host) {
			return "", false
		}
		return redirectLocationFromURL(u), true
	}

	if u.Host != "" || strings.HasPrefix(raw, "//") || u.Scheme != "" {
		return "", false
	}

	return redirectLocationFromURL(u), true
}

func redirectLocationFromURL(u *url.URL) string {
	if u == nil {
		return "/"
	}

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		path += "#" + u.Fragment
	}

	return path
}
