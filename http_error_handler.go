package router

import (
	stderrors "errors"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

type HTTPErrorHandlerConfig struct {
	APIPrefix      string
	FullError      bool
	DelegateNonAPI bool
	ErrorConfig    ErrorHandlerConfig
}

func DefaultHTTPErrorHandlerConfig() HTTPErrorHandlerConfig {
	return HTTPErrorHandlerConfig{
		APIPrefix:      "/api",
		FullError:      false,
		DelegateNonAPI: true,
		ErrorConfig:    DefaultErrorHandlerConfig(),
	}
}

func (cfg HTTPErrorHandlerConfig) withDefaults() HTTPErrorHandlerConfig {
	cfg.APIPrefix = normalizeAPIPrefix(cfg.APIPrefix)
	cfg.ErrorConfig = normalizeErrorHandlerConfig(cfg.ErrorConfig)
	return cfg
}

func DefaultHTTPErrorHandler(cfg HTTPErrorHandlerConfig) func(c Context, err error) error {
	cfg = cfg.withDefaults()
	apiPrefix := cfg.APIPrefix

	return func(c Context, err error) error {
		path := c.Path()
		isAPI := strings.HasPrefix(path, apiPrefix)

		code, rawErr := httpStatusFromError(err)
		if !isAPI {
			if cfg.DelegateNonAPI {
				return writeHTTPError(c, err, code)
			}
			return c.Status(code).SendString(err.Error())
		}

		response, status := buildAPIErrorResponse(rawErr, code, c, cfg.ErrorConfig, cfg.FullError)
		return c.JSON(status, response)
	}
}

func httpStatusFromError(err error) (int, error) {
	if err == nil {
		return http.StatusInternalServerError, err
	}

	var routerErr *goerrors.Error
	if stderrors.As(err, &routerErr) {
		if routerErr.Code != 0 {
			return routerErr.Code, err
		}
	}

	type statusCoder interface {
		StatusCode() int
	}
	if se, ok := err.(statusCoder); ok {
		if code := se.StatusCode(); code != 0 {
			return code, err
		}
	}

	type coder interface {
		Code() int
	}
	if ce, ok := err.(coder); ok {
		if code := ce.Code(); code != 0 {
			return code, err
		}
	}

	return http.StatusInternalServerError, err
}

func writeHTTPError(c Context, err error, code int) error {
	message := http.StatusText(code)
	if err != nil {
		message = err.Error()
	}

	if hc, ok := c.(*httpRouterContext); ok && hc != nil {
		http.Error(hc.w, message, code)
		hc.written = true
		return nil
	}

	c.Status(code)
	return c.SendString(message)
}
