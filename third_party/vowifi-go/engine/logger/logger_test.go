package logger

import (
	"io"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestInitAndLog(t *testing.T) {
	if err := Init(zapcore.DebugLevel, ""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	Debug("debug msg")
	Info("info msg", zap.String("k", "v"))
	Warn("warn msg")
	Error("error msg")
	child := With(zap.String("ctx", "test"))
	if child == nil {
		t.Error("With returned nil")
	}
}

func TestFixedWidthColorLevelEncoder(t *testing.T) {
	enc := zapcore.NewMapObjectEncoder()
	pe := zapcore.NewMapObjectEncoder()
	_ = enc
	_ = pe
	// The encoder appends to a PrimitiveArrayEncoder; exercise via a console
	// encoder to ensure no panic.
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.EncodeLevel = fixedWidthColorLevelEncoder
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(cfg.EncoderConfig),
		zapcore.AddSync(io.Discard),
		zapcore.DebugLevel,
	)
	logger := zap.New(core)
	logger.Info("test")
}
