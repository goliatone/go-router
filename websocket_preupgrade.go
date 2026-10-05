package router

import (
	"errors"
	"net/http"

	goerrors "github.com/goliatone/go-errors"
)

// writeWebSocketPreUpgradeError is shared by direct routes and upgrade middleware.
// Legacy unclassified hook failures remain bad requests; internal error details
// are never sent to the peer. A rendered response must not be overwritten.
func writeWebSocketPreUpgradeError(c Context, err error) error {
	if errors.Is(err, ErrWebSocketUpgradeHandled) {
		return nil
	}
	code := webSocketPreUpgradeStatus(err)
	return c.Status(code).SendString(http.StatusText(code))
}

func webSocketPreUpgradeStatus(err error) int {
	code := http.StatusBadRequest
	var structured *goerrors.Error
	var status interface{ StatusCode() int }
	var coder interface{ Code() int }
	switch {
	case errors.As(err, &structured):
		code = structured.Code
	case errors.As(err, &status):
		code = status.StatusCode()
	case errors.As(err, &coder):
		code = coder.Code()
	case errors.Is(err, ErrUnauthorized):
		code = http.StatusUnauthorized
	}
	if code < 400 || code > 599 {
		return http.StatusBadRequest
	}
	return code
}
