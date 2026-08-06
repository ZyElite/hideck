package media

import (
	"errors"
	"net"
	"sync"
	"time"
)

// NewRTPRelay creates a relay bound to the given IMS and LAN packet conns.
func NewRTPRelay(imsConn, lanConn net.PacketConn) *RTPRelay {
	return &RTPRelay{
		imsConn:      imsConn,
		lanConn:      lanConn,
		ptMapping:    make(map[int]int),
		monitor:      NewRTPMonitor(),
		oneWayTimeout: 10 * time.Second,
		stop:         make(chan struct{}),
	}
}

// NewRTPRelayWithListener creates a relay, listening on the IMS side.
func NewRTPRelayWithListener(imsLocal *net.UDPAddr) (*RTPRelay, error) {
	conn, err := net.ListenUDP("udp", imsLocal)
	if err != nil {
		return nil, err
	}
	return NewRTPRelay(conn, nil), nil
}

// SetClientAddr sets the LAN-side remote address.
func (r *RTPRelay) SetClientAddr(addr *net.UDPAddr) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.lanRemote = addr
	r.mu.Unlock()
}

// SetRemoteAddr sets the IMS-side remote address.
func (r *RTPRelay) SetRemoteAddr(addr *net.UDPAddr) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.imsRemote = addr
	r.mu.Unlock()
}

// SetPTMapping sets the LAN->IMS payload type mapping.
func (r *RTPRelay) SetPTMapping(m map[int]int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.ptMapping = make(map[int]int, len(m))
	for k, v := range m {
		r.ptMapping[k] = v
	}
	r.mu.Unlock()
}

// SetOneWayTimeoutHandler sets the one-way media timeout callback.
func (r *RTPRelay) SetOneWayTimeoutHandler(fn func()) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onOneWay = fn
	r.mu.Unlock()
}

// SetLogContext sets the log context string.
func (r *RTPRelay) SetLogContext(ctx string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.logContext = ctx
	r.mu.Unlock()
}

// IMSPort returns the IMS-side local port.
func (r *RTPRelay) IMSPort() int {
	if r == nil || r.imsConn == nil {
		return 0
	}
	return r.imsConn.LocalAddr().(*net.UDPAddr).Port
}

// LANPort returns the LAN-side local port.
func (r *RTPRelay) LANPort() int {
	if r == nil || r.lanConn == nil {
		return 0
	}
	return r.lanConn.LocalAddr().(*net.UDPAddr).Port
}

// GetIMSConnAndRemote returns the IMS conn and remote address.
func (r *RTPRelay) GetIMSConnAndRemote() (net.PacketConn, *net.UDPAddr) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.imsConn, r.imsRemote
}

// EnableMonitor enables the RTP monitor.
func (r *RTPRelay) EnableMonitor() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.monitor = NewRTPMonitor()
	r.mu.Unlock()
}

// Start launches the relay loops.
func (r *RTPRelay) Start() error {
	if r == nil {
		return errors.New("media: nil relay")
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return errors.New("media: relay stopped")
	}
	r.stop = make(chan struct{})
	r.mu.Unlock()

	var wg sync.WaitGroup
	if r.imsConn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.loopIMS()
		}()
	}
	if r.lanConn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.loopLAN()
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.monitorLoop()
	}()
	return nil
}

// Stop shuts the relay down.
func (r *RTPRelay) Stop() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return nil
	}
	r.stopped = true
	close(r.stop)
	ims, lan := r.imsConn, r.lanConn
	r.mu.Unlock()
	if ims != nil {
		_ = ims.Close()
	}
	if lan != nil {
		_ = lan.Close()
	}
	return nil
}

// shouldStop reports whether the relay is stopping.
func (r *RTPRelay) shouldStop() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	select {
	case <-r.stop:
		return true
	default:
		return false
	}
}

// loopIMS relays packets from the IMS side to the LAN side.
func (r *RTPRelay) loopIMS() {
	buf := make([]byte, 2048)
	for {
		if r.shouldStop() {
			return
		}
		n, addr, err := r.imsConn.ReadFrom(buf)
		if err != nil {
			if isRTPRelayReadClosedError(err) {
				return
			}
			continue
		}
		r.mu.RLock()
		lan := r.lanConn
		lanRemote := r.lanRemote
		r.mu.RUnlock()
		if lan == nil || lanRemote == nil {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		_, _ = lan.WriteTo(pkt, lanRemote)
		r.monitor.UpdateIMS()
		_ = addr
	}
}

// loopLAN relays packets from the LAN side to the IMS side.
func (r *RTPRelay) loopLAN() {
	buf := make([]byte, 2048)
	for {
		if r.shouldStop() {
			return
		}
		n, _, err := r.lanConn.ReadFrom(buf)
		if err != nil {
			if isRTPRelayReadClosedError(err) {
				return
			}
			continue
		}
		r.mu.RLock()
		ims := r.imsConn
		imsRemote := r.imsRemote
		r.mu.RUnlock()
		if ims == nil || imsRemote == nil {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		_, _ = ims.WriteTo(pkt, imsRemote)
		r.monitor.UpdateLAN()
	}
}

// monitorLoop watches for one-way media timeouts.
func (r *RTPRelay) monitorLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			if r.monitor.OneWay(r.oneWayTimeout) {
				r.mu.RLock()
				fn := r.onOneWay
				r.mu.RUnlock()
				if fn != nil {
					fn()
				}
			}
		}
	}
}

// isRTPRelayReadClosedError reports whether err is a closed-conn read error.
func isRTPRelayReadClosedError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, net.ErrClosed)
}

// isRTPRelayReadTimeout reports whether err is a read timeout.
func isRTPRelayReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

// NewRTPMonitor creates an RTP monitor.
func NewRTPMonitor() *RTPMonitor {
	return &RTPMonitor{}
}

// UpdateIMS records an IMS-side packet.
func (m *RTPMonitor) UpdateIMS() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.imsCount++
	m.lastIMSPkt = time.Now()
	m.mu.Unlock()
}

// UpdateLAN records a LAN-side packet.
func (m *RTPMonitor) UpdateLAN() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.lanCount++
	m.lastLANPkt = time.Now()
	m.mu.Unlock()
}

// OneWay reports whether media has been one-way for the given duration.
func (m *RTPMonitor) OneWay(timeout time.Duration) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	imsIdle := now.Sub(m.lastIMSPkt)
	lanIdle := now.Sub(m.lastLANPkt)
	if m.imsCount == 0 || m.lanCount == 0 {
		return false
	}
	if imsIdle > timeout && lanIdle > timeout {
		return false
	}
	if imsIdle > timeout || lanIdle > timeout {
		if m.oneWaySince.IsZero() {
			m.oneWaySince = now
		}
		return now.Sub(m.oneWaySince) >= timeout
	}
	m.oneWaySince = time.Time{}
	return false
}

// Counts returns the packet counts.
func (m *RTPMonitor) Counts() (ims, lan uint64) {
	if m == nil {
		return 0, 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.imsCount, m.lanCount
}
