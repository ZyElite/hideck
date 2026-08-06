package ipsec3gpp

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// netIP builds a 4-byte net.IP from 4 bytes.
func netIP(a, b, c, d byte) net.IP {
	return net.IPv4(a, b, c, d).To4()
}

func TestDerive3DESKeyFromCK(t *testing.T) {
	ck := bytes.Repeat([]byte{0x11}, 16)
	key, err := Derive3DESKeyFromCK(ck)
	if err != nil {
		t.Fatalf("Derive3DESKeyFromCK: %v", err)
	}
	if len(key) != 24 {
		t.Fatalf("key len = %d, want 24", len(key))
	}
	// The last 8 bytes repeat the first 8 (with parity adjustment).
	if !bytes.Equal(key[16:], key[:8]) {
		t.Error("key[16:] should repeat key[:8]")
	}
	// The whole byte should have odd parity (DES key convention).
	for i, b := range key {
		ones := 0
		for j := 0; j < 8; j++ {
			if b&(1<<j) != 0 {
				ones++
			}
		}
		if ones%2 == 0 {
			t.Errorf("byte %d has even parity", i)
		}
	}
}

func TestDeriveSecureChannelKeys(t *testing.T) {
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	keys, err := DeriveSecureChannelKeys(ck, ik)
	if err != nil {
		t.Fatalf("DeriveSecureChannelKeys: %v", err)
	}
	if len(keys.EncKey) != 24 {
		t.Errorf("enc key len = %d", len(keys.EncKey))
	}
	if len(keys.AuthKey) != 16 {
		t.Errorf("auth key len = %d", len(keys.AuthKey))
	}
}

func TestReplayWindow(t *testing.T) {
	w := NewReplayWindow(32)
	if !w.Accept(1) {
		t.Error("first packet should be accepted")
	}
	if w.Accept(1) {
		t.Error("duplicate should be rejected")
	}
	if !w.Accept(2) {
		t.Error("next sequence should be accepted")
	}
	if !w.Accept(100) {
		t.Error("jump ahead should be accepted")
	}
	if w.Accept(1) {
		t.Error("old sequence should be rejected")
	}
	// Within the window.
	if !w.Accept(99) {
		t.Error("recent old sequence should be accepted once")
	}
	if w.Accept(99) {
		t.Error("duplicate recent should be rejected")
	}
}

func TestTransportRoundTrip(t *testing.T) {
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	keys, err := DeriveSecureChannelKeys(ck, ik)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	tr := NewTransport()
	tr.AddFlow(netIP(10, 0, 0, 1), netIP(10, 0, 0, 2), 0x12345678, keys)

	inner := make([]byte, 20)
	inner[0] = 0x45
	copy(inner[12:16], netIP(10, 0, 0, 1))
	copy(inner[16:20], netIP(10, 0, 0, 2))

	out, err := tr.TransformOutbound(inner)
	if err != nil {
		t.Fatalf("TransformOutbound: %v", err)
	}
	// The outbound transform encrypts the payload; the SPI is prepended for
	// inbound matching.
	encrypted := append([]byte{0x12, 0x34, 0x56, 0x78}, out...)
	back, err := tr.TransformInbound(encrypted)
	if err != nil {
		t.Fatalf("TransformInbound: %v", err)
	}
	if !bytes.Equal(back, inner) {
		t.Errorf("round trip mismatch: %x vs %x", back, inner)
	}
}

func TestParseIPPacket(t *testing.T) {
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[12:16], netIP(10, 0, 0, 1))
	copy(pkt[16:20], netIP(10, 0, 0, 2))
	ip, err := parseIPPacket(pkt)
	if err != nil {
		t.Fatalf("parseIPPacket: %v", err)
	}
	if ip.version != 4 || !ip.src.Equal(netIP(10, 0, 0, 1)) || !ip.dst.Equal(netIP(10, 0, 0, 2)) {
		t.Errorf("parsed = %+v", ip)
	}
}

func TestUpdateIPv4HeaderChecksum(t *testing.T) {
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], 20)
	updateIPv4HeaderChecksum(pkt)
	// Verify the checksum.
	sum := uint32(0)
	for i := 0; i < 20; i += 2 {
		sum += uint32(pkt[i])<<8 | uint32(pkt[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	if ^uint16(sum) != 0 {
		t.Error("checksum invalid")
	}
}

func TestReplaceIPPayload(t *testing.T) {
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[12:16], netIP(10, 0, 0, 1))
	copy(pkt[16:20], netIP(10, 0, 0, 2))
	out, err := replaceIPPayload(pkt, []byte("hello"))
	if err != nil {
		t.Fatalf("replaceIPPayload: %v", err)
	}
	if len(out) != 25 {
		t.Errorf("len = %d, want 25", len(out))
	}
	if string(out[20:]) != "hello" {
		t.Error("payload not replaced")
	}
}
