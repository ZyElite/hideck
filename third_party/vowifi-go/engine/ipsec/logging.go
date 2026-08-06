package ipsec

import "log/slog"

// Package-level logging helpers. The original routes through engine/logger
// (zap); slog is used here for a self-contained, dependency-free sink.
func logDebug(msg string, args ...any) { slog.Debug(msg, args...) }
func logInfo(msg string, args ...any)  { slog.Info(msg, args...) }
func logWarn(msg string, args ...any)  { slog.Warn(msg, args...) }
