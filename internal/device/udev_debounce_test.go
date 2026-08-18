package device

import (
	"sync/atomic"
	"testing"
	"time"
)

var fastUdevDebounceTiming = udevDebounceTiming{
	addQuiet:    10 * time.Millisecond,
	addMax:      60 * time.Millisecond,
	removeQuiet: 5 * time.Millisecond,
	removeMax:   30 * time.Millisecond,
}

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

func TestUdevSchedulerWaitsForAddCapWithoutControlPath(t *testing.T) {
	fired := make(chan struct{}, 1)
	scheduler := newUdevRescanSchedulerWithTiming(fastUdevDebounceTiming, func() { fired <- struct{}{} })
	defer scheduler.Stop()

	scheduler.Schedule(udevEventAdd, false)
	select {
	case <-fired:
		t.Fatal("tty-only add must not fire at the quiet window")
	case <-time.After(2 * fastUdevDebounceTiming.addQuiet):
	}
	waitUdevSignal(t, fired, "add wave did not fire at the maximum wait")
}

func TestUdevSchedulerFiresAfterControlPathQuietWindow(t *testing.T) {
	fired := make(chan struct{}, 1)
	scheduler := newUdevRescanSchedulerWithTiming(fastUdevDebounceTiming, func() { fired <- struct{}{} })
	defer scheduler.Stop()

	scheduler.Schedule(udevEventAdd, true)
	waitUdevSignal(t, fired, "control-path add did not fire after the quiet window")
}

func TestUdevSchedulerSerializesPendingRescan(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	completed := make(chan int32, 2)
	var calls atomic.Int32
	var active atomic.Int32
	var overlapped atomic.Bool

	scheduler := newUdevRescanSchedulerWithTiming(fastUdevDebounceTiming, func() {
		call := calls.Add(1)
		if active.Add(1) > 1 {
			overlapped.Store(true)
		}
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		active.Add(-1)
		completed <- call
	})
	defer scheduler.Stop()

	scheduler.Schedule(udevEventAdd, true)
	waitUdevSignal(t, firstStarted, "first rescan did not start")
	scheduler.Schedule(udevEventAdd, true)
	waitUdevPending(t, scheduler)
	close(releaseFirst)
	waitUdevCompletion(t, completed)
	waitUdevCompletion(t, completed)

	if overlapped.Load() {
		t.Fatal("pending udev rescans must not overlap")
	}
	if calls.Load() != 2 {
		t.Fatalf("rescan calls = %d, want 2", calls.Load())
	}
}

func TestUdevSchedulerKindChangeCancelsPreviousWave(t *testing.T) {
	fired := make(chan struct{}, 2)
	scheduler := newUdevRescanSchedulerWithTiming(fastUdevDebounceTiming, func() { fired <- struct{}{} })
	defer scheduler.Stop()

	scheduler.Schedule(udevEventAdd, false)
	scheduler.Schedule(udevEventRemove, false)
	waitUdevSignal(t, fired, "remove wave did not replace pending add wave")
	select {
	case <-fired:
		t.Fatal("canceled add wave triggered a second rescan")
	case <-time.After(2 * fastUdevDebounceTiming.addMax):
	}
}

func TestUdevSchedulerStopCancelsWave(t *testing.T) {
	fired := make(chan struct{}, 1)
	scheduler := newUdevRescanSchedulerWithTiming(fastUdevDebounceTiming, func() { fired <- struct{}{} })
	scheduler.Schedule(udevEventAdd, true)
	scheduler.Stop()

	select {
	case <-fired:
		t.Fatal("stopped scheduler must not fire a pending wave")
	case <-time.After(2 * fastUdevDebounceTiming.addMax):
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

func waitUdevSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(500 * time.Millisecond):
		t.Fatal(message)
	}
}

func waitUdevCompletion(t *testing.T, completed <-chan int32) {
	t.Helper()
	select {
	case <-completed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for rescan completion")
	}
}

func waitUdevPending(t *testing.T, scheduler *udevRescanScheduler) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		scheduler.mu.Lock()
		pending := scheduler.rescanPending
		scheduler.mu.Unlock()
		if pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("second wave was not retained as a pending rescan")
}
