package requestid

import (
	"github.com/goliatone/go-router"
	"github.com/google/uuid"
)

type Config struct {
	Skip       func(c router.Context) bool
	Header     string
	Generator  func() string
	ContextKey any
}

var ConfigDefault = Config{
	Skip:       nil,
	Header:     router.XRequestID,
	Generator:  uuid.NewString,
	ContextKey: "requestid",
}

func New(config ...Config) router.MiddlewareFunc {
	cfg := configDefault(config...)

	return func(hf router.HandlerFunc) router.HandlerFunc {
		return func(ctx router.Context) error {
			if cfg.Skip != nil && cfg.Skip(ctx) {
				return ctx.Next()
			}

			rid := ctx.Header(cfg.Header)
			if rid == "" {
				rid = cfg.Generator()
			}

			ctx.SetHeader(cfg.Header, rid)
			ctx.Locals(cfg.ContextKey, rid)

			return ctx.Next()
		}
	}
}

func configDefault(config ...Config) Config {
	if len(config) == 0 {
		return ConfigDefault
	}

	cfg := config[0]

	if cfg.Header == "" {
		cfg.Header = ConfigDefault.Header
	}

	if cfg.Generator == nil {
		cfg.Generator = ConfigDefault.Generator
	}

	if cfg.ContextKey == nil {
		cfg.ContextKey = ConfigDefault.ContextKey
	}

	return cfg
}
