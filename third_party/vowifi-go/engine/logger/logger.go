// Package logger is a thin zap wrapper used by the SWu session and the IMS
// stack. Reconstructed from the decompiled engine/logger.
package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	mu     sync.RWMutex
	global *zap.Logger
)

// Init configures the global logger. When filePath is non-empty the output is
// appended to that file; otherwise it goes to stderr.
func Init(level zapcore.Level, filePath string) error {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(level)
	cfg.EncoderConfig.EncodeLevel = fixedWidthColorLevelEncoder
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var core zapcore.Core
	if filePath != "" {
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		core = zapcore.NewCore(
			zapcore.NewConsoleEncoder(cfg.EncoderConfig),
			zapcore.AddSync(f),
			cfg.Level,
		)
	} else {
		core = zapcore.NewCore(
			zapcore.NewConsoleEncoder(cfg.EncoderConfig),
			zapcore.AddSync(os.Stderr),
			cfg.Level,
		)
	}
	mu.Lock()
	global = zap.New(core)
	mu.Unlock()
	return nil
}

// initLogger is the internal initialiser (defaults to Info on stderr).
func initLogger() {
	_ = Init(zapcore.InfoLevel, "")
}

// L returns the global logger, initialising it on first use.
func L() *zap.Logger {
	mu.RLock()
	l := global
	mu.RUnlock()
	if l == nil {
		initLogger()
		mu.RLock()
		l = global
		mu.RUnlock()
	}
	return l
}

// Debug logs at debug level.
func Debug(msg string, fields ...zap.Field) { L().Debug(msg, fields...) }

// Info logs at info level.
func Info(msg string, fields ...zap.Field) { L().Info(msg, fields...) }

// Warn logs at warn level.
func Warn(msg string, fields ...zap.Field) { L().Warn(msg, fields...) }

// Error logs at error level.
func Error(msg string, fields ...zap.Field) { L().Error(msg, fields...) }

// With returns a child logger with the given fields.
func With(fields ...zap.Field) *zap.Logger { return L().With(fields...) }

// fixedWidthColorLevelEncoder renders the level as a fixed-width, coloured
// token (e.g. "INFO " in green).
func fixedWidthColorLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	text := l.CapitalString()
	switch l {
	case zapcore.DebugLevel:
		text = "\x1b[36m" + text + "\x1b[0m" // cyan
	case zapcore.InfoLevel:
		text = "\x1b[32m" + text + "\x1b[0m" // green
	case zapcore.WarnLevel:
		text = "\x1b[33m" + text + "\x1b[0m" // yellow
	case zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		text = "\x1b[31m" + text + "\x1b[0m" // red
	}
	for len(text) < 5 {
		text += " "
	}
	enc.AppendString(text)
}
