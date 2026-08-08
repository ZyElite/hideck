package swu

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const (
	notifyInvalidKE      uint16 = 17
	notifyCookie         uint16 = 16390
	notifyNATSource      uint16 = 16388
	notifyNATDestination uint16 = 16389
	notifyRedirectedTo   uint16 = 16407
	notifyFragmentation  uint16 = 16430
)

var errCookieRequired = ErrCookieRequired

func ParseRedirectData(data []byte) (string, error) {
	if len(data) < 1 {
		return "", errors.New("empty redirect data")
	}
	gatewayType, gatewayData := data[0], data[1:]
	switch gatewayType {
	case 1:
		if len(gatewayData) != net.IPv4len {
			return "", fmt.Errorf("invalid IPv4 length: %d", len(gatewayData))
		}
		return net.IP(gatewayData).String(), nil
	case 2:
		if len(gatewayData) != net.IPv6len {
			return "", fmt.Errorf("invalid IPv6 length: %d", len(gatewayData))
		}
		return net.IP(gatewayData).String(), nil
	case 3:
		return string(gatewayData), nil
	default:
		return "", fmt.Errorf("unknown gateway identity type: %d", gatewayType)
	}
}

func detectOutboundIPv4(remoteIP net.IP, remotePort uint16) (net.IP, error) {
	if remoteIP == nil {
		return nil, errors.New("remote ip is nil")
	}
	remote := &net.UDPAddr{IP: remoteIP, Port: int(remotePort)}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	connection, err := (&net.Dialer{}).DialContext(ctx, "udp", remote.String())
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	local, ok := connection.LocalAddr().(*net.UDPAddr)
	if ok && local.IP.To4() != nil {
		return local.IP.To4(), nil
	}
	return nil, errors.New("cannot detect outbound ip")
}

func (s *Session) sendIKESAInit() error {
	data, err := s.buildIKESAInitPacket()
	if err != nil {
		return err
	}
	return s.sendIKE(data)
}

func (s *Session) buildIKESAInitPacket() ([]byte, error) {
	packet, err := s.buildIKESAInitPacketObject()
	if err != nil {
		return nil, err
	}
	data, err := packet.Encode()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Session) buildIKESAInitPacketObject() (*ikev2.IKEPacket, error) {
	proposals, profiles, _, err := buildIKEProposals(s.cfg, nil, s.ikeProfileOffset)
	if err != nil {
		return nil, err
	}
	s.offeredIKEProfiles = append([]string(nil), profiles...)
	s.effectiveCipherPolicy = buildAlgorithmPlan(s.cfg).policyLabel()
	if err := s.prepareIKEInitMaterial(proposals); err != nil {
		return nil, err
	}
	s.offeredIKEProposals = cloneProposals(proposals)

	payloads := make([]ikev2.Payload, 0, 7)
	if s.sendCookie && len(s.cookie) > 0 {
		payloads = append(payloads, &ikev2.EncryptedPayloadNotify{
			ProtocolID: 0, NotifyType: notifyCookie,
			NotifyData: append([]byte(nil), s.cookie...),
		})
	}
	payloads = append(payloads,
		&ikev2.EncryptedPayloadSA{Proposals: proposals},
		&ikev2.EncryptedPayloadKE{
			DHGroup: ikev2.AlgorithmType(s.dhGroup), KEData: s.dh.PublicKeyBytes(),
		},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), s.Ni...)},
	)
	payloads = append(payloads, s.ikeInitNetworkNotifies()...)
	payloads = append(payloads, &ikev2.EncryptedPayloadNotify{NotifyType: notifyFragmentation})
	return &ikev2.IKEPacket{
		Header:   newIKEHeader(s.SPIi, [8]byte{}, ikev2.IKE_SA_INIT, ikev2.FlagInitiator, 0),
		Payloads: payloads,
	}, nil
}

func (s *Session) prepareIKEInitMaterial(proposals []*ikev2.Proposal) error {
	preferredGroup := uint16(firstDHGroupFromProposals(proposals))
	if s.dh == nil {
		dh, err := crypto.NewDiffieHellman(preferredGroup)
		if err != nil {
			return err
		}
		s.dh, s.dhGroup = dh, preferredGroup
	}
	ensureProposalDHGroup(proposals, ikev2.AlgorithmType(s.dhGroup))
	prioritizeDHGroup(proposals, ikev2.AlgorithmType(s.dhGroup))
	if s.SPIi != ([8]byte{}) && len(s.Ni) > 0 && len(s.dh.PublicKeyBytes()) > 0 {
		return nil
	}
	if s.sendCookie {
		return errors.New("cannot retry COOKIE without original IKE_SA_INIT material")
	}
	return s.generateIKEInitMaterial()
}

func ensureProposalDHGroup(proposals []*ikev2.Proposal, group ikev2.AlgorithmType) {
	for _, proposal := range proposals {
		for _, transform := range proposal.Transforms {
			if transform != nil && transform.Type == ikev2.TransformTypeDH && transform.ID == group {
				return
			}
		}
	}
	for _, proposal := range proposals {
		for _, transform := range proposal.Transforms {
			if transform != nil && transform.Type == ikev2.TransformTypeDH {
				transform.ID = group
			}
		}
	}
}

func (s *Session) generateIKEInitMaterial() error {
	if err := s.dh.GenerateKey(); err != nil {
		return fmt.Errorf("generate DH key: %w", err)
	}
	if _, err := rand.Read(s.SPIi[:]); err != nil {
		return fmt.Errorf("generate SPIi: %w", err)
	}
	nonceLength := s.nonceLen
	if nonceLength <= 0 {
		nonceLength = 32
	}
	s.Ni = make([]byte, nonceLength)
	if _, err := rand.Read(s.Ni); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	return nil
}

func (s *Session) ikeInitNetworkNotifies() []ikev2.Payload {
	if s.socket == nil {
		return nil
	}
	return []ikev2.Payload{
		natDetectionNotify(notifyNATSource, s.SPIi, [8]byte{}, s.socket.LocalIP(), s.socket.LocalPort()),
		natDetectionNotify(notifyNATDestination, s.SPIi, [8]byte{}, s.socket.RemoteIP(), uint16(s.socket.RemotePort())),
	}
}

func prioritizeDHGroup(proposals []*ikev2.Proposal, preferred ikev2.AlgorithmType) {
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		others := make([]*ikev2.Transform, 0, len(proposal.Transforms))
		preferredDH := make([]*ikev2.Transform, 0, 1)
		remainingDH := make([]*ikev2.Transform, 0, len(proposal.Transforms))
		for _, transform := range proposal.Transforms {
			if transform == nil || transform.Type != ikev2.TransformTypeDH {
				others = append(others, transform)
				continue
			}
			if transform.ID == preferred {
				preferredDH = append(preferredDH, transform)
			} else {
				remainingDH = append(remainingDH, transform)
			}
		}
		proposal.Transforms = append(others, preferredDH...)
		proposal.Transforms = append(proposal.Transforms, remainingDH...)
	}
}

func cloneProposals(proposals []*ikev2.Proposal) []*ikev2.Proposal {
	result := make([]*ikev2.Proposal, 0, len(proposals))
	for _, proposal := range proposals {
		result = append(result, cloneProposal(proposal))
	}
	return result
}

func (s *Session) advanceIKEProfileOffset() bool {
	profileCount := len(s.cfg.IKEProposals)
	if profileCount == 0 {
		profileCount = len(ikev2.CreateMultiProposalIKE([]byte(nil)))
	}
	if s.ikeProfileOffset+1 >= profileCount {
		return false
	}
	s.ikeProfileOffset++
	s.negotiationFallbackCount++
	return true
}

func (s *Session) resetIKEInitMaterial() {
	s.SPIr = [8]byte{}
	s.nr = nil
	s.ikeKeys = nil
	s.dhSharedSecret = nil
}

func (s *Session) selectRequestedDHGroup(groupError *ErrInvalidKEGroup) error {
	group := groupError.PreferredGroup
	if group == 0 {
		group = groupError.Group
	}
	dh, err := crypto.NewDiffieHellman(group)
	if err != nil {
		return fmt.Errorf("服务器期望的 DH Group %d 不支持: %v", group, err)
	}
	s.dh, s.dhGroup = dh, group
	s.SPIi, s.SPIr = [8]byte{}, [8]byte{}
	s.Ni, s.nr, s.ikeKeys, s.dhSharedSecret = nil, nil, nil, nil
	s.cookie, s.sendCookie = nil, false
	return nil
}

func natDetectionNotify(
	notifyType uint16,
	initiatorSPI, responderSPI [8]byte,
	ip net.IP,
	port uint16,
) *ikev2.EncryptedPayloadNotify {
	return &ikev2.EncryptedPayloadNotify{
		NotifyType: notifyType,
		NotifyData: natDetectionHash(initiatorSPI, responderSPI, ip, port),
	}
}

func natDetectionHash(initiatorSPI, responderSPI [8]byte, ip net.IP, port uint16) []byte {
	data := make([]byte, 0, 16+net.IPv6len+2)
	data = append(data, initiatorSPI[:]...)
	data = append(data, responderSPI[:]...)
	if ipv4 := ip.To4(); ipv4 != nil {
		data = append(data, ipv4...)
	} else if ipv6 := ip.To16(); ipv6 != nil {
		data = append(data, ipv6...)
	}
	data = binary.BigEndian.AppendUint16(data, port)
	digest := sha1.Sum(data)
	return digest[:]
}
