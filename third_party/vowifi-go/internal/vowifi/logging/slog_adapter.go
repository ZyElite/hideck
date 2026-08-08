package logging

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	readErrorMessage       = "Read error"
	normalizedReadErrorMsg = "SIP TCP 通道读异常"
)

var warningReadErrors = [...]string{
	"connection reset by peer",
	"connection timed out",
	"i/o timeout",
	"broken pipe",
}

// SlogAdapter bridges slog records to a zap logger while preserving Record.PC.
type SlogAdapter struct {
	logger      *zap.Logger
	callerChain []string
}

type adapterEntry struct {
	level     zapcore.Level
	timestamp time.Time
	message   string
	caller    zapcore.EntryCaller
}

// NewSlogHandler returns a slog handler backed by logger.
func NewSlogHandler(logger *zap.Logger) *SlogAdapter {
	if logger == nil {
		logger = zap.L()
	}
	return &SlogAdapter{logger: logger.WithOptions(zap.WithCaller(false))}
}

// Enabled defers actual level filtering to the zap core at write time.
func (h *SlogAdapter) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle converts attributes, normalizes known SIP read errors, and writes.
func (h *SlogAdapter) Handle(_ context.Context, record slog.Record) error {
	fields := make([]zap.Field, 0, record.NumAttrs())
	callers := append(make([]string, 0, len(h.callerChain)+2), h.callerChain...)
	errText := ""
	record.Attrs(func(attr slog.Attr) bool {
		if strings.TrimSpace(attr.Key) == "" {
			return true
		}
		if attr.Key == "caller" {
			callers = appendCaller(callers, attr.Value.Any())
			return true
		}
		if attr.Key == "error" {
			errText = strings.TrimSpace(fmt.Sprint(attr.Value.Any()))
		}
		fields = append(fields, zap.Any(attr.Key, attr.Value.Any()))
		return true
	})
	fields = appendCallerFields(fields, callers)
	level, msg := normalizeReadError(record.Level, record.Message, errText)
	h.writeWithCaller(adapterEntry{
		level: toZapLevel(level), timestamp: record.Time,
		message: msg, caller: callerFromPC(record.PC),
	}, fields)
	return nil
}

// WithAttrs returns an independent handler with preset zap fields and callers.
func (h *SlogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := make([]zap.Field, 0, len(attrs))
	callers := append(make([]string, 0, len(h.callerChain)+len(attrs)), h.callerChain...)
	for _, attr := range attrs {
		if strings.TrimSpace(attr.Key) == "" {
			continue
		}
		if attr.Key == "caller" {
			callers = appendCaller(callers, attr.Value.Any())
			continue
		}
		fields = append(fields, zap.Any(attr.Key, attr.Value.Any()))
	}
	return &SlogAdapter{logger: h.logger.With(fields...), callerChain: callers}
}

// WithGroup maps a slog group to the original zap logger namespace behavior.
func (h *SlogAdapter) WithGroup(name string) slog.Handler {
	callers := append([]string(nil), h.callerChain...)
	return &SlogAdapter{logger: h.logger.Named(name), callerChain: callers}
}

func appendCaller(callers []string, value any) []string {
	if caller := strings.TrimSpace(fmt.Sprint(value)); caller != "" {
		return append(callers, caller)
	}
	return callers
}

func appendCallerFields(fields []zap.Field, callers []string) []zap.Field {
	unique := dedupeNonEmpty(callers)
	if len(unique) == 0 {
		return fields
	}
	fields = append(fields, zap.String("caller", unique[len(unique)-1]))
	if len(unique) > 1 {
		fields = append(fields, zap.String("caller_chain", strings.Join(unique, " -> ")))
	}
	return fields
}

func normalizeReadError(level slog.Level, msg, errText string) (slog.Level, string) {
	if !strings.EqualFold(strings.TrimSpace(msg), readErrorMessage) {
		return level, msg
	}
	errText = strings.ToLower(errText)
	if strings.Contains(errText, "use of closed network connection") || strings.Contains(errText, "eof") {
		return slog.LevelDebug, normalizedReadErrorMsg
	}
	for _, fragment := range warningReadErrors {
		if strings.Contains(errText, fragment) {
			return slog.LevelWarn, normalizedReadErrorMsg
		}
	}
	return level, msg
}

func dedupeNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func callerFromPC(pc uintptr) zapcore.EntryCaller {
	if pc == 0 {
		return zapcore.EntryCaller{}
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" || frame.Line <= 0 {
		return zapcore.EntryCaller{}
	}
	return zapcore.EntryCaller{
		Defined: true, PC: frame.PC, File: frame.File,
		Line: frame.Line, Function: frame.Function,
	}
}

func toZapLevel(level slog.Level) zapcore.Level {
	switch {
	case level <= slog.LevelDebug:
		return zapcore.DebugLevel
	case level < slog.LevelWarn:
		return zapcore.InfoLevel
	case level < slog.LevelError:
		return zapcore.WarnLevel
	default:
		return zapcore.ErrorLevel
	}
}

func (h *SlogAdapter) writeWithCaller(logEntry adapterEntry, fields []zap.Field) {
	entry := zapcore.Entry{
		Level: logEntry.level, Time: logEntry.timestamp,
		Message: logEntry.message, Caller: logEntry.caller,
	}
	if checked := h.logger.Core().Check(entry, nil); checked != nil {
		checked.Write(fields...)
	}
}

// WithCaller preserves the current additive slog handler option helper.
func WithCaller(enabled bool) slog.HandlerOptions {
	return slog.HandlerOptions{AddSource: enabled}
}
