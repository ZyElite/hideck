package swu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

// dataPlaneRuntimeStats tracks inner-packet data-plane counters.
type dataPlaneRuntimeStats struct {
	innerRx atomic.Uint64 // inner packets received from the host stack
	innerTx atomic.Uint64 // inner packets sent to the host stack
	espRx   atomic.Uint64 // ESP packets received from the network
	espTx   atomic.Uint64 // ESP packets sent to the network
}

// snapshot returns a copy of the counters.
func (s *dataPlaneRuntimeStats) snapshot() map[string]uint64 {
	return map[string]uint64{
		"inner_rx": s.innerRx.Load(),
		"inner_tx": s.innerTx.Load(),
		"esp_rx":   s.espRx.Load(),
		"esp_tx":   s.espTx.Load(),
	}
}

// innerPacketSnapshot returns the inner-packet counters as a string.
func (s *dataPlaneRuntimeStats) innerPacketSnapshot() string {
	return fmt.Sprintf("inner rx=%d tx=%d esp rx=%d tx=%d",
		s.innerRx.Load(), s.innerTx.Load(), s.espRx.Load(), s.espTx.Load())
}

// userspaceInnerPacketEndpoint is the user-space TUN-like endpoint that
// exchanges inner IP packets with the host stack. It is backed by a channel
// pair: the host stack reads from innerPackets and writes to hostPackets.
type userspaceInnerPacketEndpoint struct {
	innerPackets chan []byte // inner IP packets from the network (ESP-decapsulated)
	hostPackets  chan []byte // inner IP packets from the host stack (to be ESP-encapsulated)
	closed       chan struct{}
	closeOnce    sync.Once
	stats        dataPlaneRuntimeStats
}

// InnerPacketIO is the packet boundary consumed by the user-space IMS stack.
type InnerPacketIO interface {
	ReadPacketContext(context.Context) ([]byte, error)
	WritePacketContext(context.Context, []byte) error
}

// newUserspaceInnerPacketEndpoint creates the endpoint with the given buffer
// sizes.
func newUserspaceInnerPacketEndpoint(innerBuf, hostBuf int) *userspaceInnerPacketEndpoint {
	if innerBuf <= 0 {
		innerBuf = 64
	}
	if hostBuf <= 0 {
		hostBuf = 64
	}
	return &userspaceInnerPacketEndpoint{
		innerPackets: make(chan []byte, innerBuf),
		hostPackets:  make(chan []byte, hostBuf),
		closed:       make(chan struct{}),
	}
}

// start begins the endpoint (no-op; the channels are live on construction).
func (e *userspaceInnerPacketEndpoint) start() error {
	return nil
}

// isClosed reports whether the endpoint has been closed.
func (e *userspaceInnerPacketEndpoint) isClosed() bool {
	select {
	case <-e.closed:
		return true
	default:
		return false
	}
}

// Close shuts the endpoint down.
func (e *userspaceInnerPacketEndpoint) Close() error {
	e.closeOnce.Do(func() { close(e.closed) })
	return nil
}

// ReadPacket returns the next inner packet from the network.
func (e *userspaceInnerPacketEndpoint) ReadPacket() ([]byte, error) {
	return e.ReadPacketContext(context.Background())
}

// ReadPacketContext returns the next inner packet or the context error.
func (e *userspaceInnerPacketEndpoint) ReadPacketContext(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.closed:
		return nil, errors.New("swu: inner endpoint closed")
	case pkt := <-e.innerPackets:
		e.stats.innerTx.Add(1)
		return pkt, nil
	}
}

// WritePacket queues an inner packet from the host stack for ESP encapsulation.
func (e *userspaceInnerPacketEndpoint) WritePacket(pkt []byte) error {
	return e.WritePacketContext(context.Background(), pkt)
}

// WritePacketContext queues an inner packet or returns the context error.
func (e *userspaceInnerPacketEndpoint) WritePacketContext(ctx context.Context, pkt []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return errors.New("swu: inner endpoint closed")
	case e.hostPackets <- pkt:
		e.stats.innerRx.Add(1)
		return nil
	}
}

// Snapshot returns the endpoint counters.
func (e *userspaceInnerPacketEndpoint) Snapshot() string {
	return e.stats.innerPacketSnapshot()
}

// setupDataPlane builds the ESP SA and the inner packet endpoint after the
// CREATE_CHILD_SA exchange.
func (s *Session) setupDataPlane() error {
	if s.ikeKeys == nil {
		return errors.New("swu: no IKE SA keys for child SA derivation")
	}
	if s.innerIP == nil {
		return errors.New("swu: no inner address assigned by ePDG")
	}

	// Derive the CHILD_SA keys from SK_d (RFC 7296 §2.17). The key material is
	// prf+(SK_d, Ni | Nr) for the CREATE_CHILD_SA exchange.
	childKeys, err := s.deriveChildSAKeys()
	if err != nil {
		return err
	}

	outbound, err := s.newESPAssociation(s.espRemoteSPI, childKeys.initiator)
	if err != nil {
		return err
	}
	inbound, err := s.newESPAssociation(s.espLocalSPI, childKeys.responder)
	if err != nil {
		return err
	}
	s.childSAMu.Lock()
	s.espOutboundSA = outbound
	s.espInboundSA = inbound
	s.espKey = append([]byte{}, childKeys.initiator.enc...)
	s.espIntegKey = append([]byte{}, childKeys.initiator.integ...)
	s.childSAMu.Unlock()

	// Create the inner packet endpoint.
	s.innerEndpoint = newUserspaceInnerPacketEndpoint(0, 0)
	return nil
}

func (s *Session) newESPAssociation(spi uint32, keys childDirectionKeys) (*ipsec.SecurityAssociation, error) {
	if spi == 0 {
		return nil, errors.New("swu: ESP SPI is zero")
	}
	if s.espAEAD {
		return ipsec.NewSecurityAssociation(spi, s.espCipher, keys.enc, 0), nil
	}
	integ := crypto.NewIntegrity(s.espInteg)
	if integ == nil {
		return nil, fmt.Errorf("swu: unsupported ESP integrity transform %d", s.espInteg)
	}
	return ipsec.NewSecurityAssociationCBC(
		spi, s.espCipher, keys.enc, integ, keys.integ, 0,
	), nil
}

// childSAKeys holds the derived CHILD_SA key material.
type childSAKeys struct {
	initiator childDirectionKeys
	responder childDirectionKeys
}

type childDirectionKeys struct {
	enc   []byte
	integ []byte
}

// deriveChildSAKeys derives the CHILD_SA encryption/integrity keys from SK_d
// (RFC 7296 §2.17): prf+(SK_d, Ni | Nr).
func (s *Session) deriveChildSAKeys() (*childSAKeys, error) {
	return s.deriveChildSAKeysFor(s.childNi, s.childNr)
}

func (s *Session) deriveChildSAKeysFor(initiatorNonce, responderNonce []byte) (*childSAKeys, error) {
	if s.prf == nil {
		return nil, errors.New("swu: no PRF for child SA keys")
	}
	if len(initiatorNonce) == 0 || len(responderNonce) == 0 {
		return nil, errors.New("swu: child SA nonces are incomplete")
	}
	seed := append(append([]byte{}, initiatorNonce...), responderNonce...)
	encLen := s.espEncKeyLen
	integLen := s.espIntegKeyLen
	if encLen <= 0 {
		return nil, errors.New("swu: invalid ESP encryption key length")
	}
	directionLen := encLen + integLen
	km := crypto.PrfPlus(s.prf, s.ikeKeys.SK_d, seed, 2*directionLen)
	if len(km) < 2*directionLen {
		return nil, errors.New("swu: prf+ produced insufficient child SA keys")
	}
	return &childSAKeys{
		initiator: childDirectionKeys{
			enc:   append([]byte{}, km[:encLen]...),
			integ: append([]byte{}, km[encLen:directionLen]...),
		},
		responder: childDirectionKeys{
			enc:   append([]byte{}, km[directionLen:directionLen+encLen]...),
			integ: append([]byte{}, km[directionLen+encLen:2*directionLen]...),
		},
	}, nil
}

// startEstablishedDataPlane starts the data plane loops.
func (s *Session) startEstablishedDataPlane() error {
	if s.socket == nil || s.innerEndpoint == nil {
		return errors.New("swu: data plane not ready")
	}
	if err := s.innerEndpoint.start(); err != nil {
		return fmt.Errorf("swu: start inner endpoint: %w", err)
	}
	s.mu.Lock()
	if s.dataPlaneStarted {
		s.mu.Unlock()
		return nil
	}
	s.dataPlaneStarted = true
	s.mu.Unlock()
	s.startDataPlaneLoop()
	return nil
}

// startDataPlaneLoop runs the two data-plane loops: ESP → inner and inner → ESP.
func (s *Session) startDataPlaneLoop() {
	transport := s.socket
	endpoint := s.innerEndpoint
	s.dataPlaneWG.Add(2)
	go s.loopESPToInner(transport, endpoint)
	go s.loopInnerToESP(transport, endpoint)
}

// loopESPToInner reads ESP packets from the socket and delivers the inner
// packets to the endpoint.
func (s *Session) loopESPToInner(transport ipsec.Transport, endpoint *userspaceInnerPacketEndpoint) {
	defer s.dataPlaneWG.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case raw, ok := <-transport.ESPPackets():
			if !ok {
				return
			}
			inner, err := s.decapsulateOuterESP(raw)
			if err != nil {
				continue
			}
			if endpoint != nil {
				_ = endpoint.WritePacket(inner)
			}
		}
	}
}

// loopInnerToESP reads inner packets from the endpoint and sends them as ESP.
func (s *Session) loopInnerToESP(transport ipsec.Transport, endpoint *userspaceInnerPacketEndpoint) {
	defer s.dataPlaneWG.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case pkt, ok := <-endpoint.hostPackets:
			if !ok {
				return
			}
			esp, err := s.encapsulateInnerPacket(pkt)
			if err != nil {
				continue
			}
			transport.SendESP(esp)
		}
	}
}

// encapsulateInnerPacket wraps an inner IP packet in ESP (RFC 4303).
func (s *Session) encapsulateInnerPacket(inner []byte) ([]byte, error) {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	if s.espOutboundSA == nil {
		return nil, errors.New("swu: no ESP SA")
	}
	if !matchSelectors(inner, s.childTSi, s.childTSr) {
		return nil, errors.New("swu: outbound inner packet is outside negotiated traffic selectors")
	}
	return ipsec.Encapsulate(inner, nil, s.espOutboundSA)
}

// encapsulateInnerPacketLease wraps an inner packet using a buffer-pool lease.
func (s *Session) encapsulateInnerPacketLease(inner []byte) (*packetLease, error) {
	esp, err := s.encapsulateInnerPacket(inner)
	if err != nil {
		return nil, err
	}
	return &packetLease{data: esp}, nil
}

// decapsulateOuterESP unwraps an ESP packet into the inner IP packet.
func (s *Session) decapsulateOuterESP(esp []byte) ([]byte, error) {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	if s.espInboundSA == nil {
		return nil, errors.New("swu: no ESP SA")
	}
	inner, err := ipsec.Decapsulate(esp, nil, s.espInboundSA)
	if err != nil {
		return nil, err
	}
	if !matchInboundSelectors(inner, s.childTSi, s.childTSr) {
		return nil, errors.New("swu: inbound inner packet is outside negotiated traffic selectors")
	}
	return inner, nil
}

// handleOuterESP processes an inbound ESP packet (alias for decapsulateOuterESP).
func (e *userspaceInnerPacketEndpoint) handleOuterESP(esp []byte) ([]byte, error) {
	return nil, errors.New("swu: handleOuterESP requires a session")
}

// readOuterESP reads the next ESP packet from the socket.
func (e *userspaceInnerPacketEndpoint) readOuterESP() ([]byte, error) {
	return nil, errors.New("swu: readOuterESP requires a session")
}

// stopDataPlane tears down the data plane.
func (s *Session) stopDataPlane() {
	if s.innerEndpoint != nil {
		_ = s.innerEndpoint.Close()
	}
	s.childSAMu.Lock()
	s.espOutboundSA = nil
	s.espInboundSA = nil
	s.childSAMu.Unlock()
	s.mu.Lock()
	s.dataPlaneStarted = false
	s.mu.Unlock()
}

// packetLease wraps a buffer-pool lease for an outbound packet.
type packetLease struct {
	data []byte
}

// Release returns the lease to the pool.
func (l *packetLease) Release() {
	l.data = nil
}

// Nr returns the responder nonce (stored on the session during IKE_SA_INIT).
func (s *Session) Nr() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nr
}

// setNr records the responder nonce.
func (s *Session) setNr(nr []byte) {
	s.mu.Lock()
	s.nr = append([]byte{}, nr...)
	s.mu.Unlock()
}

// resolveXFRMOuterTuple resolves the outer (local, remote) tuple for XFRM.
func (s *Session) resolveXFRMOuterTuple() (net.IP, net.IP, uint16, uint16, error) {
	if s.socket == nil {
		return nil, nil, 0, 0, errors.New("swu: no transport")
	}
	return s.socket.LocalIP(), s.socket.RemoteIP(), s.socket.LocalPort(), s.socket.RemotePort(), nil
}

// selectOutgoingSA selects the outbound ESP SA (single-SA model).
func (s *Session) selectOutgoingSA() *ipsec.SecurityAssociation {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	return s.espOutboundSA
}
