package ipsec

import (
	enginelogger "github.com/iniwex5/vowifi-go/engine/logger"
	"go.uber.org/zap"
)

func logDebug(message string, fields ...zap.Field) { enginelogger.Debug(message, fields...) }
func logInfo(message string, fields ...zap.Field)  { enginelogger.Info(message, fields...) }
func logWarn(message string, fields ...zap.Field)  { enginelogger.Warn(message, fields...) }
