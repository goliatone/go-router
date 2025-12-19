package router

import (
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
	cfg.APIPrefix = normalizeAPIPrefix(cfg.APIPrefix)
	cfg.ErrorConfig = normalizeErrorHandlerConfig(cfg.ErrorConfig)
	return cfg
}

func DefaultFiberErrorHandler(cfg FiberErrorHandlerConfig) func(c *fiber.Ctx, err error) error {
	cfg = cfg.withDefaults()
	apiPrefix := cfg.APIPrefix

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

		routerCtx := NewFiberContext(c, cfg.ErrorConfig.Logger)
		response, status := buildAPIErrorResponse(rawErr, code, routerCtx, cfg.ErrorConfig, cfg.FullError)
		return c.Status(status).JSON(response)
	}
}
