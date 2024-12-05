package router

import (
	"runtime"
	"strings"
)

func funcName(i interface{}) string {
	pc := make([]uintptr, 10)
	// Skip some frames to get the caller of funcName.
	// 2 or 3 might need tweaking depending on the call stack.
	n := runtime.Callers(3, pc)
	if n == 0 {
		return "unknown"
	}

	frames := runtime.CallersFrames(pc[:n])
	frame, more := frames.Next()
	if !more {
		return "unknown"
	}

	fullName := frame.Function
	if idx := strings.LastIndex(fullName, "/"); idx != -1 {
		fullName = fullName[idx+1:]
	}
	return fullName
}
