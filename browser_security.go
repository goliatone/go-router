package router

import (
	"errors"
	"net/http"
	"strings"
)

var ErrOriginProtectionRejected = errors.New("origin protection rejected request")

type OriginProtectionConfig struct {
	Skip                  func(Context) bool
	AllowedOrigins        []string
	AllowSameOrigin       bool
	UnsafeMethods         []string
	TrustForwardedHeaders bool
	ErrorHandler          ErrorHandler
}

func OriginProtection(config ...OriginProtectionConfig) MiddlewareFunc {
	cfg := originProtectionConfigDefault(config...)

	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			if cfg.Skip != nil && cfg.Skip(c) {
				return next(c)
			}

			if !methodInSet(c.Method(), cfg.UnsafeMethods) {
				return next(c)
			}

			if allowedByOriginProtection(c, cfg) {
				return next(c)
			}

			return cfg.ErrorHandler(c, ErrOriginProtectionRejected)
		}
	}
}

func originProtectionConfigDefault(config ...OriginProtectionConfig) OriginProtectionConfig {
	cfg := OriginProtectionConfig{
		AllowSameOrigin: true,
		UnsafeMethods: []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		},
	}
	if len(config) > 0 {
		userCfg := config[0]
		cfg.Skip = userCfg.Skip
		cfg.AllowedOrigins = userCfg.AllowedOrigins
		cfg.TrustForwardedHeaders = userCfg.TrustForwardedHeaders
		if len(userCfg.UnsafeMethods) > 0 {
			cfg.UnsafeMethods = userCfg.UnsafeMethods
		}
		if userCfg.ErrorHandler != nil {
			cfg.ErrorHandler = userCfg.ErrorHandler
		}
	}
	if cfg.ErrorHandler == nil {
		cfg.ErrorHandler = func(c Context, err error) error {
			if err == nil {
				err = ErrOriginProtectionRejected
			}
			return c.Status(http.StatusForbidden).SendString(err.Error())
		}
	}
	return cfg
}

func allowedByOriginProtection(c Context, cfg OriginProtectionConfig) bool {
	origin := strings.TrimSpace(c.Header("Origin"))
	if origin != "" {
		return originAllowed(origin, c, cfg)
	}

	referer := strings.TrimSpace(c.Referer())
	if referer != "" {
		return originAllowed(referer, c, cfg)
	}

	return false
}

func originAllowed(candidate string, c Context, cfg OriginProtectionConfig) bool {
	scheme := requestSchemeForOriginCheck(c, cfg.TrustForwardedHeaders)
	host := requestHostForOriginCheck(c, cfg.TrustForwardedHeaders)
	if cfg.AllowSameOrigin && originMatchesRequest(candidate, scheme, host) {
		return true
	}
	if len(cfg.AllowedOrigins) == 0 {
		return false
	}
	return matchesAnyOriginPattern(candidate, cfg.AllowedOrigins)
}

func requestHostForOriginCheck(c Context, trustForwarded bool) string {
	if !trustForwarded {
		return requestHost(c)
	}
	if value := strings.TrimSpace(c.Header("X-Forwarded-Host")); value != "" {
		if idx := strings.Index(value, ","); idx >= 0 {
			value = value[:idx]
		}
		return strings.TrimSpace(value)
	}
	return requestHost(c)
}

func requestSchemeForOriginCheck(c Context, trustForwarded bool) string {
	if trustForwarded {
		return requestScheme(c)
	}

	if httpCtx, ok := c.(HTTPContext); ok {
		if req := httpCtx.Request(); req != nil {
			if req.TLS != nil {
				return "https"
			}
			if req.URL != nil {
				switch strings.ToLower(req.URL.Scheme) {
				case "http", "https":
					return strings.ToLower(req.URL.Scheme)
				}
			}
		}
	}
	return "http"
}

func methodInSet(method string, methods []string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	for _, candidate := range methods {
		if method == strings.ToUpper(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}
