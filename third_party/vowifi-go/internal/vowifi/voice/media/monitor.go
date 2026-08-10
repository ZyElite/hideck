package media

import "time"

const (
	oneWayIMSToLAN = "IMS->LAN"
	oneWayLANToIMS = "LAN->IMS"
)

func NewRTPMonitor() *RTPMonitor {
	return &RTPMonitor{stopMonitor: make(chan struct{})}
}

func (r *RTPRelay) monitorSnapshot() *RTPMonitor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Monitor
}

func (m *RTPMonitor) UpdateIMS() {
	if m == nil {
		return
	}
	now := time.Now().UnixNano()
	m.LastActivity.Store(now)
	m.LastIMSToLAN.Store(now)
	m.imsCount.Add(1)
}

func (m *RTPMonitor) UpdateLAN() {
	if m == nil {
		return
	}
	now := time.Now().UnixNano()
	m.LastActivity.Store(now)
	m.LastLANToIMS.Store(now)
	m.lanCount.Add(1)
}

// EnableMonitor accepts the original timeout/callback pair; no arguments
// retains the additive default monitor API.
func (r *RTPRelay) EnableMonitor(args ...any) {
	if r == nil {
		return
	}
	timeout := 10 * time.Second
	var onTimeout func()
	if len(args) > 0 {
		switch value := args[0].(type) {
		case int64:
			timeout = time.Duration(value)
		case time.Duration:
			timeout = value
		}
	}
	if len(args) > 1 {
		onTimeout, _ = args[1].(func())
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	r.mu.Lock()
	if r.isStopped() {
		r.mu.Unlock()
		return
	}
	if r.Monitor == nil {
		r.Monitor = NewRTPMonitor()
	}
	monitor := r.Monitor
	monitor.mu.Lock()
	monitor.Timeout = int64(timeout)
	monitor.OnTimeout = onTimeout
	monitor.mu.Unlock()
	now := time.Now().UnixNano()
	monitor.LastActivity.Store(now)
	monitor.LastIMSToLAN.Store(now)
	monitor.LastLANToIMS.Store(now)
	start := !r.monitorStarted
	r.monitorStarted = true
	if start {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.monitorLoop(monitor)
		}()
	}
	r.mu.Unlock()
}

// SetOneWayTimeoutHandler accepts the original direction callback and the
// additive no-argument callback.
func (r *RTPRelay) SetOneWayTimeoutHandler(handler any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.Monitor == nil {
		r.Monitor = NewRTPMonitor()
	}
	r.Monitor.mu.Lock()
	switch callback := handler.(type) {
	case func(string):
		r.Monitor.OnOneWayTimeout = callback
	case func():
		r.Monitor.OnOneWayTimeout = func(string) { callback() }
	}
	r.Monitor.mu.Unlock()
	r.mu.Unlock()
}

func (r *RTPRelay) monitorLoop(monitor *RTPMonitor) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-monitor.stopMonitor:
			return
		case <-ticker.C:
			checkMediaTimeouts(monitor, time.Now())
		}
	}
}

func checkMediaTimeouts(monitor *RTPMonitor, now time.Time) {
	if monitor == nil {
		return
	}
	monitor.mu.RLock()
	timeout := time.Duration(monitor.Timeout)
	onTimeout := monitor.OnTimeout
	monitor.mu.RUnlock()
	if timeout <= 0 {
		return
	}
	lastActivity := monitor.LastActivity.Load()
	if lastActivity > 0 && now.Sub(time.Unix(0, lastActivity)) >= timeout {
		if onTimeout != nil {
			go onTimeout()
		}
		return
	}
	monitor.checkOneWay(now, timeout)
}

func (m *RTPMonitor) checkOneWay(now time.Time, timeout time.Duration) {
	imsIdle := idleFor(now, m.LastIMSToLAN.Load())
	lanIdle := idleFor(now, m.LastLANToIMS.Load())
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkDirection(imsIdle >= timeout && lanIdle < timeout,
		&m.imsToLanTimeoutTriggered, oneWayIMSToLAN)
	m.checkDirection(lanIdle >= timeout && imsIdle < timeout,
		&m.lanToImsTimeoutTriggered, oneWayLANToIMS)
}

func (m *RTPMonitor) checkDirection(timedOut bool, triggered *bool, direction string) {
	if !timedOut {
		*triggered = false
		return
	}
	if *triggered {
		return
	}
	*triggered = true
	if m.OnOneWayTimeout != nil {
		go m.OnOneWayTimeout(direction)
	}
}

func idleFor(now time.Time, timestamp int64) time.Duration {
	if timestamp == 0 {
		return time.Duration(1<<63 - 1)
	}
	return now.Sub(time.Unix(0, timestamp))
}

func (m *RTPMonitor) stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopMonitor) })
}

// OneWay retains the additive synchronous query.
func (m *RTPMonitor) OneWay(timeout time.Duration) bool {
	if m == nil || m.imsCount.Load() == 0 || m.lanCount.Load() == 0 {
		return false
	}
	now := time.Now()
	imsIdle := idleFor(now, m.LastIMSToLAN.Load())
	lanIdle := idleFor(now, m.LastLANToIMS.Load())
	return (imsIdle >= timeout) != (lanIdle >= timeout)
}

func (m *RTPMonitor) Counts() (uint64, uint64) {
	if m == nil {
		return 0, 0
	}
	return m.imsCount.Load(), m.lanCount.Load()
}
