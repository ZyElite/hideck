package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	comfortNoisePacketInterval = 20 * time.Millisecond
	comfortNoiseSamples        = 160
	comfortNoiseSSRC           = 0xdeadbeef
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
	seed := uint32(time.Now().UnixNano())
	return &ComfortNoiseGenerator{
		payloadType: 0, timestamp: seed, ssrc: comfortNoiseSSRC, randomState: seed,
		stop: make(chan struct{}), errors: make(chan error, 1),
	}
}

// Start begins generating comfort noise to addr.
func (g *ComfortNoiseGenerator) Start(conn net.PacketConn, addr *net.UDPAddr) error {
	if g == nil {
		return errors.New("media: nil noise generator")
	}
	if conn == nil || addr == nil {
		return errors.New("media: comfort-noise connection and destination are required")
	}
	g.mu.Lock()
	if g.started {
		g.mu.Unlock()
		return nil
	}
	g.conn = conn
	g.addr = addr
	g.stop = make(chan struct{})
	g.errors = make(chan error, 1)
	g.started = true
	g.wg.Add(1)
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
	if g.started {
		g.started = false
		close(g.stop)
	}
	g.mu.Unlock()
	g.wg.Wait()
}

// Errors reports the first asynchronous RTP write failure.
func (g *ComfortNoiseGenerator) Errors() <-chan error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.errors
}

// sendLoop emits comfort noise packets periodically.
func (g *ComfortNoiseGenerator) sendLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(comfortNoisePacketInterval)
	defer ticker.Stop()
	for {
		g.mu.Lock()
		stop := g.stop
		g.mu.Unlock()
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := g.sendOnePacket(); err != nil {
				g.reportError(err)
				return
			}
		}
	}
}

// sendOnePacket emits a single comfort noise packet.
func (g *ComfortNoiseGenerator) sendOnePacket() error {
	if g == nil {
		return errors.New("media: nil noise generator")
	}
	g.mu.Lock()
	conn := g.conn
	addr := g.addr
	g.mu.Unlock()
	if conn == nil || addr == nil {
		return errors.New("media: comfort-noise connection and destination are required")
	}
	_, err := conn.WriteTo(g.generateComfortNoiseUlaw(), addr)
	return err
}

func (g *ComfortNoiseGenerator) reportError(err error) {
	if err == nil {
		return
	}
	g.mu.Lock()
	errorsCh := g.errors
	g.mu.Unlock()
	select {
	case errorsCh <- fmt.Errorf("media: write PCMU RTP: %w", err):
	default:
	}
}

// generateComfortNoiseUlaw builds one 20 ms PCMU RTP packet.
func (g *ComfortNoiseGenerator) generateComfortNoiseUlaw() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	pkt := make([]byte, 12+comfortNoiseSamples)
	pkt[0] = 0x80
	pkt[1] = g.payloadType
	binary.BigEndian.PutUint16(pkt[2:4], g.sequence)
	binary.BigEndian.PutUint32(pkt[4:8], g.timestamp)
	binary.BigEndian.PutUint32(pkt[8:12], g.ssrc)
	for i := 12; i < len(pkt); i++ {
		g.randomState = g.randomState*1103515245 + 12345
		sample := int16((g.randomState>>16)&0x1ff) - 0x100
		pkt[i] = linearToUlaw(sample)
	}
	g.sequence++
	g.timestamp += comfortNoiseSamples
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
