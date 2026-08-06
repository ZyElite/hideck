package swu

import (
	"crypto/rand"
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

// buildIKESAInitPacket constructs the IKE_SA_INIT request: SAi | KEi | Ni.
// The initiator SPI is randomly generated, the DH keypair is generated on the
// session's Diffie-Hellman instance, and the initiator nonce is stored for
// later key derivation.
func (s *Session) buildIKESAInitPacket() (*ikev2.IKEPacket, error) {
	if s.dh == nil {
		return nil, errors.New("no Diffie-Hellman instance configured")
	}
	if err := s.dh.GenerateKey(); err != nil {
		return nil, fmt.Errorf("generate DH key: %w", err)
	}

	// Initiator SPI (random 8 bytes).
	if _, err := rand.Read(s.SPIi[:]); err != nil {
		return nil, fmt.Errorf("generate SPIi: %w", err)
	}

	// Initiator nonce.
	nonceLen := s.nonceLen
	if nonceLen <= 0 {
		nonceLen = 32
	}
	ni := make([]byte, nonceLen)
	if _, err := rand.Read(ni); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	s.Ni = ni

	pkt := &ikev2.IKEPacket{
		InitiatorSPI: s.SPIi,
		ResponderSPI: [8]byte{}, // unknown for IKE_SA_INIT
		Version:      0x20,
		ExchangeType: ikev2.ExchangeIKEInit,
		Flags:        0x08, // Initiator
		MessageID:    0,
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: buildIKEProposals(s.encrAlg, s.prfAlg, s.integAlg, s.dhGroup)},
			&ikev2.EncryptedPayloadKE{DHGroupNum: s.dhGroup, KeyData: s.dh.PublicKeyBytes()},
			&ikev2.EncryptedPayloadNonce{Data: ni},
		},
	}
	// Retransmit the COOKIE notify if the responder previously demanded one.
	if len(s.cookie) > 0 {
		pkt.Payloads = append(pkt.Payloads, &ikev2.EncryptedPayloadNotify{
			ProtocolID: ikev2.ProtoIKE,
			NotifyType: notifyCookie,
			NotifyData: append([]byte{}, s.cookie...),
		})
	}
	return pkt, nil
}
// handleIKESAInitResp processes an IKE_SA_INIT response. On a normal response
// it records the responder SPI/nonce, computes the DH shared secret and derives
// the IKE SA keys. Special responses (COOKIE, INVALID_KE_PAYLOAD, REDIRECT) are
// surfaced as sentinel/error values for the caller.
func (s *Session) handleIKESAInitResp(resp *ikev2.IKEPacket) error {
	if resp == nil {
		return errors.New("nil IKE_SA_INIT response")
	}
	if resp.ExchangeType != ikev2.ExchangeIKEInit {
		return fmt.Errorf("unexpected exchange type %d in IKE_SA_INIT response", resp.ExchangeType)
	}

	// First pass: handle control notifies (COOKIE / INVALID_KE / REDIRECT)
	// before doing any DH work.
	var nATSource, nATDest []byte
	var keRaw *ikev2.RawPayload
	var nr []byte
	for _, pl := range resp.Payloads {
		switch pl.Type() {
		case ikev2.PayloadNotify:
			nt, data := parseNotifyRaw(pl.(*ikev2.RawPayload))
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
			keRaw = pl.(*ikev2.RawPayload)
		case ikev2.PayloadNi:
			nr = append([]byte{}, pl.(*ikev2.RawPayload).Data...)
		}
	}

	if keRaw == nil {
		return errors.New("IKE_SA_INIT response missing KE payload")
	}
	if len(nr) == 0 {
		return errors.New("IKE_SA_INIT response missing nonce")
	}

	group, peerKey, err := parseKERaw(keRaw)
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
	s.SPIr = resp.ResponderSPI
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
	return nil
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
