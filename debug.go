package router

import (
	"reflect"
	"runtime"
	"strings"
)

// funcName returns a friendly name for a (middleware) function.
// If the (middleware) function is anonymous or its name is not
// extractable, it returns a default name.
func funcName(mw any) string {
	v := reflect.ValueOf(mw)
	if v.Kind() != reflect.Func {
		return "non-function"
	}

	fn := runtime.FuncForPC(v.Pointer())
	if fn == nil {
		return "unknown"
	}

	fullName := fn.Name()

	// trim package path to keep only the name.
	if idx := strings.LastIndex(fullName, "."); idx != -1 {
		fullName = fullName[idx+1:]
	}

	if strings.HasPrefix(fullName, "func") {
		return "anonymous"
	}

	return fullName
}
