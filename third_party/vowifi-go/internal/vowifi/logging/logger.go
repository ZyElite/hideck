// Package logging provides the IMS stack's structured logging, rate limiting,
// run-mode detection, and sensitive-data redaction.
package logging

import "log/slog"

// Info writes a structured informational event.
func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Debug writes a structured debug event.
func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// RunInfo writes an informational event only for a process started by go run.
func RunInfo(msg string, args ...any) {
	if IsGoRun() {
		slog.Info(msg, args...)
	}
}

// RunDebug writes a debug event only for a process started by go run.
func RunDebug(msg string, args ...any) {
	if IsGoRun() {
		slog.Debug(msg, args...)
	}
}
