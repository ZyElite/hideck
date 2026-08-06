//go:build !linux

package qmicore

import "strings"

// qmiControlDeviceHolder describes a process holding a QMI control device.
type qmiControlDeviceHolder struct {
	PID     int
	Command string
}

// qmiControlDeviceHolders is the set of processes holding a control device.
type qmiControlDeviceHolders struct {
	Holders []qmiControlDeviceHolder
	Unknown bool
}

// detectQMIControlDeviceHolders is a no-op on non-Linux platforms.
var detectQMIControlDeviceHolders = func(controlDevice string) (qmiControlDeviceHolders, error) {
	return qmiControlDeviceHolders{}, nil
}

// onlyQMIProxy reports whether all holders are the QMI proxy.
func (h qmiControlDeviceHolders) onlyQMIProxy() bool {
	if len(h.Holders) == 0 {
		return false
	}
	for _, holder := range h.Holders {
		cmd := strings.ToLower(strings.TrimSpace(holder.Command))
		if !strings.Contains(cmd, "qmi-proxy") {
			return false
		}
	}
	return true
}
