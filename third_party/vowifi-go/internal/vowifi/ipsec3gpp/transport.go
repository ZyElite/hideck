package ipsec3gpp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
)

const espICVLength = 12

type outboundSA struct {
	localPort, remotePort uint16
	spi                   uint32
	sequence              uint32
	mu                    sync.Mutex
}

type inboundSA struct {
	remotePort, localPort uint16
	spi                   uint32
	replay                *ReplayWindow
}

type Transport struct {
	policy   Policy
	outbound []outboundSA
	inbound  map[uint32]*inboundSA
	stats    transportCounters
}

type TransportStats struct {
	InboundPackets, OutboundPackets uint64
	InboundBytes, OutboundBytes     uint64
}

type transportCounters struct {
	inboundPackets, outboundPackets atomic.Uint64
	inboundBytes, outboundBytes     atomic.Uint64
}

func NewTransport(policy Policy) (*Transport, error) {
	policy, err := NewPolicy(policy)
	if err != nil {
		return nil, err
	}
	return &Transport{
		policy: policy,
		outbound: []outboundSA{
			{localPort: policy.LocalClientPort, remotePort: policy.RemoteServerPort, spi: policy.RemoteServerSPI},
			{localPort: policy.LocalServerPort, remotePort: policy.RemoteClientPort, spi: policy.RemoteClientSPI},
		},
		inbound: map[uint32]*inboundSA{
			policy.LocalClientSPI: {remotePort: policy.RemoteServerPort, localPort: policy.LocalClientPort, spi: policy.LocalClientSPI, replay: NewReplayWindow(32)},
			policy.LocalServerSPI: {remotePort: policy.RemoteClientPort, localPort: policy.LocalServerPort, spi: policy.LocalServerSPI, replay: NewReplayWindow(32)},
		},
	}, nil
}

func (t *Transport) TransformOutbound(packet []byte) ([]byte, error) {
	parsed, err := parseIPPacket(packet)
	if err != nil {
		return nil, err
	}
	flow := t.matchOutbound(parsed)
	if flow == nil {
		return t.passOrRejectOutbound(packet, parsed)
	}
	sequence, err := flow.nextSequence()
	if err != nil {
		return nil, err
	}
	espPayload, err := t.seal(flow.spi, sequence, parsed.protocol, parsed.payload)
	if err != nil {
		return nil, err
	}
	out, err := replaceIPPayload(packet, protocolESP, espPayload)
	if err != nil {
		return nil, err
	}
	t.stats.outboundPackets.Add(1)
	t.stats.outboundBytes.Add(uint64(len(out)))
	return out, nil
}

func (t *Transport) TransformInbound(packet []byte) ([]byte, error) {
	parsed, err := parseIPPacket(packet)
	if err != nil {
		return nil, err
	}
	if parsed.protocol != protocolESP {
		return t.passOrRejectInbound(packet, parsed)
	}
	if !parsed.source.Equal(t.policy.RemoteIP) || !parsed.destination.Equal(t.policy.LocalIP) {
		return packet, nil
	}
	if len(parsed.payload) < 8+espICVLength {
		return nil, errors.New("ipsec3gpp: ESP packet too short")
	}
	spi := binary.BigEndian.Uint32(parsed.payload[:4])
	flow := t.inbound[spi]
	if flow == nil {
		return nil, fmt.Errorf("ipsec3gpp: unknown inbound SPI %d", spi)
	}
	sequence := binary.BigEndian.Uint32(parsed.payload[4:8])
	plaintext, nextHeader, err := t.open(parsed.payload)
	if err != nil {
		return nil, err
	}
	if !flow.replay.Accept(sequence) {
		return nil, fmt.Errorf("ipsec3gpp: rejected replay sequence %d", sequence)
	}
	if err := validatePorts(nextHeader, plaintext, flow.remotePort, flow.localPort); err != nil {
		return nil, err
	}
	out, err := replaceIPPayload(packet, nextHeader, plaintext)
	if err != nil {
		return nil, err
	}
	t.stats.inboundPackets.Add(1)
	t.stats.inboundBytes.Add(uint64(len(out)))
	return out, nil
}

func (t *Transport) matchOutbound(packet ipPacket) *outboundSA {
	if !packet.source.Equal(t.policy.LocalIP) || !packet.destination.Equal(t.policy.RemoteIP) {
		return nil
	}
	source, destination, ok := transportPorts(packet.protocol, packet.payload)
	if !ok {
		return nil
	}
	for index := range t.outbound {
		flow := &t.outbound[index]
		if flow.localPort == source && flow.remotePort == destination {
			return flow
		}
	}
	return nil
}

func (t *Transport) passOrRejectOutbound(packet []byte, parsed ipPacket) ([]byte, error) {
	_, destination, ok := transportPorts(parsed.protocol, parsed.payload)
	if !parsed.destination.Equal(t.policy.RemoteIP) || !ok {
		return packet, nil
	}
	for index := range t.outbound {
		if t.outbound[index].remotePort == destination {
			return nil, errors.New("ipsec3gpp: outbound packet missed protected selector")
		}
	}
	return packet, nil
}

func (t *Transport) passOrRejectInbound(packet []byte, parsed ipPacket) ([]byte, error) {
	source, destination, ok := transportPorts(parsed.protocol, parsed.payload)
	if !parsed.source.Equal(t.policy.RemoteIP) || !parsed.destination.Equal(t.policy.LocalIP) || !ok {
		return packet, nil
	}
	for _, flow := range t.inbound {
		if flow.remotePort == source && flow.localPort == destination {
			return nil, errors.New("ipsec3gpp: unprotected packet matched protected selector")
		}
	}
	return packet, nil
}

func (flow *outboundSA) nextSequence() (uint32, error) {
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.sequence == math.MaxUint32 {
		return 0, errors.New("ipsec3gpp: ESP sequence exhausted")
	}
	flow.sequence++
	return flow.sequence, nil
}

func (t *Transport) Stats() TransportStats {
	return TransportStats{
		InboundPackets: t.stats.inboundPackets.Load(), OutboundPackets: t.stats.outboundPackets.Load(),
		InboundBytes: t.stats.inboundBytes.Load(), OutboundBytes: t.stats.outboundBytes.Load(),
	}
}
