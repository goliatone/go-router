package flash

import (
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
)

type Config struct {
	Skip       func(c router.Context) bool
	ContextKey any
	Flash      *flash.Flash
}

var ConfigDefault = Config{
	Skip:       nil,
	ContextKey: "flash",
	Flash:      flash.DefaultFlash,
}

func New(config ...Config) router.MiddlewareFunc {
	cfg := configDefault(config...)

	return func(hf router.HandlerFunc) router.HandlerFunc {
		return func(ctx router.Context) error {
			if cfg.Skip != nil && cfg.Skip(ctx) {
				return ctx.Next()
			}

			flashData := cfg.Flash.Get(ctx)
			ctx.Locals(cfg.ContextKey, flashData)

			return ctx.Next()
		}
	}
}

func configDefault(config ...Config) Config {
	if len(config) == 0 {
		return ConfigDefault
	}

	cfg := config[0]

	if cfg.ContextKey == nil {
		cfg.ContextKey = ConfigDefault.ContextKey
	}

	if cfg.Flash == nil {
		cfg.Flash = ConfigDefault.Flash
	}

	return cfg
}
