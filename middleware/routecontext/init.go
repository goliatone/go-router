package routecontext

import (
	"github.com/goliatone/go-router"
)

type Config struct {
	Skip                func(c router.Context) bool
	TemplateContextKey  any
	CurrentRouteNameKey string
	CurrentParamsKey    string
	CurrentQueryKey     string
	ExportAsMap         bool
}

var ConfigDefault = Config{
	Skip:                nil,
	TemplateContextKey:  "template_context",
	CurrentRouteNameKey: "current_route_name",
	CurrentParamsKey:    "current_params",
	CurrentQueryKey:     "current_query",
	ExportAsMap:         true,
}

func New(config ...Config) router.MiddlewareFunc {
	cfg := configDefault(config...)

	return func(hf router.HandlerFunc) router.HandlerFunc {
		return func(ctx router.Context) error {
			if cfg.Skip != nil && cfg.Skip(ctx) {
				return ctx.Next()
			}

			currentRoute := ctx.RouteName()
			currentParams := ctx.RouteParams()
			currentQuery := ctx.Queries()

			if cfg.ExportAsMap {
				ctx.LocalsMerge(cfg.TemplateContextKey, map[string]any{
					cfg.CurrentRouteNameKey: currentRoute,
					cfg.CurrentParamsKey:    currentParams,
					cfg.CurrentQueryKey:     currentQuery,
				})
			} else {
				ctx.Locals(cfg.CurrentRouteNameKey, currentRoute)
				ctx.Locals(cfg.CurrentParamsKey, currentParams)
				ctx.Locals(cfg.CurrentQueryKey, currentQuery)
			}

			return ctx.Next()
		}
	}
}

func configDefault(config ...Config) Config {
	if len(config) == 0 {
		return ConfigDefault
	}

	cfg := config[0]

	if cfg.TemplateContextKey == nil {
		cfg.TemplateContextKey = ConfigDefault.TemplateContextKey
	}

	if cfg.CurrentRouteNameKey == "" {
		cfg.CurrentRouteNameKey = ConfigDefault.CurrentRouteNameKey
	}

	if cfg.CurrentParamsKey == "" {
		cfg.CurrentParamsKey = ConfigDefault.CurrentParamsKey
	}

	if cfg.CurrentQueryKey == "" {
		cfg.CurrentQueryKey = ConfigDefault.CurrentQueryKey
	}

	return cfg
}
