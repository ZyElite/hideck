package swu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/bufferpool"
	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

// setupDataPlane builds the ESP SA and the inner packet endpoint after the
// CREATE_CHILD_SA exchange.
func (s *Session) setupDataPlane() error {
	if s.ikeKeys == nil {
		return errors.New("swu: no IKE SA keys for child SA derivation")
	}
	if s.primaryInnerIP() == nil {
		return errors.New("swu: no inner address assigned by ePDG")
	}

	// Derive the CHILD_SA keys from SK_d (RFC 7296 §2.17). The key material is
	// prf+(SK_d, Ni | Nr) for the CREATE_CHILD_SA exchange.
	childKeys, err := s.deriveChildSAKeys()
	if err != nil {
		return err
	}
	mode, err := configuredDataplaneMode(s.cfg)
	if err != nil {
		return err
	}
	if mode == DataplaneModeXFRMI {
		return s.setupKernelXFRMDataPlane(childKeys)
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
	s.espInboundSAs[s.espLocalSPI] = inbound
	s.espKey = append([]byte{}, childKeys.initiator.enc...)
	s.espIntegKey = append([]byte{}, childKeys.initiator.integ...)
	s.childSAMu.Unlock()

	if mode == DataplaneModeTUN {
		return s.setupTUNDataPlane()
	}
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
	return s.deriveChildSAKeysForPFS(s.childNi, s.childNr, s.childDHSecret)
}

func (s *Session) deriveChildSAKeysFor(initiatorNonce, responderNonce []byte) (*childSAKeys, error) {
	return s.deriveChildSAKeysForPFS(initiatorNonce, responderNonce, nil)
}

func (s *Session) deriveChildSAKeysForPFS(
	initiatorNonce []byte,
	responderNonce []byte,
	sharedSecret []byte,
) (*childSAKeys, error) {
	if s.prf == nil {
		return nil, errors.New("swu: no PRF for child SA keys")
	}
	if s.ikeKeys == nil || len(s.ikeKeys.SK_d) == 0 {
		return nil, errors.New("swu: no IKE SK_d for child SA keys")
	}
	if len(initiatorNonce) == 0 || len(responderNonce) == 0 {
		return nil, errors.New("swu: child SA nonces are incomplete")
	}
	seed := append([]byte(nil), sharedSecret...)
	seed = append(seed, initiatorNonce...)
	seed = append(seed, responderNonce...)
	defer crypto.Wipe(seed)
	encLen := s.espEncKeyLen
	integLen := s.espIntegKeyLen
	if encLen <= 0 {
		return nil, errors.New("swu: invalid ESP encryption key length")
	}
	directionLen := encLen + integLen
	km, err := crypto.PrfPlus(s.prf, s.ikeKeys.SK_d, seed, 2*directionLen)
	if err != nil {
		return nil, err
	}
	if len(km) < 2*directionLen {
		crypto.Wipe(km)
		return nil, errors.New("swu: prf+ produced insufficient child SA keys")
	}
	keys := &childSAKeys{
		initiator: childDirectionKeys{
			enc:   append([]byte{}, km[:encLen]...),
			integ: append([]byte{}, km[encLen:directionLen]...),
		},
		responder: childDirectionKeys{
			enc:   append([]byte{}, km[directionLen:directionLen+encLen]...),
			integ: append([]byte{}, km[directionLen+encLen:2*directionLen]...),
		},
	}
	crypto.Wipe(km)
	return keys, nil
}

// startEstablishedDataPlane starts the data plane loops.
func (s *Session) startEstablishedDataPlane() error {
	if s.transport() == nil {
		return errors.New("swu: data plane not ready")
	}
	if s.kernelDataPlane != nil {
		s.markDataPlaneStarted()
		return nil
	}
	if s.tun != nil {
		if !s.markDataPlaneStarted() {
			return nil
		}
		s.startTUNDataPlaneLoop()
		return nil
	}
	return s.startUserspaceDataPlane()
}

func (s *Session) markDataPlaneStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dataPlaneStarted {
		return false
	}
	s.dataPlaneStarted = true
	return true
}

// encapsulateInnerPacket wraps an inner IP packet in ESP (RFC 4303).
func (s *Session) encapsulateInnerPacket(inner []byte) ([]byte, error) {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	if s.espOutboundSA == nil {
		return nil, errInnerPacketSAMissing
	}
	if !matchSelectors(inner, s.childTSi, s.childTSr) {
		return nil, errors.New("swu: outbound inner packet is outside negotiated traffic selectors")
	}
	esp, err := ipsec.Encapsulate(inner, s.espOutboundSA)
	if err != nil {
		return nil, fmt.Errorf("encapsulate inner packet: %w", err)
	}
	return esp, nil
}

// encapsulateInnerPacketLease wraps an inner packet using a buffer-pool lease.
func (s *Session) encapsulateInnerPacketLease(inner []byte) (*packetLease, error) {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	if s.espOutboundSA == nil {
		return nil, errInnerPacketSAMissing
	}
	if !matchSelectors(inner, s.childTSi, s.childTSr) {
		return nil, errors.New("swu: outbound inner packet is outside negotiated traffic selectors")
	}
	total, _, err := ipsec.EncapsulationLayout(len(inner), s.espOutboundSA)
	if err != nil {
		return nil, fmt.Errorf("plan inner packet encapsulation: %w", err)
	}
	buffer := bufferpool.Get(total)
	esp, err := ipsec.EncapsulateInto(buffer.Bytes()[:0], inner, s.espOutboundSA)
	if err != nil {
		buffer.Release()
		return nil, fmt.Errorf("encapsulate inner packet: %w", err)
	}
	return &packetLease{data: esp, buffer: buffer}, nil
}

// decapsulateOuterESP returns the plaintext and parsed SPI for diagnostics.
func (s *Session) decapsulateOuterESP(esp []byte) ([]byte, uint32, error) {
	if len(esp) < 4 {
		return nil, 0, errors.New("ESP packet too short for SPI")
	}
	spi := binary.BigEndian.Uint32(esp[:4])
	s.childSAMu.RLock()
	inbound := s.espInboundSAs[spi]
	if inbound == nil && s.espInboundSA != nil && s.espInboundSA.SPI == spi {
		inbound = s.espInboundSA
	}
	s.childSAMu.RUnlock()
	if inbound == nil {
		return nil, spi, fmt.Errorf("%w: spi=%08x", errInnerPacketSAMissing, spi)
	}
	inner, err := ipsec.Decapsulate(esp, inbound)
	if err != nil {
		return nil, spi, fmt.Errorf("decapsulate ESP packet: %w", err)
	}
	if !matchInboundSelectors(inner, s.childTSi, s.childTSr) {
		return nil, spi, errors.New("swu: inbound inner packet is outside negotiated traffic selectors")
	}
	return inner, spi, nil
}

// stopDataPlane tears down the data plane.
func (s *Session) stopDataPlane() error {
	var cleanupErr error
	s.stopXFRMActions()
	if endpoint := s.swapInnerPacketEndpoint(nil); endpoint != nil {
		cleanupErr = errors.Join(cleanupErr, endpoint.Close())
	}
	cleanupErr = errors.Join(cleanupErr, s.rollbackNetworkConfig())
	if s.tun != nil {
		cleanupErr = errors.Join(cleanupErr, s.tun.Close())
		s.tun = nil
	}
	if s.kernelDataPlane != nil {
		cleanupErr = errors.Join(cleanupErr, s.kernelDataPlane.Close())
		s.kernelDataPlane = nil
	}
	s.dataPlaneWG.Wait()
	s.childSAMu.Lock()
	wipeESPAssociation(s.espOutboundSA)
	for _, association := range s.espInboundSAs {
		wipeESPAssociation(association)
	}
	crypto.Wipe(s.espKey)
	crypto.Wipe(s.espIntegKey)
	crypto.Wipe(s.childDHSecret)
	if s.childDH != nil {
		crypto.Wipe(s.childDH.SharedKey)
	}
	s.espOutboundSA = nil
	s.espInboundSA = nil
	s.espKey, s.espIntegKey = nil, nil
	s.childDH, s.childDHSecret = nil, nil
	clear(s.espInboundSAs)
	clear(s.retiredChildSAs)
	s.childSAMu.Unlock()
	s.mu.Lock()
	s.dataPlaneStarted = false
	s.mu.Unlock()
	return cleanupErr
}

// packetLease wraps a buffer-pool lease for an outbound packet.
type packetLease struct {
	data   []byte
	buffer bufferpool.Lease
}

// Release returns the lease to the pool.
func (l *packetLease) Release() {
	if l == nil {
		return
	}
	l.buffer.Release()
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
	transport := s.transport()
	if transport == nil {
		return nil, nil, 0, 0, errors.New("swu: no transport")
	}
	remotePort := transport.RemotePort()
	if remotePort < 0 || remotePort > int(^uint16(0)) {
		return nil, nil, 0, 0, fmt.Errorf("swu: invalid remote UDP port %d", remotePort)
	}
	return transport.LocalIP(), transport.RemoteIP(), transport.LocalPort(), uint16(remotePort), nil
}

// selectOutgoingSA selects the outbound ESP SA (single-SA model).
func (s *Session) selectOutgoingSA() *ipsec.SecurityAssociation {
	s.childSAMu.RLock()
	defer s.childSAMu.RUnlock()
	return s.espOutboundSA
}
