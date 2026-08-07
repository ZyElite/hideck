// Package logging provides the IMS stack's logging surface: slog-based
// handlers, run-mode detection, rate-limited logging and sensitive-data
// redaction.
//
// Reconstructed from the decompiled internal/vowifi/logging.
package logging

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// runMode is the detected execution mode.
type runMode int

const (
	modeUnknown runMode = iota
	modeGoRun
	modeBinary
)

var (
	mu       sync.Mutex
	runModeV runMode
)

// IsGoRun reports whether the process was started via `go run` (used to pick
// the log level: debug under `go run`, info otherwise).
func IsGoRun() bool {
	mu.Lock()
	defer mu.Unlock()
	if runModeV == modeUnknown {
		runModeV = detectGoRunMode()
	}
	return runModeV == modeGoRun
}

// detectGoRunMode inspects the executable path for the go-build temp dir.
func detectGoRunMode() runMode {
	exe, err := os.Executable()
	if err != nil {
		return modeBinary
	}
	if strings.Contains(exe, "go-build") || strings.Contains(exe, "/tmp/go-build") {
		return modeGoRun
	}
	return modeBinary
}

// Info logs at info level.
func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Debug logs at debug level.
func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// RunInfo logs at info level, or debug when running under `go run`.
func RunInfo(msg string, args ...any) {
	if IsGoRun() {
		slog.Debug(msg, args...)
		return
	}
	slog.Info(msg, args...)
}

// RunDebug logs at debug level, or suppresses when running as a binary.
func RunDebug(msg string, args ...any) {
	if IsGoRun() {
		slog.Debug(msg, args...)
	}
}

// rateLimitWindow is the minimum interval between rate-limited log lines.
const rateLimitWindow = 5 * time.Second

var rateMu sync.Mutex

// rateState tracks the last emission time per key.
var rateState = map[string]time.Time{}

// shouldEmitRateLimited reports whether a rate-limited line for key may be
// emitted now.
func shouldEmitRateLimited(key string) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	if last, ok := rateState[key]; ok && now.Sub(last) < rateLimitWindow {
		return false
	}
	rateState[key] = now
	return true
}

// InfoRate logs at info level, at most once per rateLimitWindow per key.
func InfoRate(key, msg string, args ...any) {
	if shouldEmitRateLimited(key) {
		slog.Info(msg, args...)
	}
}

// WarnRate logs at warn level, at most once per rateLimitWindow per key.
func WarnRate(key, msg string, args ...any) {
	if shouldEmitRateLimited(key) {
		slog.Warn(msg, args...)
	}
}

// envEnabled reports whether the named environment variable is set to a
// truthy value.
func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// longDigitRe matches runs of 8+ digits (phone numbers, IMSIs, IPs).
var longDigitRe = regexp.MustCompile(`\d{8,}`)

// maskLongDigits replaces runs of 8+ digits with a masked form.
func maskLongDigits(s string) string {
	return longDigitRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) <= 4 {
			return m
		}
		return m[:2] + "****" + m[len(m)-2:]
	})
}

// RedactSIPRaw redacts sensitive values in a raw SIP message (digits and
// Authorization credentials).
func RedactSIPRaw(s string) string {
	s = maskLongDigits(s)
	// Redact Authorization / Proxy-Authorization credentials.
	s = regexp.MustCompile(`(?i)(authorization|proxy-authorization):[^\r\n]+`).
		ReplaceAllString(s, "$1: [redacted]")
	return s
}

// RedactSMSContent redacts the SMS content (keeps the first/last characters).
func RedactSMSContent(content string) string {
	if len(content) <= 8 {
		return "****"
	}
	return content[:2] + "****" + content[len(content)-2:]
}

// SlogAdapter adapts a slog.Handler to add caller information.
type SlogAdapter struct {
	inner slog.Handler
}

// NewSlogHandler wraps a slog handler with caller attribution.
func NewSlogHandler(inner slog.Handler) *SlogAdapter {
	if inner == nil {
		inner = slog.NewTextHandler(os.Stderr, nil)
	}
	return &SlogAdapter{inner: inner}
}

// Enabled reports whether the level is enabled.
func (a *SlogAdapter) Enabled(ctx context.Context, level slog.Level) bool {
	return a.inner.Enabled(ctx, level)
}

// Handle writes a log record with caller attribution.
func (a *SlogAdapter) Handle(ctx context.Context, r slog.Record) error {
	return a.writeWithCaller(ctx, r)
}

// WithAttrs returns a handler with the given attributes.
func (a *SlogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SlogAdapter{inner: a.inner.WithAttrs(attrs)}
}

// WithGroup returns a handler with the given group.
func (a *SlogAdapter) WithGroup(name string) slog.Handler {
	return &SlogAdapter{inner: a.inner.WithGroup(name)}
}

// writeWithCaller adds the caller PC to the record and delegates.
func (a *SlogAdapter) writeWithCaller(ctx context.Context, r slog.Record) error {
	if r.PC == 0 {
		r.PC = callerFromPC()
	}
	return a.inner.Handle(ctx, r)
}

// callerFromPC returns the program counter of the logging call site.
func callerFromPC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	return pcs[0]
}

// dedupeNonEmpty removes empty strings from a slice, preserving order.
func dedupeNonEmpty(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// WithCaller returns a slog HandlerOption that adds the caller to log
// records.
func WithCaller(enabled bool) slog.HandlerOptions {
	return slog.HandlerOptions{AddSource: enabled}
}
