package router

import (
	"net/http"
	"reflect"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

func normalizeErrorHandlerConfig(cfg ErrorHandlerConfig) ErrorHandlerConfig {
	def := DefaultErrorHandlerConfig()
	if reflect.ValueOf(cfg).IsZero() {
		return def
	}

	if cfg.Logger == nil {
		cfg.Logger = def.Logger
	}

	if cfg.GetRequestID == nil {
		cfg.GetRequestID = def.GetRequestID
	}

	if len(cfg.ErrorMappers) == 0 {
		cfg.ErrorMappers = def.ErrorMappers
	}

	if cfg.Environment == "" {
		cfg.Environment = def.Environment
	}

	return cfg
}

func normalizeAPIPrefix(prefix string) string {
	if prefix == "" {
		prefix = "/api"
	}
	return "/" + strings.TrimPrefix(prefix, "/")
}

func buildAPIErrorResponse(rawErr error, code int, ctx Context, cfg ErrorHandlerConfig, fullError bool) (goerrors.ErrorResponse, int) {
	routerErr := goerrors.MapToError(rawErr, cfg.ErrorMappers)
	if routerErr.Code == 0 {
		routerErr.Code = code
	} else {
		code = routerErr.Code
	}

	if cfg.GetRequestID != nil {
		if requestID := cfg.GetRequestID(ctx); requestID != "" {
			routerErr.RequestID = requestID
		}
	}

	errCfg := cfg
	if !fullError {
		errCfg.IncludeStack = false
	}

	response := PrepareErrorResponse(routerErr, errCfg)

	if !fullError {
		if msg := http.StatusText(code); msg != "" {
			response.Error.Message = msg
		}
	}

	LogError(cfg.Logger, routerErr, ctx)

	return response, code
}
