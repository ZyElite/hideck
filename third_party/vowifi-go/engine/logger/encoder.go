package logger

import (
	"strings"

	"go.uber.org/zap/zapcore"
)

const (
	ansiMagenta   = "\x1b[35m"
	ansiBlue      = "\x1b[34m"
	ansiYellow    = "\x1b[33m"
	ansiRed       = "\x1b[31m"
	ansiBrightRed = "\x1b[31;1m"
	ansiReset     = "\x1b[0m"
	levelWidth    = 5
)

func fixedWidthColorLevelEncoder(level zapcore.Level, encoder zapcore.PrimitiveArrayEncoder) {
	label := level.CapitalString()
	if len(label) < levelWidth {
		label += strings.Repeat(" ", levelWidth-len(label))
	}
	switch level {
	case zapcore.DebugLevel:
		label = ansiMagenta + label + ansiReset
	case zapcore.InfoLevel:
		label = ansiBlue + label + ansiReset
	case zapcore.WarnLevel:
		label = ansiYellow + label + ansiReset
	case zapcore.ErrorLevel:
		label = ansiRed + label + ansiReset
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		label = ansiBrightRed + label + ansiReset
	}
	encoder.AppendString(label)
}
