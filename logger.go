package router

import "fmt"

var LoggerEnabled = false

type defaultLogger struct {
}

func (d *defaultLogger) Debug(format string, args ...any) {
	if LoggerEnabled {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

func (d *defaultLogger) Info(format string, args ...any) {
	if LoggerEnabled {
		fmt.Printf("[INFO] "+format+"\n", args...)
	}
}

func (d *defaultLogger) Error(format string, args ...any) {
	if LoggerEnabled {
		fmt.Printf("[ERROR] "+format+"\n", args...)
	}
}
