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

type ipPacket struct {
	version          byte
	headerLength     int
	nextHeaderOffset int
	protocol         byte
	fragmented       bool
	source           net.IP
	destination      net.IP
	payload          []byte
	sourcePort       uint16
	destinationPort  uint16
}

func parseIPPacket(packet []byte) (ipPacket, error) {
	if len(packet) == 0 {
		return ipPacket{}, errors.New("ipsec3gpp: empty IP packet")
	}
	switch packet[0] >> 4 {
	case 4:
		return parseIPv4Packet(packet)
	case 6:
		return parseIPv6Packet(packet)
	default:
		return ipPacket{}, errors.New("ipsec3gpp: unsupported IP version")
	}
}

func parseIPv4Packet(packet []byte) (ipPacket, error) {
	if len(packet) < 20 {
		return ipPacket{}, errors.New("ipsec3gpp: IPv4 packet too short")
	}
	headerLength := int(packet[0]&0x0f) * 4
	if headerLength < 20 || headerLength > len(packet) {
		return ipPacket{}, errors.New("ipsec3gpp: invalid IPv4 header length")
	}
	totalLength := int(binary.BigEndian.Uint16(packet[2:4]))
	if totalLength < headerLength || totalLength > len(packet) {
		return ipPacket{}, errors.New("ipsec3gpp: invalid IPv4 total length")
	}
	parsed := ipPacket{
		version: 4, headerLength: headerLength, nextHeaderOffset: 9,
		protocol: packet[9], fragmented: binary.BigEndian.Uint16(packet[6:8])&0x3fff != 0,
		source: append(net.IP(nil), packet[12:16]...), destination: append(net.IP(nil), packet[16:20]...),
		payload: packet[headerLength:totalLength],
	}
	parsed.readTransportPorts()
	return parsed, nil
}

func parseIPv6Packet(packet []byte) (ipPacket, error) {
	const baseHeaderLength = 40
	if len(packet) < baseHeaderLength {
		return ipPacket{}, errors.New("ipsec3gpp: IPv6 packet too short")
	}
	totalLength := baseHeaderLength + int(binary.BigEndian.Uint16(packet[4:6]))
	if totalLength < baseHeaderLength || totalLength > len(packet) {
		return ipPacket{}, errors.New("ipsec3gpp: invalid IPv6 payload length")
	}
	extensions, err := parseIPv6ExtensionHeaders(packet, totalLength)
	if err != nil {
		return ipPacket{}, err
	}
	parsed := ipPacket{
		version: 6, headerLength: extensions.headerLength,
		nextHeaderOffset: extensions.nextHeaderOffset, protocol: extensions.protocol,
		fragmented: extensions.fragmented,
		source:     append(net.IP(nil), packet[8:24]...), destination: append(net.IP(nil), packet[24:40]...),
		payload: packet[extensions.headerLength:totalLength],
	}
	parsed.readTransportPorts()
	return parsed, nil
}

type ipv6Extensions struct {
	headerLength     int
	nextHeaderOffset int
	protocol         byte
	fragmented       bool
}

func parseIPv6ExtensionHeaders(packet []byte, totalLength int) (ipv6Extensions, error) {
	result := ipv6Extensions{headerLength: 40, nextHeaderOffset: 6, protocol: packet[6]}
	for isIPv6ExtensionHeader(result.protocol) {
		if result.headerLength >= totalLength {
			return ipv6Extensions{}, errors.New("ipsec3gpp: truncated IPv6 extension header")
		}
		if result.protocol == 44 {
			if result.headerLength+8 > totalLength {
				return ipv6Extensions{}, errors.New("ipsec3gpp: truncated IPv6 fragment header")
			}
			fragment := binary.BigEndian.Uint16(packet[result.headerLength+2 : result.headerLength+4])
			result.fragmented = result.fragmented || fragment&0xfff9 != 0
			result.advance(packet, 8)
			continue
		}
		if result.headerLength+2 > totalLength {
			return ipv6Extensions{}, errors.New("ipsec3gpp: truncated IPv6 extension header")
		}
		extensionLength := (int(packet[result.headerLength+1]) + 1) * 8
		if result.headerLength+extensionLength > totalLength {
			return ipv6Extensions{}, errors.New("ipsec3gpp: invalid IPv6 extension header length")
		}
		result.advance(packet, extensionLength)
	}
	return result, nil
}

func (extensions *ipv6Extensions) advance(packet []byte, length int) {
	extensions.protocol = packet[extensions.headerLength]
	extensions.nextHeaderOffset = extensions.headerLength
	extensions.headerLength += length
}

func isIPv6ExtensionHeader(protocol byte) bool {
	switch protocol {
	case 0, 43, 44, 60:
		return true
	default:
		return false
	}
}

func (packet *ipPacket) readTransportPorts() {
	if packet.fragmented || (packet.protocol != protocolTCP && packet.protocol != protocolUDP) || len(packet.payload) < 4 {
		return
	}
	packet.sourcePort = binary.BigEndian.Uint16(packet.payload[:2])
	packet.destinationPort = binary.BigEndian.Uint16(packet.payload[2:4])
}

type payloadReplacement struct {
	packet   []byte
	parsed   ipPacket
	protocol byte
	payload  []byte
}

func replaceIPPayload(replacement payloadReplacement) ([]byte, error) {
	newLength := replacement.parsed.headerLength + len(replacement.payload)
	out := make([]byte, newLength)
	copy(out, replacement.packet[:replacement.parsed.headerLength])
	copy(out[replacement.parsed.headerLength:], replacement.payload)
	if replacement.parsed.version == 4 {
		if newLength > 0xffff {
			return nil, errors.New("ipsec3gpp: IPv4 packet too large")
		}
		binary.BigEndian.PutUint16(out[2:4], uint16(newLength))
		out[replacement.parsed.nextHeaderOffset] = replacement.protocol
		updateIPv4HeaderChecksum(out[:replacement.parsed.headerLength])
		return out, nil
	}
	if replacement.parsed.version != 6 {
		return nil, errors.New("ipsec3gpp: unsupported IP version")
	}
	payloadLength := newLength - 40
	if len(replacement.payload) > 0xffff {
		return nil, errors.New("ipsec3gpp: IPv6 payload too large")
	}
	binary.BigEndian.PutUint16(out[4:6], uint16(payloadLength))
	out[replacement.parsed.nextHeaderOffset] = replacement.protocol
	return out, nil
}

func updateIPv4HeaderChecksum(header []byte) {
	header[10], header[11] = 0, 0
	var sum uint32
	for offset := 0; offset+1 < len(header); offset += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[offset : offset+2]))
	}
	if len(header)%2 != 0 {
		sum += uint32(header[len(header)-1]) << 8
	}
	for sum > 0xffff {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(header[10:12], ^uint16(sum))
}
