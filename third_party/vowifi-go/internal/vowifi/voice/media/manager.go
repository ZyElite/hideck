package media

import (
	"errors"
	"net"
	"time"
)

// NewMediaSessionManager creates a media session manager.
func NewMediaSessionManager() *MediaSessionManager {
	return &MediaSessionManager{relays: make(map[string]*RTPRelay)}
}

// CreateRelay creates and stores a relay for a call.
func (m *MediaSessionManager) CreateRelay(callID string, imsLocal *net.UDPAddr) (*RTPRelay, error) {
	if m == nil {
		return nil, errors.New("media: nil manager")
	}
	r, err := NewRTPRelayWithListener(imsLocal)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.relays[callID] = r
	m.mu.Unlock()
	return r, nil
}

// GetRelay returns the relay for a call.
func (m *MediaSessionManager) GetRelay(callID string) *RTPRelay {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.relays[callID]
}

// Release removes and stops the relay for a call.
func (m *MediaSessionManager) Release(callID string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	r := m.relays[callID]
	delete(m.relays, callID)
	m.mu.Unlock()
	if r != nil {
		return r.Stop()
	}
	return nil
}

// Start starts all relays.
func (m *MediaSessionManager) Start() error {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.relays {
		if err := r.Start(); err != nil {
			return err
		}
	}
	return nil
}

// NewBridge creates a media bridge.
func NewBridge() *Bridge {
	return &Bridge{}
}

// SetEndpoint sets the client endpoint.
func (b *Bridge) SetEndpoint(ep string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.endpoint = ep
	b.mu.Unlock()
}

// SetupRelay attaches a relay to the bridge.
func (b *Bridge) SetupRelay(r *RTPRelay) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.relay = r
	b.mu.Unlock()
}

// IMSLocalIP returns the IMS-side local IP.
func (b *Bridge) IMSLocalIP() net.IP {
	if b == nil || b.relay == nil {
		return nil
	}
	conn, _ := b.relay.GetIMSConnAndRemote()
	if conn == nil {
		return nil
	}
	if ua, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return ua.IP
	}
	return nil
}

// NewComfortNoiseGenerator creates a comfort noise generator.
func NewComfortNoiseGenerator() *ComfortNoiseGenerator {
	return &ComfortNoiseGenerator{stop: make(chan struct{})}
}

// Start begins generating comfort noise to addr.
func (g *ComfortNoiseGenerator) Start(conn net.PacketConn, addr *net.UDPAddr) error {
	if g == nil {
		return errors.New("media: nil noise generator")
	}
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return nil
	}
	g.conn = conn
	g.addr = addr
	g.stop = make(chan struct{})
	g.started = true
	g.mu.Unlock()
	go g.sendLoop()
	return nil
}

// Stop halts comfort noise generation.
func (g *ComfortNoiseGenerator) Stop() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if !g.started {
		g.mu.Unlock()
		return
	}
	g.started = false
	close(g.stop)
	g.mu.Unlock()
}

// sendLoop emits comfort noise packets periodically.
func (g *ComfortNoiseGenerator) sendLoop() {
	for {
		g.mu.Lock()
		stop := g.stop
		conn := g.conn
		addr := g.addr
		g.mu.Unlock()
		select {
		case <-stop:
			return
		default:
		}
		if conn != nil && addr != nil {
			pkt := g.generateComfortNoiseUlaw()
			_, _ = conn.WriteTo(pkt, addr)
		}
		select {
		case <-stop:
			return
		case <-timeAfter(20 * time.Millisecond):
		}
	}
}

// sendOnePacket emits a single comfort noise packet.
func (g *ComfortNoiseGenerator) sendOnePacket() {
	if g == nil {
		return
	}
	g.mu.Lock()
	conn := g.conn
	addr := g.addr
	g.mu.Unlock()
	if conn != nil && addr != nil {
		_, _ = conn.WriteTo(g.generateComfortNoiseUlaw(), addr)
	}
}

// generateComfortNoiseUlaw builds a comfort noise RTP packet (RFC 3389,
// payload type 13, u-law).
func (g *ComfortNoiseGenerator) generateComfortNoiseUlaw() []byte {
	// RTP header (12 bytes) + CN payload (1 byte level).
	pkt := make([]byte, 13)
	pkt[0] = 0x80 // version 2, no padding, no extension
	pkt[1] = 13   // comfort noise PT
	pkt[2] = 0    // sequence high
	pkt[3] = 0    // sequence low
	pkt[4] = 0    // timestamp high
	pkt[5] = 0
	pkt[6] = 0
	pkt[7] = 0
	pkt[8] = 0 // SSRC
	pkt[9] = 0
	pkt[10] = 0
	pkt[11] = 0
	pkt[12] = 0x20 // noise level (approx -40 dBov)
	return pkt
}

// linearToUlaw converts a 16-bit linear PCM sample to u-law (G.711).
func linearToUlaw(sample int16) byte {
	// Standard u-law encoding.
	const (
		biases = 0x84
		clip   = 32635
	)
	sign := byte(0)
	if sample < 0 {
		sample = -sample
		sign = 0x80
	}
	if sample > clip {
		sample = clip
	}
	sample += biases
	exp := 7
	for seg := int16(0x4000); seg > 0; seg >>= 1 {
		if sample&seg != 0 {
			break
		}
		exp--
	}
	mantissa := byte((sample >> (exp + 3)) & 0x0F)
	ulaw := ^(sign | byte(exp)<<4 | mantissa)
	return ulaw
}
