package router

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	goerrors "github.com/goliatone/go-errors"
)

func DefaultFiberV3ErrorHandler(cfg FiberErrorHandlerConfig) func(c fiber.Ctx, err error) error {
	cfg = cfg.withDefaults()
	apiPrefix := cfg.APIPrefix

	return func(c fiber.Ctx, err error) error {
		path := c.Path()
		isAPI := strings.HasPrefix(path, apiPrefix)

		code := fiber.StatusInternalServerError
		rawErr := err
		var e *fiber.Error
		if errors.As(err, &e) && e != nil {
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

		routerCtx := NewFiberV3Context(c, cfg.ErrorConfig.Logger)
		response, status := buildAPIErrorResponse(rawErr, code, routerCtx, cfg.ErrorConfig, cfg.FullError)
		return c.Status(status).JSON(response)
	}
}
