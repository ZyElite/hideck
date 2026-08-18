package device

import (
	"strings"
	"time"
)

const (
	udevQuietAdd       = 400 * time.Millisecond
	udevMaxAdd         = 1200 * time.Millisecond
	udevQuietRemove    = 200 * time.Millisecond
	udevMaxRemove      = 600 * time.Millisecond
	udevRescanCooldown = 300 * time.Millisecond
)

type udevEventKind int

const (
	udevEventNone udevEventKind = iota
	udevEventAdd
	udevEventRemove
)

func parseUdevEventKind(data []byte) udevEventKind {
	s := string(data)
	hasAdd := strings.Contains(s, "ACTION=add")
	hasRemove := strings.Contains(s, "ACTION=remove")
	switch {
	case hasRemove && !hasAdd:
		return udevEventRemove
	case hasAdd && !hasRemove:
		return udevEventAdd
	default:
		return udevEventNone
	}
}

func udevEventHasControlPath(data []byte) bool {
	s := string(data)
	if strings.Contains(s, "SUBSYSTEM=usbmisc") && strings.Contains(s, "cdc-wdm") {
		return true
	}
	if strings.Contains(s, "SUBSYSTEM=wwan") && (strings.Contains(s, "qmi") || strings.Contains(s, "cdc-wdm")) {
		return true
	}
	return false
}

func udevDebounceWindows(kind udevEventKind) (quiet, max time.Duration) {
	if kind == udevEventRemove {
		return udevQuietRemove, udevMaxRemove
	}
	return udevQuietAdd, udevMaxAdd
}

func udevShouldFire(waveStart, lastEvent, now time.Time, kind udevEventKind) bool {
	if kind == udevEventNone || waveStart.IsZero() || lastEvent.IsZero() {
		return false
	}
	quiet, max := udevDebounceWindows(kind)
	quietAt := lastEvent.Add(quiet)
	maxAt := waveStart.Add(max)
	fireAt := quietAt
	if maxAt.Before(fireAt) {
		fireAt = maxAt
	}
	return !now.Before(fireAt)
}

func udevSwallowAfterFire(firedAt, now time.Time, firedKind, incoming udevEventKind, incomingControl bool) bool {
	if firedAt.IsZero() || incoming == udevEventNone || incoming != firedKind {
		return false
	}
	// ttyUSB 安静后可能已经扫过；晚到的 cdc-wdm/qmi 必须再开一轮，否则扫描看不到控制口。
	if incoming == udevEventAdd && incomingControl {
		return false
	}
	return now.Before(firedAt.Add(udevRescanCooldown))
}
