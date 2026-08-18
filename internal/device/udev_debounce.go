package device

import (
	"strings"
	"sync"
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

type udevDebounceTiming struct {
	addQuiet    time.Duration
	addMax      time.Duration
	removeQuiet time.Duration
	removeMax   time.Duration
}

var defaultUdevDebounceTiming = udevDebounceTiming{
	addQuiet:    udevQuietAdd,
	addMax:      udevMaxAdd,
	removeQuiet: udevQuietRemove,
	removeMax:   udevMaxRemove,
}

type udevRescanScheduler struct {
	mu             sync.Mutex
	timing         udevDebounceTiming
	fire           func()
	stopped        bool
	waveKind       udevEventKind
	waveSawControl bool
	waveGen        uint64
	quietTimer     *time.Timer
	maxTimer       *time.Timer
	firedAt        time.Time
	firedKind      udevEventKind
	rescanRunning  bool
	rescanPending  bool
}

func newUdevRescanScheduler(fire func()) *udevRescanScheduler {
	return newUdevRescanSchedulerWithTiming(defaultUdevDebounceTiming, fire)
}

func newUdevRescanSchedulerWithTiming(timing udevDebounceTiming, fire func()) *udevRescanScheduler {
	return &udevRescanScheduler{timing: timing, fire: fire}
}

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

func (t udevDebounceTiming) windows(kind udevEventKind) (quiet, max time.Duration) {
	if kind == udevEventRemove {
		return t.removeQuiet, t.removeMax
	}
	return t.addQuiet, t.addMax
}

func (s *udevRescanScheduler) Schedule(kind udevEventKind, controlReady bool) {
	if kind == udevEventNone {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}

	now := time.Now()
	if udevSwallowAfterFire(s.firedAt, now, s.firedKind, kind, controlReady) {
		return
	}
	if s.waveKind != udevEventNone && kind != s.waveKind {
		s.stopWaveLocked()
	}
	if s.waveKind == udevEventNone {
		s.startWaveLocked(kind, controlReady)
		return
	}

	if controlReady {
		s.waveSawControl = true
	}
	if kind == udevEventAdd && !s.waveSawControl {
		return
	}
	s.resetQuietTimerLocked(kind)
}

func (s *udevRescanScheduler) startWaveLocked(kind udevEventKind, controlReady bool) {
	quiet, max := s.timing.windows(kind)
	s.waveKind = kind
	s.waveSawControl = controlReady
	s.waveGen++
	gen := s.waveGen
	s.maxTimer = time.AfterFunc(max, func() { s.tryFire(gen) })
	if kind == udevEventRemove || controlReady {
		s.quietTimer = time.AfterFunc(quiet, func() { s.tryFire(gen) })
	}
}

func (s *udevRescanScheduler) resetQuietTimerLocked(kind udevEventKind) {
	if s.quietTimer != nil {
		s.quietTimer.Stop()
	}
	quiet, _ := s.timing.windows(kind)
	gen := s.waveGen
	s.quietTimer = time.AfterFunc(quiet, func() { s.tryFire(gen) })
}

func (s *udevRescanScheduler) tryFire(gen uint64) {
	s.mu.Lock()
	if s.stopped || gen != s.waveGen || s.waveKind == udevEventNone {
		s.mu.Unlock()
		return
	}
	s.firedAt = time.Now()
	s.firedKind = s.waveKind
	s.stopWaveLocked()
	if s.rescanRunning {
		s.rescanPending = true
		s.mu.Unlock()
		return
	}
	s.rescanRunning = true
	s.mu.Unlock()
	s.runRescans()
}

func (s *udevRescanScheduler) runRescans() {
	for {
		if s.fire != nil {
			s.fire()
		}
		s.mu.Lock()
		if s.stopped || !s.rescanPending {
			s.rescanRunning = false
			s.rescanPending = false
			s.mu.Unlock()
			return
		}
		s.rescanPending = false
		s.mu.Unlock()
	}
}

func (s *udevRescanScheduler) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.rescanPending = false
	s.stopWaveLocked()
	s.mu.Unlock()
}

func (s *udevRescanScheduler) stopWaveLocked() {
	if s.quietTimer != nil {
		s.quietTimer.Stop()
		s.quietTimer = nil
	}
	if s.maxTimer != nil {
		s.maxTimer.Stop()
		s.maxTimer = nil
	}
	s.waveKind = udevEventNone
	s.waveSawControl = false
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
