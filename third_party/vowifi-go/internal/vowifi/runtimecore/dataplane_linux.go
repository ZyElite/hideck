//go:build linux

package runtimecore

import (
	"strings"

	"github.com/iniwex5/netlink"
	"go.uber.org/zap"
)

func CleanupDataplaneInterface(deviceID, tunName string) {
	tunName = strings.TrimSpace(tunName)
	if tunName == "" {
		return
	}
	link, err := netlink.LinkByName(tunName)
	if err != nil {
		return
	}
	zap.S().Infow("removing runtime dataplane interface", "device", strings.TrimSpace(deviceID), "interface", tunName)
	if err := netlink.LinkDel(link); err != nil {
		zap.S().Warnw("failed to remove runtime dataplane interface", "device", strings.TrimSpace(deviceID), "interface", tunName, "error", err)
	}
}
