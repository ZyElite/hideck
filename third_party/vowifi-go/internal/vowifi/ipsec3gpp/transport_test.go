package ipsec3gpp

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func TestTransportProtectsBothDirections(t *testing.T) {
	ue, server := newTransportPair(t, EncryptionAES, nil, nil)
	request := udpPacket(t, net.IPv4(10, 0, 0, 2), 41000, net.IPv4(10, 0, 0, 1), 51001, []byte("REGISTER"))
	protected, transformed, err := ue.TransformOutbound(request)
	if err != nil || !transformed {
		t.Fatalf("protect request = transformed:%t err:%v", transformed, err)
	}
	assertESP(t, protected, 0x44444444)
	decoded, transformed, err := server.TransformInbound(protected)
	if err != nil || !transformed || !bytes.Equal(decoded, request) {
		t.Fatalf("decode request = transformed:%t err:%v\n got %x\nwant %x", transformed, err, decoded, request)
	}
	if _, transformed, err := server.TransformInbound(protected); err == nil || !transformed || err.Error() != "ipsec3gpp: replay packet rejected" {
		t.Fatalf("replay = transformed:%t err:%v", transformed, err)
	}
	if stats := server.Stats(); stats.TransformErrors != 0 || stats.Replay.Accepted != 1 || stats.Replay.Duplicate != 1 {
		t.Fatalf("stats after replay = %+v", stats)
	}

	response := udpPacket(t, net.IPv4(10, 0, 0, 1), 51001, net.IPv4(10, 0, 0, 2), 41000, []byte("200 OK"))
	protected, transformed, err = server.TransformOutbound(response)
	if err != nil || !transformed {
		t.Fatalf("protect response = transformed:%t err:%v", transformed, err)
	}
	assertESP(t, protected, 0x11111111)
	decoded, transformed, err = ue.TransformInbound(protected)
	if err != nil || !transformed || !bytes.Equal(decoded, response) {
		t.Fatalf("decode response = transformed:%t err:%v", transformed, err)
	}
}

func TestTransportPreservesIPv6ExtensionHeaders(t *testing.T) {
	local := net.ParseIP("2001:db8::2")
	remote := net.ParseIP("2001:db8::1")
	ue, server := newTransportPair(t, EncryptionAES, local, remote)
	request := ipv6ExtensionUDPPacket(t, local, 41000, remote, 51001, []byte("REGISTER"), false)
	protected, transformed, err := ue.TransformOutbound(request)
	if err != nil || !transformed {
		t.Fatalf("protect IPv6 extensions = transformed:%t err:%v", transformed, err)
	}
	if protected[6] != 0 || protected[40] != protocolESP {
		t.Fatalf("next-header chain was not preserved: base=%d extension=%d", protected[6], protected[40])
	}
	parsed, err := parseIPPacket(protected)
	if err != nil || parsed.headerLength != 48 || parsed.protocol != protocolESP {
		t.Fatalf("protected IPv6 parse = %+v, %v", parsed, err)
	}
	decoded, transformed, err := server.TransformInbound(protected)
	if err != nil || !transformed || !bytes.Equal(decoded, request) {
		t.Fatalf("IPv6 extension round trip = transformed:%t err:%v\n got %x\nwant %x", transformed, err, decoded, request)
	}
}

func TestTransportPassesNonInitialIPv6Fragment(t *testing.T) {
	local := net.ParseIP("2001:db8::2")
	remote := net.ParseIP("2001:db8::1")
	ue, _ := newTransportPair(t, EncryptionAES, local, remote)
	fragment := ipv6ExtensionUDPPacket(t, local, 41000, remote, 51001, []byte("fragment"), true)
	got, transformed, err := ue.TransformOutbound(fragment)
	if err != nil || transformed || !bytes.Equal(got, fragment) {
		t.Fatalf("fragment passthrough = transformed:%t err:%v packet:%x", transformed, err, got)
	}
	fragment[50], fragment[51] = 0, 0
	got, transformed, err = ue.TransformOutbound(fragment)
	if err != nil || !transformed || bytes.Equal(got, fragment) {
		t.Fatalf("atomic fragment = transformed:%t err:%v", transformed, err)
	}
}

func TestTransportPassesUnmatchedTrafficInBothDirections(t *testing.T) {
	ue, _ := newTransportPair(t, EncryptionAES, nil, nil)
	dns := udpPacket(t, net.IPv4(10, 0, 0, 2), 42000, net.IPv4(10, 0, 0, 1), 53, []byte("dns"))
	got, transformed, err := ue.TransformOutbound(dns)
	if err != nil || transformed || !bytes.Equal(got, dns) || &got[0] == &dns[0] {
		t.Fatalf("outbound passthrough = transformed:%t err:%v", transformed, err)
	}
	unprotected := udpPacket(t, net.IPv4(10, 0, 0, 1), 51001, net.IPv4(10, 0, 0, 2), 41000, []byte("200 OK"))
	got, transformed, err = ue.TransformInbound(unprotected)
	if err != nil || transformed || !bytes.Equal(got, unprotected) || &got[0] == &unprotected[0] {
		t.Fatalf("inbound passthrough = transformed:%t err:%v", transformed, err)
	}
	if stats := ue.Stats(); stats.PassthroughPackets != 2 {
		t.Fatalf("passthrough stats = %+v", stats)
	}
}

func TestTransportRejectsTamperingAndReportsStats(t *testing.T) {
	ue, server := newTransportPair(t, EncryptionNull, nil, nil)
	request := udpPacket(t, net.IPv4(10, 0, 0, 2), 41000, net.IPv4(10, 0, 0, 1), 51001, []byte("REGISTER"))
	protected, _, err := ue.TransformOutbound(request)
	if err != nil {
		t.Fatalf("protect request: %v", err)
	}
	protected[len(protected)-1] ^= 0xff
	if _, transformed, err := server.TransformInbound(protected); err == nil || !transformed {
		t.Fatalf("tampered packet = transformed:%t err:%v", transformed, err)
	}
	if stats := server.Stats(); stats.TransformErrors != 1 || stats.InboundPackets != 0 || stats.Replay.Accepted != 0 {
		t.Fatalf("stats after tampering = %+v", stats)
	}
}

func TestReplaceIPPayloadMatchesOriginalIPv6Bounds(t *testing.T) {
	packet := make([]byte, 48)
	packet[0] = 0x60
	replacement := payloadReplacement{
		packet:   packet,
		parsed:   ipPacket{version: 6, headerLength: 48, nextHeaderOffset: 40},
		protocol: protocolESP, payload: make([]byte, 0xffff),
	}
	got, err := replaceIPPayload(replacement)
	if err != nil {
		t.Fatalf("replace maximum IPv6 payload: %v", err)
	}
	if payloadLength := binary.BigEndian.Uint16(got[4:6]); payloadLength != 7 {
		t.Fatalf("wrapped IPv6 payload length = %d, want 7", payloadLength)
	}
	replacement.payload = make([]byte, 0x10000)
	if _, err := replaceIPPayload(replacement); err == nil || err.Error() != "ipsec3gpp: IPv6 payload too large" {
		t.Fatalf("oversized IPv6 payload error = %v", err)
	}
	replacement.parsed.version = 5
	if _, err := replaceIPPayload(replacement); err == nil || err.Error() != "ipsec3gpp: unsupported IP version" {
		t.Fatalf("unsupported replacement version error = %v", err)
	}
}

func newTransportPair(t *testing.T, encryption string, local, remote net.IP) (*Transport, *Transport) {
	t.Helper()
	uePolicy := testPolicy(local, remote, encryption)
	return transportPairFromPolicy(t, uePolicy)
}

func testPolicy(local, remote net.IP, encryption string) Policy {
	if local == nil {
		local = net.IPv4(10, 0, 0, 2)
	}
	if remote == nil {
		remote = net.IPv4(10, 0, 0, 1)
	}
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	return Policy{
		LocalIP: local, RemoteIP: remote,
		LocalPortC: 41000, LocalPortS: 41001, RemotePortC: 51000, RemotePortS: 51001,
		FlowC: Flow{
			OutboundSPI: 0x44444444, InboundSPI: 0x11111111, LocalPort: 41000, RemotePort: 51001,
			AuthAlg: AuthHMACSHA196, EncAlg: encryption, CK: ck, IK: ik,
		},
		FlowS: Flow{
			OutboundSPI: 0x33333333, InboundSPI: 0x22222222, LocalPort: 41001, RemotePort: 51000,
			AuthAlg: AuthHMACSHA196, EncAlg: encryption, CK: ck, IK: ik,
		},
	}
}

func reverseFlow(flow Flow) Flow {
	return Flow{
		OutboundSPI: flow.InboundSPI, InboundSPI: flow.OutboundSPI,
		LocalPort: flow.RemotePort, RemotePort: flow.LocalPort,
		AuthAlg: flow.AuthAlg, EncAlg: flow.EncAlg, CK: flow.CK, IK: flow.IK,
	}
}

func udpPacket(t *testing.T, source net.IP, sourcePort uint16, destination net.IP, destinationPort uint16, payload []byte) []byte {
	t.Helper()
	packet := make([]byte, 28+len(payload))
	packet[0], packet[8], packet[9] = 0x45, 64, protocolUDP
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], source.To4())
	copy(packet[16:20], destination.To4())
	binary.BigEndian.PutUint16(packet[20:22], sourcePort)
	binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+len(payload)))
	copy(packet[28:], payload)
	updateIPv4HeaderChecksum(packet[:20])
	return packet
}

func ipv6ExtensionUDPPacket(t *testing.T, source net.IP, sourcePort uint16, destination net.IP, destinationPort uint16, payload []byte, fragmented bool) []byte {
	t.Helper()
	headerLength := 48
	if fragmented {
		headerLength = 56
	}
	packet := make([]byte, headerLength+8+len(payload))
	packet[0], packet[6], packet[7] = 0x60, 0, 64
	copy(packet[8:24], source.To16())
	copy(packet[24:40], destination.To16())
	if fragmented {
		packet[40] = 44
		packet[48] = protocolUDP
		binary.BigEndian.PutUint16(packet[50:52], 1)
	} else {
		packet[40] = protocolUDP
	}
	binary.BigEndian.PutUint16(packet[4:6], uint16(len(packet)-40))
	binary.BigEndian.PutUint16(packet[headerLength:headerLength+2], sourcePort)
	binary.BigEndian.PutUint16(packet[headerLength+2:headerLength+4], destinationPort)
	binary.BigEndian.PutUint16(packet[headerLength+4:headerLength+6], uint16(8+len(payload)))
	copy(packet[headerLength+8:], payload)
	return packet
}

func assertESP(t *testing.T, packet []byte, spi uint32) {
	t.Helper()
	parsed, err := parseIPPacket(packet)
	if err != nil || parsed.protocol != protocolESP || len(parsed.payload) < 8 || binary.BigEndian.Uint32(parsed.payload[:4]) != spi {
		t.Fatalf("invalid ESP packet: %x, %v", packet, err)
	}
}
