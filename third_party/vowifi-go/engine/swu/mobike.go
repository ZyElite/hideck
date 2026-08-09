package swu

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const cookie2Size = 16

type mobikeAddressSpec struct {
	localHost  string
	localPort  uint16
	remoteHost string
	remotePort uint16
}

// UpdateAddresses accepts both recovered (newLocal, newRemote string) and
// interim (oldLocal, newLocal net.IP) call shapes.
func (s *Session) UpdateAddresses(addresses ...any) error {
	spec, err := s.parseMOBIKEAddresses(addresses)
	if err != nil {
		return err
	}
	s.mu.RLock()
	supported := s.mobikeSupported
	s.mu.RUnlock()
	if !supported {
		return errors.New("swu: peer did not negotiate MOBIKE")
	}
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if _, err := s.sendMOBIKEUpdateLocked(); err != nil {
		return fmt.Errorf("swu: send MOBIKE address update: %w", err)
	}
	return s.migrateMOBIKETransport(spec)
}

// sendMOBIKEUpdate restores the recovered no-argument COOKIE2 exchange.
func (s *Session) sendMOBIKEUpdate() ([]byte, error) {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	return s.sendMOBIKEUpdateLocked()
}

func (s *Session) sendMOBIKEUpdateLocked() ([]byte, error) {
	transport := s.transport()
	s.mu.RLock()
	hasKeys := s.ikeKeys != nil
	s.mu.RUnlock()
	if transport == nil || !hasKeys || s.State() != stateEstablished {
		return nil, errors.New("swu: session is not established for MOBIKE")
	}
	cookie := make([]byte, cookie2Size)
	if _, err := rand.Read(cookie); err != nil {
		return nil, fmt.Errorf("swu: generate COOKIE2: %w", err)
	}
	payloads, err := s.buildMOBIKEPayloads(transport, cookie)
	if err != nil {
		return nil, err
	}
	response, err := s.sendEncryptedWithRetry(payloads, ikev2.INFORMATIONAL)
	if err != nil {
		return nil, err
	}
	if err := s.verifyCookie2Response(response, cookie); err != nil {
		return nil, err
	}
	return cookie, nil
}

func (s *Session) buildMOBIKEPayloads(transport Transport, cookie []byte) ([]ikev2.Payload, error) {
	payloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.UPDATE_SA_ADDRESSES},
		&ikev2.EncryptedPayloadNotify{NotifyType: ikev2.COOKIE2, NotifyData: append([]byte(nil), cookie...)},
	}
	localIP, remoteIP := transport.LocalIP(), transport.RemoteIP()
	if localIP == nil || remoteIP == nil {
		return payloads, nil
	}
	remotePort := transport.RemotePort()
	if remotePort < 0 || remotePort > int(^uint16(0)) {
		return nil, fmt.Errorf("swu: invalid MOBIKE remote port %d", remotePort)
	}
	source := natDetectionHash(s.spiI, s.spiR, localIP, transport.LocalPort())
	destination := natDetectionHash(s.spiI, s.spiR, remoteIP, uint16(remotePort))
	return append(payloads,
		ikev2.CreateNATDetectionNotify(ikev2.NAT_DETECTION_SOURCE_IP, source),
		ikev2.CreateNATDetectionNotify(ikev2.NAT_DETECTION_DESTINATION_IP, destination),
	), nil
}

// verifyCookie2Response restores the recovered response and downgrade behavior.
func (s *Session) verifyCookie2Response(response, expected []byte) error {
	if len(response) == 0 {
		return nil
	}
	packet, err := ikev2.DecodePacket(response)
	if err != nil {
		return fmt.Errorf("swu: decode MOBIKE response: %w", err)
	}
	payloads, err := s.decryptAndParse(packet)
	if err != nil {
		return fmt.Errorf("swu: decrypt MOBIKE response: %w", err)
	}
	for _, payload := range payloads {
		notify, ok := payload.(*ikev2.EncryptedPayloadNotify)
		if !ok || notify.NotifyType != ikev2.COOKIE2 {
			continue
		}
		if len(notify.NotifyData) != len(expected) {
			return fmt.Errorf("swu: COOKIE2 length %d does not match %d", len(notify.NotifyData), len(expected))
		}
		if subtle.ConstantTimeCompare(notify.NotifyData, expected) != 1 {
			return errors.New("swu: COOKIE2 response mismatch")
		}
		return nil
	}
	return nil
}

func (s *Session) parseMOBIKEAddresses(addresses []any) (mobikeAddressSpec, error) {
	if len(addresses) != 2 {
		return mobikeAddressSpec{}, fmt.Errorf("swu: MOBIKE requires two addresses, got %d", len(addresses))
	}
	switch first := addresses[0].(type) {
	case string:
		second, ok := addresses[1].(string)
		if !ok {
			return mobikeAddressSpec{}, errors.New("swu: MOBIKE address argument types do not match")
		}
		return s.legacyMOBIKEAddressSpec(first, second)
	case net.IP:
		second, ok := addresses[1].(net.IP)
		if !ok || first == nil || second == nil {
			return mobikeAddressSpec{}, errors.New("swu: MOBIKE requires valid old and new local IP addresses")
		}
		return s.currentMOBIKEAddressSpec(second)
	default:
		return mobikeAddressSpec{}, fmt.Errorf("swu: unsupported MOBIKE address type %T", addresses[0])
	}
}

func (s *Session) legacyMOBIKEAddressSpec(local, remote string) (mobikeAddressSpec, error) {
	transport := s.transport()
	local = strings.TrimSpace(local)
	remote = strings.TrimSpace(remote)
	if local == "" && transport != nil && transport.LocalIP() != nil {
		local = transport.LocalIP().String()
	}
	if local == "" {
		local = strings.TrimSpace(s.cfg.LocalAddr)
	}
	if remote == "" && transport != nil && transport.RemoteIP() != nil {
		remote = transport.RemoteIP().String()
	}
	if remote == "" {
		remote = configuredEPDGAddress(s.cfg)
	}
	return s.mobikeAddressSpec(local, remote)
}

func (s *Session) currentMOBIKEAddressSpec(newLocal net.IP) (mobikeAddressSpec, error) {
	transport := s.transport()
	remote := ""
	if transport != nil && transport.RemoteIP() != nil {
		remote = transport.RemoteIP().String()
	}
	if remote == "" {
		remote = configuredEPDGAddress(s.cfg)
	}
	return s.mobikeAddressSpec(newLocal.String(), remote)
}
