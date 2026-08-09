//go:build !linux

package runtimecore

import (
	"strings"

	"go.uber.org/zap"
)

func CleanupDataplaneInterface(deviceID, tunName string) {
	if strings.TrimSpace(tunName) == "" {
		return
	}
	zap.S().Warnw("dataplane interface cleanup is unavailable on this platform",
		"device", strings.TrimSpace(deviceID), "interface", strings.TrimSpace(tunName))
}
