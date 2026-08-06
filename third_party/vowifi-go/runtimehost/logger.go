package runtimehost

import "log/slog"

// logger is the package-level logger (slog; the original routes through
// engine/logger which wraps zap).
var logger = slog.Default()

// SetLogger installs the package logger.
func SetLogger(l *slog.Logger) {
	if l != nil {
		logger = l
	}
}
