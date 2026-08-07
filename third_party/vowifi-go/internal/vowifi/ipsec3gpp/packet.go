package ipsec3gpp

import (
	"encoding/binary"
	"errors"
	"net"
)

const (
	protocolTCP = 6
	protocolUDP = 17
	protocolESP = 50
)

type ipv4Packet struct {
	headerLength int
	protocol     byte
	source       net.IP
	destination  net.IP
	payload      []byte
}

func parseIPv4Packet(packet []byte) (ipv4Packet, error) {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return ipv4Packet{}, errors.New("ipsec3gpp: valid IPv4 packet required")
	}
	headerLength := int(packet[0]&0x0f) * 4
	if headerLength < 20 || headerLength > len(packet) {
		return ipv4Packet{}, errors.New("ipsec3gpp: invalid IPv4 header length")
	}
	totalLength := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLength < headerLength || totalLength > len(packet) {
		return ipv4Packet{}, errors.New("ipsec3gpp: invalid IPv4 total length")
	}
	fragment := binary.BigEndian.Uint16(packet[6:8])
	if fragment&0x3fff != 0 {
		return ipv4Packet{}, errors.New("ipsec3gpp: fragmented packets are unsupported")
	}
	return ipv4Packet{
		headerLength: headerLength,
		protocol:     packet[9],
		source:       append(net.IP(nil), packet[12:16]...),
		destination:  append(net.IP(nil), packet[16:20]...),
		payload:      packet[headerLength:totalLength],
	}, nil
}

func transportPorts(protocol byte, payload []byte) (uint16, uint16, bool) {
	if protocol != protocolUDP && protocol != protocolTCP || len(payload) < 4 {
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(payload[:2]), binary.BigEndian.Uint16(payload[2:4]), true
}

func replaceIPv4Payload(packet []byte, protocol byte, payload []byte) ([]byte, error) {
	parsed, err := parseIPv4Packet(packet)
	if err != nil {
		return nil, err
	}
	out := make([]byte, parsed.headerLength+len(payload))
	copy(out, packet[:parsed.headerLength])
	copy(out[parsed.headerLength:], payload)
	out[9] = protocol
	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	updateIPv4HeaderChecksum(out[:parsed.headerLength])
	return out, nil
}

func updateIPv4HeaderChecksum(header []byte) {
	if len(header) < 20 || len(header)%2 != 0 {
		return
	}
	header[10], header[11] = 0, 0
	var sum uint32
	for offset := 0; offset < len(header); offset += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[offset : offset+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(header[10:12], ^uint16(sum))
}
