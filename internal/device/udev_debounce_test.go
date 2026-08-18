package device

import (
	"testing"
	"time"
)

func TestParseUdevEventKind(t *testing.T) {
	if got := parseUdevEventKind([]byte("ACTION=add\x00SUBSYSTEM=tty")); got != udevEventAdd {
		t.Fatalf("add kind = %d, want add", got)
	}
	if got := parseUdevEventKind([]byte("ACTION=remove\x00SUBSYSTEM=usbmisc")); got != udevEventRemove {
		t.Fatalf("remove kind = %d, want remove", got)
	}
	if got := parseUdevEventKind([]byte("ACTION=change\x00SUBSYSTEM=usb")); got != udevEventNone {
		t.Fatalf("change kind = %d, want none", got)
	}
}

func TestUdevEventHasControlPath(t *testing.T) {
	if !udevEventHasControlPath([]byte("ACTION=add\x00SUBSYSTEM=usbmisc\x00DEVNAME=/dev/cdc-wdm0")) {
		t.Fatal("cdc-wdm usbmisc should count as control path")
	}
	if !udevEventHasControlPath([]byte("ACTION=add\x00SUBSYSTEM=wwan\x00DEVNAME=/dev/wwan0qmi0")) {
		t.Fatal("wwan qmi port should count as control path")
	}
	if udevEventHasControlPath([]byte("ACTION=add\x00SUBSYSTEM=tty\x00DEVNAME=/dev/ttyUSB2")) {
		t.Fatal("ttyUSB should not count as control path")
	}
}

func TestUdevShouldFireCapsAddWait(t *testing.T) {
	start := time.Unix(1000, 0)
	// A burst of ttyUSB events that would have reset a 3s timer forever.
	last := start.Add(900 * time.Millisecond)
	now := start.Add(udevMaxAdd)
	if !udevShouldFire(start, last, now, udevEventAdd) {
		t.Fatal("add wave must fire at the 1.2s cap even if events keep arriving")
	}
	if udevShouldFire(start, last, start.Add(800*time.Millisecond), udevEventAdd) {
		t.Fatal("add wave should not fire before quiet window or cap")
	}
}

func TestUdevShouldFireAfterQuietWindow(t *testing.T) {
	start := time.Unix(2000, 0)
	last := start.Add(100 * time.Millisecond)
	if !udevShouldFire(start, last, last.Add(udevQuietAdd), udevEventAdd) {
		t.Fatal("add wave should fire 400ms after the last event")
	}
	if udevShouldFire(start, last, last.Add(udevQuietAdd-time.Millisecond), udevEventAdd) {
		t.Fatal("add wave should wait the quiet window")
	}
}

func TestUdevRemoveFiresFasterThanAdd(t *testing.T) {
	start := time.Unix(3000, 0)
	last := start
	if udevShouldFire(start, last, start.Add(udevQuietRemove-time.Millisecond), udevEventRemove) {
		t.Fatal("remove should still wait its short quiet window")
	}
	if !udevShouldFire(start, last, start.Add(udevQuietRemove), udevEventRemove) {
		t.Fatal("remove should fire after 200ms quiet")
	}
	if !udevShouldFire(start, last.Add(500*time.Millisecond), start.Add(udevMaxRemove), udevEventRemove) {
		t.Fatal("remove must fire at the 600ms cap")
	}
}

func TestUdevSwallowLateAddAfterFire(t *testing.T) {
	firedAt := time.Unix(4000, 0)
	if !udevSwallowAfterFire(firedAt, firedAt.Add(100*time.Millisecond), udevEventAdd, udevEventAdd, false) {
		t.Fatal("late tty add during cooldown should be swallowed")
	}
	if udevSwallowAfterFire(firedAt, firedAt.Add(udevRescanCooldown), udevEventAdd, udevEventAdd, false) {
		t.Fatal("cooldown must end so a later replug can start a new wave")
	}
	if udevSwallowAfterFire(firedAt, firedAt.Add(50*time.Millisecond), udevEventAdd, udevEventRemove, false) {
		t.Fatal("remove after an add scan must not be swallowed")
	}
}

func TestUdevDoesNotSwallowLateControlPathAdd(t *testing.T) {
	firedAt := time.Unix(5000, 0)
	if udevSwallowAfterFire(firedAt, firedAt.Add(100*time.Millisecond), udevEventAdd, udevEventAdd, true) {
		t.Fatal("late cdc-wdm/qmi add after a tty-only scan must start a new wave")
	}
}
