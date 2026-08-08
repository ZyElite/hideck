// Package logger provides the process-wide logger used by the protocol engine.
package logger

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultLevel        = "info"
	defaultFormat       = "console"
	jsonFormat          = "json"
	consoleTimeLayout   = "[2006-01-02 15:04:05]"
	consoleCallerWidth  = 28
	logWrapperCallerGap = 1
)

var (
	initOnce    sync.Once
	global      atomic.Pointer[zap.Logger]
	globalSugar atomic.Pointer[zap.SugaredLogger]
)

// Init initializes the process logger once. Unknown levels use info and every
// format other than "json" uses the console encoder.
func Init(level, format string) error {
	var initErr error
	initOnce.Do(func() {
		initErr = initLogger(level, format)
	})
	return initErr
}

func initLogger(level, format string) error {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	if format == jsonFormat {
		encoderConfig = zap.NewProductionEncoderConfig()
	} else {
		encoderConfig.EncodeLevel = fixedWidthColorLevelEncoder
		encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(consoleTimeLayout)
		encoderConfig.EncodeCaller = func(caller zapcore.EntryCaller, encoder zapcore.PrimitiveArrayEncoder) {
			path := caller.TrimmedPath()
			if len(path) < consoleCallerWidth {
				path += strings.Repeat(" ", consoleCallerWidth-len(path))
			}
			encoder.AppendString(path)
		}
		encoderConfig.ConsoleSeparator = " "
	}
	encoderConfig.TimeKey = "time"
	if format == jsonFormat {
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	encoder := zapcore.NewConsoleEncoder(encoderConfig)
	if format == jsonFormat {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}
	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stderr), parseLevel(level))
	base := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	global.Store(base)
	globalSugar.Store(base.Sugar())
	return nil
}

func parseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// L returns the initialized process logger.
func L() *zap.Logger {
	if logger := global.Load(); logger != nil {
		return logger
	}
	if err := Init(defaultLevel, defaultFormat); err != nil {
		panic("engine/logger: initialize: " + err.Error())
	}
	logger := global.Load()
	if logger == nil {
		panic("engine/logger: initialization completed without a logger")
	}
	return logger
}

// Debug logs at debug level.
func Debug(message string, fields ...zap.Field) {
	L().WithOptions(zap.AddCallerSkip(logWrapperCallerGap)).Debug(message, fields...)
}

// Info logs at info level.
func Info(message string, fields ...zap.Field) {
	L().WithOptions(zap.AddCallerSkip(logWrapperCallerGap)).Info(message, fields...)
}

// Warn logs at warn level.
func Warn(message string, fields ...zap.Field) {
	L().WithOptions(zap.AddCallerSkip(logWrapperCallerGap)).Warn(message, fields...)
}

// Error logs at error level.
func Error(message string, fields ...zap.Field) {
	L().WithOptions(zap.AddCallerSkip(logWrapperCallerGap)).Error(message, fields...)
}

// With returns a child logger with the supplied structured fields.
func With(fields ...zap.Field) *zap.Logger {
	return L().With(fields...)
}
