package ipsec3gpp

import (
	"errors"
	"net"
	"sync/atomic"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	engineipsec "github.com/iniwex5/vowifi-go/engine/ipsec"
)

type transportFlow struct {
	flow   Flow
	sa     *engineipsec.SecurityAssociation
	replay *ReplayWindow
}

type Transport struct {
	policy             Policy
	outbound           []transportFlow
	inbound            map[uint32]*transportFlow
	outboundPackets    atomic.Uint64
	inboundPackets     atomic.Uint64
	passthroughPackets atomic.Uint64
	transformErrors    atomic.Uint64
}

type TransportStats struct {
	OutboundPackets    uint64
	InboundPackets     uint64
	PassthroughPackets uint64
	TransformErrors    uint64
	Replay             ReplayStats
}

func NewTransport(policy Policy) (*Transport, error) {
	policy, err := NewPolicy(policy)
	if err != nil {
		return nil, err
	}
	outbound, err := newTransportFlows(policy, false)
	if err != nil {
		return nil, err
	}
	inboundFlows, err := newTransportFlows(policy, true)
	if err != nil {
		return nil, err
	}
	inbound := make(map[uint32]*transportFlow, len(inboundFlows))
	for index := range inboundFlows {
		flow := &inboundFlows[index]
		inbound[flow.sa.SPI] = flow
	}
	return &Transport{policy: policy, outbound: outbound, inbound: inbound}, nil
}

func newTransportFlows(policy Policy, inbound bool) ([]transportFlow, error) {
	flows := []Flow{policy.FlowC, policy.FlowS}
	result := make([]transportFlow, 0, len(flows))
	for _, flow := range flows {
		transportFlow, err := newTransportFlow(flow, inbound)
		if err != nil {
			return nil, err
		}
		result = append(result, transportFlow)
	}
	return result, nil
}

func newTransportFlow(flow Flow, inbound bool) (transportFlow, error) {
	sa, err := newSAForFlow(flow, inbound)
	if err != nil {
		return transportFlow{}, err
	}
	result := transportFlow{flow: cloneFlow(flow), sa: sa}
	if inbound {
		result.replay = NewReplayWindow(32)
	}
	return result, nil
}

func newSAForFlow(flow Flow, inbound bool) (*engineipsec.SecurityAssociation, error) {
	encrypter, encryptionKey, err := encrypterForFlow(flow)
	if err != nil {
		return nil, err
	}
	integrity, integrityKey, err := integrityForFlow(flow)
	if err != nil {
		return nil, err
	}
	spi := flow.OutboundSPI
	if inbound {
		spi = flow.InboundSPI
	}
	return engineipsec.NewSecurityAssociationCBC(spi, encrypter, encryptionKey, integrity, integrityKey), nil
}

func encrypterForFlow(flow Flow) (enginecrypto.Encrypter, []byte, error) {
	key, err := deriveEncKey(flow.CK, flow.EncAlg)
	if err != nil {
		return nil, nil, err
	}
	var algorithmID uint16
	switch normalizedAlgorithm(flow.EncAlg) {
	case "", "aes-cbc", "cbc(aes)":
		algorithmID = enginecrypto.EncrAESCBC
	case "des3-cbc", "des-ede3-cbc", "cbc(des3_ede)":
		algorithmID = enginecrypto.Encr3DESCBC
	case "null", "cipher_null", "ecb(cipher_null)":
		algorithmID = enginecrypto.EncrNull
	default:
		return nil, nil, errors.New("ipsec3gpp: unreachable encryption algorithm")
	}
	encrypter, err := enginecrypto.GetEncrypterWithKeyLen(algorithmID, len(key)*8)
	return encrypter, key, err
}

func integrityForFlow(flow Flow) (enginecrypto.IntegrityAlgorithm, []byte, error) {
	key, err := deriveAuthKey(flow.IK, flow.AuthAlg)
	if err != nil {
		return nil, nil, err
	}
	var algorithmID uint16
	switch normalizedAlgorithm(flow.AuthAlg) {
	case "hmac(md5)", "hmac-md5-96":
		algorithmID = 1
	case "hmac(sha1)", "hmac-sha-1-96":
		algorithmID = 2
	default:
		return nil, nil, errors.New("ipsec3gpp: unreachable authentication algorithm")
	}
	return enginecrypto.NewIntegrity(algorithmID), key, nil
}

func (transport *Transport) TransformOutbound(packet []byte) ([]byte, bool, error) {
	parsed, err := parseIPPacket(packet)
	if err != nil {
		transport.transformErrors.Add(1)
		return nil, false, err
	}
	flow := transport.matchOutbound(parsed)
	if flow == nil {
		transport.passthroughPackets.Add(1)
		return append([]byte(nil), packet...), false, nil
	}
	espPayload, err := engineipsec.EncapsulateWithNextHeaderInto(nil, parsed.payload, parsed.protocol, flow.sa)
	if err != nil {
		transport.transformErrors.Add(1)
		return nil, true, err
	}
	out, err := replaceIPPayload(payloadReplacement{packet: packet, parsed: parsed, protocol: protocolESP, payload: espPayload})
	if err != nil {
		transport.transformErrors.Add(1)
		return nil, true, err
	}
	transport.outboundPackets.Add(1)
	return out, true, nil
}

func (transport *Transport) TransformInbound(packet []byte) ([]byte, bool, error) {
	parsed, err := parseIPPacket(packet)
	if err != nil {
		transport.transformErrors.Add(1)
		return nil, false, err
	}
	if !transport.matchesInboundPacket(parsed) {
		transport.passthroughPackets.Add(1)
		return append([]byte(nil), packet...), false, nil
	}
	if len(parsed.payload) < 8 {
		return transport.inboundError(errors.New("ipsec3gpp: ESP payload too short"))
	}
	flow := transport.inbound[readSPI(parsed.payload)]
	if flow == nil {
		return transport.inboundError(errors.New("ipsec3gpp: unknown inbound ESP SPI"))
	}
	plaintext, nextHeader, sequence, err := engineipsec.DecapsulateWithSequenceInto(nil, parsed.payload, flow.sa)
	if err != nil {
		return transport.inboundError(err)
	}
	if !flow.replay.Accept(sequence) {
		return nil, true, errors.New("ipsec3gpp: replay packet rejected")
	}
	out, err := replaceIPPayload(payloadReplacement{packet: packet, parsed: parsed, protocol: nextHeader, payload: plaintext})
	if err != nil {
		return transport.inboundError(err)
	}
	transport.inboundPackets.Add(1)
	return out, true, nil
}

func (transport *Transport) inboundError(err error) ([]byte, bool, error) {
	transport.transformErrors.Add(1)
	return nil, true, err
}

func (transport *Transport) matchesInboundPacket(packet ipPacket) bool {
	return packet.protocol == protocolESP && ipEqual(packet.source, transport.policy.RemoteIP) &&
		ipEqual(packet.destination, transport.policy.LocalIP)
}

func readSPI(payload []byte) uint32 {
	return uint32(payload[0])<<24 | uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
}

func (transport *Transport) matchOutbound(packet ipPacket) *transportFlow {
	if packet.fragmented || (packet.protocol != protocolTCP && packet.protocol != protocolUDP) {
		return nil
	}
	if !ipEqual(packet.source, transport.policy.LocalIP) || !ipEqual(packet.destination, transport.policy.RemoteIP) {
		return nil
	}
	for index := range transport.outbound {
		flow := &transport.outbound[index]
		if packet.sourcePort == uint16(flow.flow.LocalPort) && packet.destinationPort == uint16(flow.flow.RemotePort) {
			return flow
		}
	}
	return nil
}

func ipEqual(left, right []byte) bool {
	return len(left) != 0 && len(right) != 0 && net.IP(left).Equal(net.IP(right))
}

func (transport *Transport) Stats() TransportStats {
	stats := TransportStats{
		OutboundPackets: transport.outboundPackets.Load(), InboundPackets: transport.inboundPackets.Load(),
		PassthroughPackets: transport.passthroughPackets.Load(), TransformErrors: transport.transformErrors.Load(),
	}
	for _, flow := range transport.inbound {
		replay := flow.replay.Snapshot()
		stats.Replay.Accepted += replay.Accepted
		stats.Replay.Duplicate += replay.Duplicate
		stats.Replay.TooOld += replay.TooOld
	}
	return stats
}
