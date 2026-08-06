package eap

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEAPPacketAKARoundTrip(t *testing.T) {
	// EAP-AKA Challenge request: 8-byte header + 4 bytes of attr data.
	orig := &EAPPacket{Code: CodeRequest, Identifier: 0x55, Type: TypeAKA, SubType: SubtypeAKAChallenge, Data: []byte{0xaa, 0xbb, 0xcc, 0xdd}}
	enc := orig.Encode()
	if len(enc) != 12 {
		t.Fatalf("encoded length = %d, want 12", len(enc))
	}
	if binary.BigEndian.Uint16(enc[2:4]) != 12 {
		t.Errorf("length field = %d, want 12", binary.BigEndian.Uint16(enc[2:4]))
	}
	// Header: Code|ID|Len|Type|SubType|2 reserved.
	if enc[4] != TypeAKA || enc[5] != SubtypeAKAChallenge || enc[6] != 0 || enc[7] != 0 {
		t.Errorf("AKA header = %x", enc[4:8])
	}

	p, err := Parse(enc, len(enc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Code != orig.Code || p.Identifier != orig.Identifier || p.Type != orig.Type || p.SubType != orig.SubType {
		t.Errorf("parsed = %+v, want %+v", p, orig)
	}
	if !bytes.Equal(p.Data, orig.Data) {
		t.Errorf("parsed data = %x, want %x", p.Data, orig.Data)
	}
}

func TestEAPPacketIdentityRoundTrip(t *testing.T) {
	orig := &EAPPacket{Code: CodeResponse, Identifier: 0x01, Type: TypeIdentity, Data: []byte("user@nai")}
	enc := orig.Encode()
	if len(enc) != 5+len(orig.Data) {
		t.Fatalf("encoded length = %d, want %d", len(enc), 5+len(orig.Data))
	}
	p, err := Parse(enc, len(enc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Type != TypeIdentity || p.SubType != 0 {
		t.Errorf("parsed type/subtype = %d/%d", p.Type, p.SubType)
	}
	if string(p.Data) != "user@nai" {
		t.Errorf("parsed data = %q", p.Data)
	}
}

func TestEAPPacketSuccess(t *testing.T) {
	orig := &EAPPacket{Code: CodeSuccess, Identifier: 0x07}
	enc := orig.Encode()
	if len(enc) != 4 {
		t.Fatalf("encoded length = %d, want 4", len(enc))
	}
	p, err := Parse(enc, len(enc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Code != CodeSuccess || p.Identifier != 0x07 {
		t.Errorf("parsed = %+v", p)
	}
	if p.Data != nil {
		t.Errorf("Success should carry no data, got %x", p.Data)
	}
}

func TestEAPPacketParseErrors(t *testing.T) {
	if _, err := Parse([]byte{1, 2, 3}, 3); err == nil {
		t.Error("short packet should error")
	}
	// Length field exceeds buffer.
	if _, err := Parse([]byte{1, 2, 0, 10, 23}, 5); err == nil {
		t.Error("overlong length should error")
	}
}

func TestParseAttributes(t *testing.T) {
	// AT_RAND (type 1, 4 words = 16 bytes: 2 hdr + 14 value) + AT_AUTN (type 2, 4 words).
	b := []byte{}
	b = append(b, AttrATRAND, 0x04)
	b = append(b, bytes.Repeat([]byte{0x11}, 14)...)
	b = append(b, AttrATAUTN, 0x04)
	b = append(b, bytes.Repeat([]byte{0x22}, 14)...)

	attrs, err := ParseAttributes(b, len(b))
	if err != nil {
		t.Fatalf("ParseAttributes: %v", err)
	}
	rand, ok := attrs[AttrATRAND]
	if !ok || len(rand.Value) != 14 {
		t.Errorf("AT_RAND value len = %d", len(rand.Value))
	}
	autn, ok := attrs[AttrATAUTN]
	if !ok || len(autn.Value) != 14 {
		t.Errorf("AT_AUTN value len = %d", len(autn.Value))
	}
}

func TestParseAttributesErrors(t *testing.T) {
	if _, err := ParseAttributes([]byte{AttrATRAND, 0x00}, 2); err == nil {
		t.Error("zero length attribute should error")
	}
	if _, err := ParseAttributes([]byte{AttrATRAND, 0x04, 0, 0}, 4); err == nil {
		t.Error("attribute exceeding buffer should error")
	}
}

func TestNotificationCodeToString(t *testing.T) {
	if got := NotificationCodeToString(0x0000); got != "notification: general success" {
		t.Errorf("code 0 = %q", got)
	}
	got := NotificationCodeToString(0x8001)
	if got == "" {
		t.Error("fallback returned empty")
	}
}

func TestFastReauthContext(t *testing.T) {
	c := &FastReauthContext{}
	if c.CanUseReauth() {
		t.Error("CanUseReauth should be false before SaveReauthData")
	}
	c.SaveReauthData([]byte("identity"), []byte("reauth@nai"), bytes.Repeat([]byte{0x01}, 16))
	if !c.CanUseReauth() {
		t.Error("CanUseReauth should be true after SaveReauthData")
	}

	mac := bytes.Repeat([]byte{0xee}, 16)
	out, err := c.BuildReauthResponse(7, false, mac)
	if err != nil {
		t.Fatalf("BuildReauthResponse: %v", err)
	}
	// AT_COUNTER (4) + AT_MAC (20) = 24.
	if len(out) != 24 {
		t.Fatalf("reauth response length = %d, want 24", len(out))
	}
	if out[0] != AttrATCounter || out[1] != 1 {
		t.Errorf("AT_COUNTER header = %x", out[0:2])
	}
	if binary.BigEndian.Uint16(out[2:4]) != 7 {
		t.Errorf("counter = %d, want 7", binary.BigEndian.Uint16(out[2:4]))
	}
	if out[4] != AttrATMAC || out[5] != 5 {
		t.Errorf("AT_MAC header = %x", out[4:6])
	}
	if !bytes.Equal(out[8:], mac) {
		t.Error("AT_MAC value mismatch")
	}

	// With counterTooSmall, AT_COUNTER_TOO_SMALL (4 bytes) is inserted.
	out2, _ := c.BuildReauthResponse(7, true, mac)
	if len(out2) != 28 {
		t.Fatalf("reauth response with counter-too-small length = %d, want 28", len(out2))
	}
	if out2[4] != AttrATCounterTooSmall {
		t.Errorf("expected AT_COUNTER_TOO_SMALL at offset 4, got %x", out2[4])
	}
}