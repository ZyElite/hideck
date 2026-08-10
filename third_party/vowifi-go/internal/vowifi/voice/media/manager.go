package media

import (
	"errors"
	"net"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

// NewMediaSessionManager creates a manager with optional device/trace context.
func NewMediaSessionManager(context ...string) *MediaSessionManager {
	manager := &MediaSessionManager{EventCh: make(chan MediaEvent, 8), relays: map[string]*RTPRelay{}}
	if len(context) > 0 {
		manager.deviceID = context[0]
	}
	if len(context) > 1 {
		manager.traceID = context[1]
	}
	return manager
}

// CreateRelay accepts the original (IMS IP, LAN IP, timeout) form and the
// additive (call ID, IMS UDP address) form.
func (m *MediaSessionManager) CreateRelay(first string, second any, rest ...any) (*RTPRelay, error) {
	if m == nil {
		return nil, errors.New("media: nil manager")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.released {
		return nil, errors.New("media: manager released")
	}
	if address, ok := second.(*net.UDPAddr); ok && len(rest) == 0 {
		return m.createAdditiveRelay(first, address)
	}
	lanAddress, ok := second.(string)
	if !ok || len(rest) != 1 {
		return nil, errors.New("media: invalid relay creation arguments")
	}
	timeout, ok := durationArgument(rest[0])
	if !ok {
		return nil, errors.New("media: invalid media timeout")
	}
	if m.relay != nil {
		previous := m.relay
		if err := previous.StopCurrent(); err != nil {
			return nil, err
		}
		m.relay = nil
		m.removeRelayAliasesLocked(previous)
	}
	relay, err := NewRTPRelayWithListener(nil, first, lanAddress, 0, 0)
	if err != nil {
		return nil, err
	}
	m.configureRelay(relay, timeout)
	m.relay = relay
	return relay, nil
}

func (m *MediaSessionManager) removeRelayAliasesLocked(target *RTPRelay) {
	for callID, relay := range m.relays {
		if relay == target {
			delete(m.relays, callID)
		}
	}
}

func (m *MediaSessionManager) createAdditiveRelay(callID string, address *net.UDPAddr) (*RTPRelay, error) {
	if previous := m.relays[callID]; previous != nil {
		if err := previous.StopCurrent(); err != nil {
			return nil, err
		}
		delete(m.relays, callID)
		if m.relay == previous {
			m.relay = nil
		}
	}
	relay, err := NewRTPRelayWithListener(address)
	if err != nil {
		return nil, err
	}
	relay.SetLogContext(m.deviceID, m.traceID)
	m.relays[callID] = relay
	m.relay = relay
	return relay, nil
}

func durationArgument(value any) (time.Duration, bool) {
	switch duration := value.(type) {
	case time.Duration:
		return duration, true
	case int64:
		return time.Duration(duration), true
	default:
		return 0, false
	}
}

func (m *MediaSessionManager) configureRelay(relay *RTPRelay, timeout time.Duration) {
	relay.SetLogContext(m.deviceID, m.traceID)
	relay.EnableMonitor(timeout, func() { m.emit(MediaEventRTPTimeout) })
	relay.SetOneWayTimeoutHandler(func(string) { m.emit(MediaEventOneWayTimeout) })
}

func (m *MediaSessionManager) emit(event MediaEvent) {
	select {
	case m.EventCh <- event:
	default:
	}
}

// GetRelay returns the original relay or a call-keyed additive relay.
func (m *MediaSessionManager) GetRelay(callID ...string) *RTPRelay {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(callID) > 0 {
		return m.relays[callID[0]]
	}
	return m.relay
}

// Start preserves the recovered void lifecycle API.
func (m *MediaSessionManager) Start() {
	_ = m.StartCurrent()
}

// StartCurrent starts the current relays and exposes lifecycle errors.
func (m *MediaSessionManager) StartCurrent() error {
	if m == nil {
		return errors.New("media: nil manager")
	}
	m.mu.Lock()
	if m.released {
		m.mu.Unlock()
		return errors.New("media: manager released")
	}
	relays := m.relaySnapshotLocked()
	m.mu.Unlock()
	started := make([]*RTPRelay, 0, len(relays))
	for _, relay := range relays {
		if err := relay.StartCurrent(); err != nil {
			for _, active := range started {
				err = errors.Join(err, active.StopCurrent())
			}
			return err
		}
		started = append(started, relay)
	}
	return nil
}

// Release preserves the recovered void full-release API.
func (m *MediaSessionManager) Release() {
	_ = m.ReleaseCurrent()
}

// ReleaseCurrent stops one call relay or all relays and exposes errors.
func (m *MediaSessionManager) ReleaseCurrent(callID ...string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if len(callID) > 0 {
		relay := m.relays[callID[0]]
		delete(m.relays, callID[0])
		if m.relay == relay {
			m.relay = nil
		}
		m.mu.Unlock()
		return relay.StopCurrent()
	}
	if m.released {
		m.mu.Unlock()
		return nil
	}
	m.released = true
	relays := m.relaySnapshotLocked()
	m.relay = nil
	m.relays = map[string]*RTPRelay{}
	m.mu.Unlock()
	var result error
	for _, relay := range relays {
		result = errors.Join(result, relay.StopCurrent())
	}
	return result
}

func (m *MediaSessionManager) relaySnapshotLocked() []*RTPRelay {
	seen := map[*RTPRelay]struct{}{}
	result := make([]*RTPRelay, 0, len(m.relays)+1)
	if m.relay != nil {
		seen[m.relay] = struct{}{}
		result = append(result, m.relay)
	}
	for _, relay := range m.relays {
		if relay == nil {
			continue
		}
		if _, exists := seen[relay]; exists {
			continue
		}
		seen[relay] = struct{}{}
		result = append(result, relay)
	}
	return result
}

// NewBridge creates a bridge with optional device context.
func NewBridge(deviceID ...string) *Bridge {
	bridge := &Bridge{}
	if len(deviceID) > 0 {
		bridge.deviceID = deviceID[0]
	}
	return bridge
}

// SetEndpoint installs an IMS snapshot source or retains an additive string.
func (b *Bridge) SetEndpoint(endpoint any) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	switch value := endpoint.(type) {
	case imsendpoint.RuntimeSnapshotSource:
		b.endpoint = value
	case string:
		b.legacyEndpoint = value
	}
}

// IMSLocalIP returns the current IMS local address without its port.
func (b *Bridge) IMSLocalIP() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	endpoint := b.endpoint
	legacy := b.legacyEndpoint
	b.mu.RUnlock()
	if endpoint == nil {
		return legacy
	}
	address := endpoint.Snapshot().LocalAddr
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

// SetupRelay accepts one additive relay or the original
// (IMS local IP, trace ID, one-way callback) form.
func (b *Bridge) SetupRelay(args ...any) (*RTPRelay, error) {
	if b == nil {
		return nil, errors.New("media: nil bridge")
	}
	if len(args) == 1 {
		relay, ok := args[0].(*RTPRelay)
		if !ok || relay == nil {
			return nil, errors.New("media: invalid relay")
		}
		if err := b.replaceRelay(relay); err != nil {
			return nil, err
		}
		return relay, nil
	}
	if len(args) != 3 {
		return nil, errors.New("media: setup requires IMS address, trace ID and callback")
	}
	imsAddress, okIMS := args[0].(string)
	traceID, okTrace := args[1].(string)
	callback, okCallback := args[2].(func(string))
	if !okIMS || !okTrace || !okCallback {
		return nil, errors.New("media: invalid setup arguments")
	}
	b.mu.RLock()
	endpoint := b.endpoint
	b.mu.RUnlock()
	listener, _ := endpoint.(imsendpoint.PacketListener)
	relay, err := NewRTPRelayWithListener(listener, imsAddress, "0.0.0.0", 0, 0)
	if err != nil {
		return nil, err
	}
	relay.SetLogContext(b.deviceID, traceID)
	relay.SetOneWayTimeoutHandler(callback)
	if err := relay.StartCurrent(); err != nil {
		relay.Stop()
		return nil, err
	}
	if err := b.replaceRelay(relay); err != nil {
		return nil, errors.Join(err, relay.StopCurrent())
	}
	return relay, nil
}
