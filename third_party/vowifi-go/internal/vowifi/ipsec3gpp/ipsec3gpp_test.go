package ipsec3gpp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"net"
	"testing"
)

func TestDerive3DESKeyFromCK(t *testing.T) {
	key, err := Derive3DESKeyFromCK(bytes.Repeat([]byte{0x11}, 16))
	if err != nil {
		t.Fatalf("Derive3DESKeyFromCK: %v", err)
	}
	if len(key) != 24 || !bytes.Equal(key[16:], key[:8]) {
		t.Fatalf("invalid 3DES key: %x", key)
	}
	for index, value := range key {
		if bitsSet(value)%2 == 0 {
			t.Fatalf("key byte %d does not have odd parity", index)
		}
	}
}

func bitsSet(value byte) int {
	count := 0
	for bit := 0; bit < 8; bit++ {
		if value&(1<<bit) != 0 {
			count++
		}
	}
	return count
}

func TestReplayWindow(t *testing.T) {
	window := NewReplayWindow(32)
	for _, sequence := range []uint32{1, 2, 100, 99} {
		if !window.Accept(sequence) {
			t.Fatalf("sequence %d was unexpectedly rejected", sequence)
		}
	}
	for _, sequence := range []uint32{0, 1, 99} {
		if window.Accept(sequence) {
			t.Fatalf("sequence %d was unexpectedly accepted", sequence)
		}
	}
}

func TestESPTransportProtectsBothDirections(t *testing.T) {
	ue, server := newTransportPair(t, EncryptionAES)
	request := udpPacket(t, "10.0.0.2", 41000, "10.0.0.1", 51001, []byte("REGISTER"))
	protected, err := ue.TransformOutbound(request)
	if err != nil {
		t.Fatalf("protect request: %v", err)
	}
	assertESP(t, protected, 0x44444444)
	decoded, err := server.TransformInbound(protected)
	if err != nil {
		t.Fatalf("unprotect request: %v", err)
	}
	if !bytes.Equal(decoded, request) {
		t.Fatalf("request round trip mismatch\n got %x\nwant %x", decoded, request)
	}
	if _, err := server.TransformInbound(protected); err == nil {
		t.Fatal("replayed ESP packet was accepted")
	}

	response := udpPacket(t, "10.0.0.1", 51001, "10.0.0.2", 41000, []byte("200 OK"))
	protected, err = server.TransformOutbound(response)
	if err != nil {
		t.Fatalf("protect response: %v", err)
	}
	assertESP(t, protected, 0x11111111)
	decoded, err = ue.TransformInbound(protected)
	if err != nil {
		t.Fatalf("unprotect response: %v", err)
	}
	if !bytes.Equal(decoded, response) {
		t.Fatal("response round trip mismatch")
	}
}

func TestESPTransportProtectsIPv6(t *testing.T) {
	local := net.ParseIP("2001:db8::2")
	remote := net.ParseIP("2001:db8::1")
	ue, server := newTransportPairForIPs(t, EncryptionAES, local, remote)
	request := ipv6UDPPacket(t, local, 41000, remote, 51001, []byte("REGISTER"))
	protected, err := ue.TransformOutbound(request)
	if err != nil {
		t.Fatalf("protect IPv6 request: %v", err)
	}
	parsed, err := parseIPPacket(protected)
	if err != nil || parsed.version != 6 || parsed.protocol != protocolESP {
		t.Fatalf("protected IPv6 packet = %x, %v", protected, err)
	}
	decoded, err := server.TransformInbound(protected)
	if err != nil {
		t.Fatalf("unprotect IPv6 request: %v", err)
	}
	if !bytes.Equal(decoded, request) {
		t.Fatalf("IPv6 request round trip mismatch\n got %x\nwant %x", decoded, request)
	}
}

func TestESPTransportRejectsTamperedIntegrity(t *testing.T) {
	ue, server := newTransportPair(t, EncryptionNull)
	request := udpPacket(t, "10.0.0.2", 41000, "10.0.0.1", 51001, []byte("REGISTER"))
	protected, err := ue.TransformOutbound(request)
	if err != nil {
		t.Fatalf("protect request: %v", err)
	}
	protected[len(protected)-1] ^= 0xff
	if _, err := server.TransformInbound(protected); err == nil {
		t.Fatal("tampered ESP packet was accepted")
	}
}

func TestESPOutputMatchesRFC4303WireFormat(t *testing.T) {
	ue, _ := newTransportPair(t, EncryptionAES)
	request := udpPacket(t, "10.0.0.2", 41000, "10.0.0.1", 51001, []byte("REGISTER"))
	protected, err := ue.TransformOutbound(request)
	if err != nil {
		t.Fatalf("protect request: %v", err)
	}
	esp := protected[20:]
	content, receivedICV := esp[:len(esp)-espICVLength], esp[len(esp)-espICVLength:]
	integrityKey := append(bytes.Repeat([]byte{0x22}, 16), make([]byte, 4)...)
	mac := hmac.New(sha1.New, integrityKey)
	_, _ = mac.Write(content)
	if !hmac.Equal(receivedICV, mac.Sum(nil)[:espICVLength]) {
		t.Fatal("ESP ICV does not match HMAC-SHA1-96")
	}
	block, err := aes.NewCipher(bytes.Repeat([]byte{0x11}, 16))
	if err != nil {
		t.Fatalf("AES cipher: %v", err)
	}
	iv, ciphertext := content[8:24], content[24:]
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	decoded, nextHeader, err := removeESPTrailer(plaintext)
	if err != nil {
		t.Fatalf("decode ESP trailer: %v", err)
	}
	if nextHeader != protocolUDP || !bytes.Equal(decoded, request[20:]) {
		t.Fatalf("ESP plaintext is not the original UDP segment: %x", decoded)
	}
}

func TestESPTransportPassesUnmatchedTrafficAndRejectsUnprotectedSelector(t *testing.T) {
	ue, _ := newTransportPair(t, EncryptionAES)
	dns := udpPacket(t, "10.0.0.2", 42000, "10.0.0.1", 53, []byte("dns"))
	got, err := ue.TransformOutbound(dns)
	if err != nil || !bytes.Equal(got, dns) {
		t.Fatalf("unmatched packet = %x, %v", got, err)
	}
	unprotected := udpPacket(t, "10.0.0.1", 51001, "10.0.0.2", 41000, []byte("200 OK"))
	if _, err := ue.TransformInbound(unprotected); err == nil {
		t.Fatal("unprotected packet on protected selector was accepted")
	}
}

func newTransportPair(t *testing.T, encryption string) (*Transport, *Transport) {
	t.Helper()
	return newTransportPairForIPs(t, encryption, net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.1"))
}

func newTransportPairForIPs(t *testing.T, encryption string, local, remote net.IP) (*Transport, *Transport) {
	t.Helper()
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	uePolicy := Policy{
		LocalIP: local, RemoteIP: remote,
		LocalClientPort: 41000, LocalServerPort: 41001,
		RemoteClientPort: 51000, RemoteServerPort: 51001,
		LocalClientSPI: 0x11111111, LocalServerSPI: 0x22222222,
		RemoteClientSPI: 0x33333333, RemoteServerSPI: 0x44444444,
		Authentication: AuthHMACSHA196, Encryption: encryption, Protocol: ProtocolESP, Mode: ModeTransport,
		CK: ck, IK: ik,
	}
	serverPolicy := Policy{
		LocalIP: uePolicy.RemoteIP, RemoteIP: uePolicy.LocalIP,
		LocalClientPort: uePolicy.RemoteClientPort, LocalServerPort: uePolicy.RemoteServerPort,
		RemoteClientPort: uePolicy.LocalClientPort, RemoteServerPort: uePolicy.LocalServerPort,
		LocalClientSPI: uePolicy.RemoteClientSPI, LocalServerSPI: uePolicy.RemoteServerSPI,
		RemoteClientSPI: uePolicy.LocalClientSPI, RemoteServerSPI: uePolicy.LocalServerSPI,
		Authentication: uePolicy.Authentication, Encryption: encryption, Protocol: ProtocolESP, Mode: ModeTransport,
		CK: ck, IK: ik,
	}
	ue, err := NewTransport(uePolicy)
	if err != nil {
		t.Fatalf("new UE transport: %v", err)
	}
	server, err := NewTransport(serverPolicy)
	if err != nil {
		t.Fatalf("new server transport: %v", err)
	}
	return ue, server
}

func ipv6UDPPacket(t *testing.T, source net.IP, sourcePort uint16, destination net.IP, destinationPort uint16, payload []byte) []byte {
	t.Helper()
	const ipv6HeaderLength = 40
	packet := make([]byte, ipv6HeaderLength+8+len(payload))
	packet[0] = 0x60
	packet[6] = protocolUDP
	packet[7] = 64
	binary.BigEndian.PutUint16(packet[4:6], uint16(8+len(payload)))
	copy(packet[8:24], source.To16())
	copy(packet[24:40], destination.To16())
	binary.BigEndian.PutUint16(packet[40:42], sourcePort)
	binary.BigEndian.PutUint16(packet[42:44], destinationPort)
	binary.BigEndian.PutUint16(packet[44:46], uint16(8+len(payload)))
	copy(packet[48:], payload)
	return packet
}

func udpPacket(t *testing.T, source string, sourcePort uint16, destination string, destinationPort uint16, payload []byte) []byte {
	t.Helper()
	packet := make([]byte, 20+8+len(payload))
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = protocolUDP
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], net.ParseIP(source).To4())
	copy(packet[16:20], net.ParseIP(destination).To4())
	binary.BigEndian.PutUint16(packet[20:22], sourcePort)
	binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+len(payload)))
	copy(packet[28:], payload)
	updateIPv4HeaderChecksum(packet[:20])
	return packet
}

func assertESP(t *testing.T, packet []byte, spi uint32) {
	t.Helper()
	parsed, err := parseIPPacket(packet)
	if err != nil {
		t.Fatalf("parse protected packet: %v", err)
	}
	if parsed.protocol != protocolESP || len(parsed.payload) < 8 || binary.BigEndian.Uint32(parsed.payload[:4]) != spi {
		t.Fatalf("invalid ESP packet: %x", packet)
	}
}
