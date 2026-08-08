package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// InitFile preserves the reconstructed runtime's additive file-output
// capability without changing the legacy Init(level, format) contract.
func InitFile(level zapcore.Level, filePath string) error {
	output := os.Stderr
	if filePath != "" {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		output = file
	}
	config := zap.NewDevelopmentEncoderConfig()
	config.TimeKey = "time"
	config.EncodeLevel = fixedWidthColorLevelEncoder
	config.EncodeTime = zapcore.TimeEncoderOfLayout(consoleTimeLayout)
	core := zapcore.NewCore(zapcore.NewConsoleEncoder(config), zapcore.AddSync(output), level)
	base := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	global.Store(base)
	globalSugar.Store(base.Sugar())
	return nil
}

// AddCallerSkip returns the corresponding zap option.
func AddCallerSkip(skip int) zap.Option { return zap.AddCallerSkip(skip) }

// AddStacktrace returns the corresponding zap option.
func AddStacktrace(level zapcore.LevelEnabler) zap.Option { return zap.AddStacktrace(level) }

// TimeEncoderOfLayout returns a time encoder for layout.
func TimeEncoderOfLayout(layout string) zapcore.TimeEncoder {
	return zapcore.TimeEncoderOfLayout(layout)
}

// WithCaller returns the corresponding zap option.
func WithCaller(enabled bool) zap.Option { return zap.WithCaller(enabled) }
