package swu

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// IKEv2 notify types used during IKE_SA_INIT (RFC 7296 §3.10.1, §2.6, RFC 5685).
const (
	notifyInvalidKE      uint16 = 35    // INVALID_KE_PAYLOAD
	notifyCookie         uint16 = 16390 // COOKIE
	notifyNATSource      uint16 = 16388 // NAT_DETECTION_SOURCE_IP
	notifyNATDestination uint16 = 16389 // NAT_DETECTION_DESTINATION_IP
	notifyRedirectedTo   uint16 = 16407 // REDIRECTED_TO
)

// errCookieRequired signals that the responder returned a COOKIE notify and the
// IKE_SA_INIT must be resent with that cookie.
var errCookieRequired = errors.New("ike: responder requires cookie")

// ParseRedirectData parses the data of an IKEv2 REDIRECTED_TO notify
// (RFC 5685 §3). The first byte is the gateway identifier type:
//
//	1 = IPv4 address (4 bytes)   → "<a.b.c.d>"
//	2 = IPv6 address (16 bytes)  → "<ipv6>"
//	3 = FQDN                      → the domain name
//
// It returns the gateway as a string.
func ParseRedirectData(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty redirect data")
	}
	typ := data[0]
	body := data[1:]
	switch typ {
	case 1: // IPv4
		if len(data) != 5 {
			return "", fmt.Errorf("bad IPv4 redirect length %d", len(data)-1)
		}
		return net.IP(body).String(), nil
	case 2: // IPv6
		if len(data) != 17 {
			return "", fmt.Errorf("bad IPv6 redirect length %d", len(data)-1)
		}
		return net.IP(body).String(), nil
	case 3: // FQDN
		return string(body), nil
	default:
		return "", fmt.Errorf("unsupported redirect gateway type %d", typ)
	}
}

// buildIKEProposals builds the SAi proposal list for IKE_SA_INIT from the
// configured IKE algorithm transform IDs.
func buildIKEProposals(encr, prf, integ, dh uint16) []*ikev2.Proposal {
	return ikev2.CreateMultiProposalIKE(encr, prf, integ, dh)
}

func buildIKEProposalsForSession(session *Session) []*ikev2.Proposal {
	return ikev2.CreateIKEProposals(ikev2.IKEProposalAlgorithms{
		Encryption: session.encrAlg, EncryptionKeyBits: session.encKeyBits,
		PRF: session.prfAlg, Integrity: session.integAlg, DH: session.dhGroup,
	})
}

// buildIKESAInitPacket constructs the IKE_SA_INIT request: SAi | KEi | Ni.
// The initiator SPI is randomly generated, the DH keypair is generated on the
// session's Diffie-Hellman instance, and the initiator nonce is stored for
// later key derivation.
func (s *Session) buildIKESAInitPacket() (*ikev2.IKEPacket, error) {
	if s.dh == nil {
		return nil, errors.New("no Diffie-Hellman instance configured")
	}
	if len(s.cookie) == 0 {
		if err := s.generateIKEInitMaterial(); err != nil {
			return nil, err
		}
	} else if s.SPIi == ([8]byte{}) || len(s.Ni) == 0 || len(s.dh.PublicKeyBytes()) == 0 {
		return nil, errors.New("cannot retry COOKIE without original IKE_SA_INIT material")
	}

	payloads := make([]ikev2.Payload, 0, 6)
	if len(s.cookie) > 0 {
		payloads = append(payloads, &ikev2.EncryptedPayloadNotify{
			NotifyType: notifyCookie,
			NotifyData: append([]byte(nil), s.cookie...),
		})
	}
	payloads = append(payloads,
		&ikev2.EncryptedPayloadSA{Proposals: buildIKEProposalsForSession(s)},
		&ikev2.EncryptedPayloadKE{DHGroup: ikev2.AlgorithmType(s.dhGroup), KEData: s.dh.PublicKeyBytes()},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), s.Ni...)},
	)

	pkt := &ikev2.IKEPacket{
		Header:   newIKEHeader(s.SPIi, [8]byte{}, ikev2.IKE_SA_INIT, ikev2.FlagInitiator, 0),
		Payloads: payloads,
	}
	if s.socket != nil {
		pkt.Payloads = append(pkt.Payloads,
			natDetectionNotify(notifyNATSource, s.SPIi, [8]byte{}, s.socket.LocalIP(), s.socket.LocalPort()),
			natDetectionNotify(notifyNATDestination, s.SPIi, [8]byte{}, s.socket.RemoteIP(), uint16(s.socket.RemotePort())),
		)
	}
	return pkt, nil
}

func (s *Session) generateIKEInitMaterial() error {
	if err := s.dh.GenerateKey(); err != nil {
		return fmt.Errorf("generate DH key: %w", err)
	}
	if _, err := rand.Read(s.SPIi[:]); err != nil {
		return fmt.Errorf("generate SPIi: %w", err)
	}
	nonceLen := s.nonceLen
	if nonceLen <= 0 {
		nonceLen = 32
	}
	s.Ni = make([]byte, nonceLen)
	if _, err := rand.Read(s.Ni); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	return nil
}

// handleIKESAInitResp processes an IKE_SA_INIT response. On a normal response
// it records the responder SPI/nonce, computes the DH shared secret and derives
// the IKE SA keys. Special responses (COOKIE, INVALID_KE_PAYLOAD, REDIRECT) are
// surfaced as sentinel/error values for the caller.
func (s *Session) handleIKESAInitResp(resp *ikev2.IKEPacket) error {
	if resp == nil {
		return errors.New("nil IKE_SA_INIT response")
	}
	header := packetIKEHeader(resp)
	if header.ExchangeType != ikev2.IKE_SA_INIT {
		return fmt.Errorf("unexpected exchange type %d in IKE_SA_INIT response", header.ExchangeType)
	}

	// First pass: handle control notifies (COOKIE / INVALID_KE / REDIRECT)
	// before doing any DH work.
	var nATSource, nATDest []byte
	var selectedSA *ikev2.EncryptedPayloadSA
	var kePayload ikev2.Payload
	var nr []byte
	for _, pl := range resp.Payloads {
		switch pl.Type() {
		case ikev2.PayloadSA:
			value, ok := pl.(*ikev2.EncryptedPayloadSA)
			if !ok || selectedSA != nil {
				return errors.New("invalid or duplicate IKE_SA_INIT SA payload")
			}
			selectedSA = value
		case ikev2.PayloadNotify:
			nt, data, ok := parseNotifyPayload(pl)
			if !ok {
				return errors.New("invalid IKE_SA_INIT Notify payload")
			}
			switch nt {
			case notifyInvalidKE:
				if len(data) >= 2 {
					return &ErrInvalidKEGroup{Group: binary.BigEndian.Uint16(data[:2])}
				}
				return &ErrInvalidKEGroup{}
			case notifyCookie:
				s.cookie = append([]byte{}, data...)
				return errCookieRequired
			case notifyRedirectedTo:
				target, err := ParseRedirectData(data)
				if err != nil {
					return fmt.Errorf("redirect: %w", err)
				}
				return &RedirectError{Target: target}
			case notifyNATSource:
				nATSource = append([]byte{}, data...)
			case notifyNATDestination:
				nATDest = append([]byte{}, data...)
			}
		case ikev2.PayloadKE:
			if kePayload != nil {
				return errors.New("duplicate IKE_SA_INIT KE payload")
			}
			kePayload = pl
		case ikev2.PayloadNi:
			value, ok := pl.(*ikev2.EncryptedPayloadNonce)
			if !ok || len(value.NonceData) == 0 || len(nr) != 0 {
				return errors.New("invalid or duplicate IKE_SA_INIT nonce payload")
			}
			nr = append([]byte{}, value.NonceData...)
		}
	}
	if err := s.validateIKESAInitSelection(selectedSA); err != nil {
		return err
	}

	if kePayload == nil {
		return errors.New("IKE_SA_INIT response missing KE payload")
	}
	if len(nr) == 0 {
		return errors.New("IKE_SA_INIT response missing nonce")
	}

	group, peerKey, err := parseKEPayload(kePayload)
	if err != nil {
		return fmt.Errorf("parse KEr: %w", err)
	}
	if group != s.dhGroup {
		return fmt.Errorf("KEr DH group %d does not match KEi group %d", group, s.dhGroup)
	}

	shared, err := s.dh.ComputeSharedSecret(peerKey)
	if err != nil {
		return fmt.Errorf("compute DH shared secret: %w", err)
	}

	// Record the responder SPI and nonce.
	s.SPIr = ikeSPIBytes(header.SPIr)
	s.setNr(nr)
	s.dhSharedSecret = shared

	// Derive the IKE SA keys (RFC 7296 §2.14-2.21).
	if err := s.GenerateIKESAKeys(nr); err != nil {
		return fmt.Errorf("derive IKE SA keys: %w", err)
	}

	// Stash the NAT-D hashes for later NAT detection (the comparison requires
	// the local/remote transport addresses, wired up with the data plane).
	s.natSourceHash = nATSource
	s.natDestHash = nATDest
	s.applyNATTraversal(nATSource, nATDest)
	return nil
}

func (s *Session) validateIKESAInitSelection(sa *ikev2.EncryptedPayloadSA) error {
	if sa == nil || len(sa.Proposals) != 1 {
		return errors.New("IKE_SA_INIT response missing one selected SA proposal")
	}
	proposal := sa.Proposals[0]
	if proposal == nil || proposal.ProposalNum != 1 || proposal.ProtocolID != ikev2.ProtoIKE || len(proposal.SPI) != 0 {
		return errors.New("IKE_SA_INIT response selected an invalid IKE proposal")
	}
	want := map[ikev2.TransformType]ikev2.AlgorithmType{
		ikev2.TransformTypeEncr:  ikev2.AlgorithmType(s.encrAlg),
		ikev2.TransformTypePRF:   ikev2.AlgorithmType(s.prfAlg),
		ikev2.TransformTypeInteg: ikev2.AlgorithmType(s.integAlg),
		ikev2.TransformTypeDH:    ikev2.AlgorithmType(s.dhGroup),
	}
	seen := make(map[ikev2.TransformType]bool, len(want))
	for _, transform := range proposal.Transforms {
		if transform == nil {
			return errors.New("IKE_SA_INIT response selected a nil transform")
		}
		expected, ok := want[transform.Type]
		if !ok || seen[transform.Type] || expected != transform.ID {
			return fmt.Errorf("IKE_SA_INIT response selected unexpected transform")
		}
		if transform.Type == ikev2.TransformTypeEncr {
			if err := validateEncryptionKeyLength(transform, s.encKeyBits); err != nil {
				return err
			}
		} else if len(transform.Attributes) != 0 {
			return errors.New("IKE_SA_INIT non-encryption transform has attributes")
		}
		seen[transform.Type] = true
	}
	if len(seen) != len(want) {
		return errors.New("IKE_SA_INIT response selected an incomplete IKE proposal")
	}
	return nil
}

func natDetectionNotify(notifyType uint16, spiI, spiR [8]byte, ip net.IP, port uint16) *ikev2.EncryptedPayloadNotify {
	return &ikev2.EncryptedPayloadNotify{
		NotifyType: notifyType,
		NotifyData: natDetectionHash(spiI, spiR, ip, port),
	}
}

func natDetectionHash(spiI, spiR [8]byte, ip net.IP, port uint16) []byte {
	data := make([]byte, 0, 16+net.IPv6len+2)
	data = append(data, spiI[:]...)
	data = append(data, spiR[:]...)
	if ipv4 := ip.To4(); ipv4 != nil {
		data = append(data, ipv4...)
	} else if ipv6 := ip.To16(); ipv6 != nil {
		data = append(data, ipv6...)
	}
	data = binary.BigEndian.AppendUint16(data, port)
	sum := sha1.Sum(data)
	return sum[:]
}

func (s *Session) applyNATTraversal(sourceHash, destinationHash []byte) {
	if s.socket == nil || len(sourceHash) == 0 || len(destinationHash) == 0 {
		return
	}
	expectedSource := natDetectionHash(s.SPIi, s.SPIr, s.socket.RemoteIP(), uint16(s.socket.RemotePort()))
	expectedDestination := natDetectionHash(s.SPIi, s.SPIr, s.socket.LocalIP(), s.socket.LocalPort())
	if !bytes.Equal(sourceHash, expectedSource) || !bytes.Equal(destinationHash, expectedDestination) {
		s.socket.SetRemotePort(4500)
		s.remotePort = 4500
	}
}

// parseKERaw extracts the DH group and key bytes from a raw KE payload body
// (group 2B | reserved 2B | key data).
func parseKERaw(p *ikev2.RawPayload) (uint16, []byte, error) {
	if len(p.Data) < 4 {
		return 0, nil, errors.New("KE payload too short")
	}
	group := binary.BigEndian.Uint16(p.Data[0:2])
	key := p.Data[4:]
	return group, key, nil
}

func parseKEPayload(payload ikev2.Payload) (uint16, []byte, error) {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadKE:
		return uint16(value.DHGroup), value.KEData, nil
	case *ikev2.RawPayload:
		return parseKERaw(value)
	default:
		return 0, nil, fmt.Errorf("unexpected KE payload type %T", payload)
	}
}

// parseNotifyRaw extracts the notify type and data from a raw Notify payload
// body (proto 1B | spiSize 1B | type 2B | [spi] | data).
func parseNotifyRaw(p *ikev2.RawPayload) (uint16, []byte) {
	if len(p.Data) < 4 {
		return 0, nil
	}
	spiSize := int(p.Data[1])
	nt := binary.BigEndian.Uint16(p.Data[2:4])
	off := 4 + spiSize
	if off > len(p.Data) {
		return nt, nil
	}
	return nt, p.Data[off:]
}

func parseNotifyPayload(payload ikev2.Payload) (uint16, []byte, bool) {
	switch value := payload.(type) {
	case *ikev2.EncryptedPayloadNotify:
		return value.NotifyType, value.NotifyData, true
	case *ikev2.RawPayload:
		notifyType, data := parseNotifyRaw(value)
		return notifyType, data, true
	default:
		return 0, nil, false
	}
}
