package router

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gofiber/fiber/v2"
	goerrors "github.com/goliatone/go-errors"
)

type FiberErrorHandlerConfig struct {
	APIPrefix      string
	FullError      bool
	DelegateNonAPI bool
	ErrorConfig    ErrorHandlerConfig
}

func DefaultFiberErrorHandlerConfig() FiberErrorHandlerConfig {
	return FiberErrorHandlerConfig{
		APIPrefix:      "/api",
		FullError:      false,
		DelegateNonAPI: true,
		ErrorConfig:    DefaultErrorHandlerConfig(),
	}
}

func (cfg FiberErrorHandlerConfig) withDefaults() FiberErrorHandlerConfig {
	if cfg.APIPrefix == "" {
		cfg.APIPrefix = "/api"
	}

	def := DefaultErrorHandlerConfig()

	if reflect.ValueOf(cfg.ErrorConfig).IsZero() {
		cfg.ErrorConfig = def
	} else {
		if cfg.ErrorConfig.Logger == nil {
			cfg.ErrorConfig.Logger = def.Logger
		}

		if cfg.ErrorConfig.GetRequestID == nil {
			cfg.ErrorConfig.GetRequestID = def.GetRequestID
		}

		if len(cfg.ErrorConfig.ErrorMappers) == 0 {
			cfg.ErrorConfig.ErrorMappers = def.ErrorMappers
		}

		if cfg.ErrorConfig.Environment == "" {
			cfg.ErrorConfig.Environment = def.Environment
		}
	}

	return cfg
}

func DefaultFiberErrorHandler(cfg FiberErrorHandlerConfig) func(c *fiber.Ctx, err error) error {
	cfg = cfg.withDefaults()
	apiPrefix := "/" + strings.TrimPrefix(cfg.APIPrefix, "/")

	return func(c *fiber.Ctx, err error) error {
		path := c.Path()
		isAPI := strings.HasPrefix(path, apiPrefix)

		code := fiber.StatusInternalServerError
		rawErr := err
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
			rawErr = goerrors.New(e.Message, goerrors.HTTPStatusToCategory(code)).
				WithCode(code).
				WithTextCode(goerrors.HTTPStatusToTextCode(code))
		}

		if !isAPI {
			if cfg.DelegateNonAPI {
				return fiber.DefaultErrorHandler(c, err)
			}
			return c.Status(code).SendString(err.Error())
		}

		routerErr := goerrors.MapToError(rawErr, cfg.ErrorConfig.ErrorMappers)
		if routerErr.Code == 0 {
			routerErr.Code = code
		} else {
			code = routerErr.Code
		}

		routerCtx := NewFiberContext(c, cfg.ErrorConfig.Logger)

		if requestID := cfg.ErrorConfig.GetRequestID(routerCtx); requestID != "" {
			routerErr.RequestID = requestID
		}

		// In safe mode, suppress stack traces.
		if !cfg.FullError {
			cfg.ErrorConfig.IncludeStack = false
		}

		response := PrepareErrorResponse(routerErr, cfg.ErrorConfig)

		if !cfg.FullError {
			if msg := http.StatusText(code); msg != "" {
				response.Error.Message = msg
			}
		}

		LogError(cfg.ErrorConfig.Logger, routerErr, routerCtx)

		return c.Status(code).JSON(response)
	}
}
