package eap

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestRecoveredTypeLayouts(t *testing.T) {
	if size := unsafe.Sizeof(EAPPacket{}); size != 32 {
		t.Fatalf("EAPPacket size = %d, want 32", size)
	}
	if size := unsafe.Sizeof(Attribute{}); size != 32 {
		t.Fatalf("Attribute size = %d, want 32", size)
	}
	if size := unsafe.Sizeof(FastReauthContext{}); size != 136 {
		t.Fatalf("FastReauthContext size = %d, want 136", size)
	}
}

func TestRecoveredConstants(t *testing.T) {
	if SubtypeReauthentication != 13 || AT_PADDING != 6 || AT_CLIENT_ERROR_CODE != 22 {
		t.Fatalf("recovered constants = subtype:%d padding:%d client-error:%d",
			SubtypeReauthentication, AT_PADDING, AT_CLIENT_ERROR_CODE)
	}
	if AT_KDF_INPUT != 23 || AT_KDF != 24 || AT_NEXT_REAUTH_ID != 133 {
		t.Fatalf("AKA prime/reauth constants = %d/%d/%d", AT_KDF_INPUT, AT_KDF, AT_NEXT_REAUTH_ID)
	}
}

func TestAttributeEncodePadsAndUpdatesLength(t *testing.T) {
	attribute := &Attribute{Type: AT_IDENTITY, Value: []byte{1, 2, 3}}
	encoded := attribute.Encode()
	if attribute.Length != 2 || !bytes.Equal(encoded, []byte{AT_IDENTITY, 2, 1, 2, 3, 0, 0, 0}) {
		t.Fatalf("encoded attribute = %x, length=%d", encoded, attribute.Length)
	}
}

func TestParseAndAttributesRetainInputViews(t *testing.T) {
	packetBytes := (&EAPPacket{
		Code: CodeRequest, Identifier: 7, Type: TypeAKA,
		Subtype: SubtypeChallenge, Data: []byte{AT_COUNTER, 1, 0, 3},
	}).Encode()
	packet, err := Parse(packetBytes)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := ParseAttributes(packet.Data)
	if err != nil {
		t.Fatal(err)
	}
	packetBytes[10] = 9
	if attributes[AT_COUNTER].Value[0] != 9 {
		t.Fatal("attribute value no longer aliases the packet buffer")
	}
}

func TestRecoveredParseErrorsAndTrailingByte(t *testing.T) {
	if _, err := Parse([]byte{1, 2, 3}); err == nil || err.Error() != "EAP packet too short" {
		t.Fatalf("short packet error = %v", err)
	}
	if _, err := Parse([]byte{1, 2, 0, 8}); err == nil || err.Error() != "EAP length exceeds data" {
		t.Fatalf("length error = %v", err)
	}
	attributes, err := ParseAttributes([]byte{0xff})
	if err != nil || len(attributes) != 0 {
		t.Fatalf("trailing byte = %#v, %v", attributes, err)
	}
	if _, err := ParseAttributes([]byte{AT_RAND, 0}); err == nil || err.Error() != "attribute length zero" {
		t.Fatalf("zero attribute error = %v", err)
	}
}

func TestRecoveredNotificationDescriptions(t *testing.T) {
	tests := map[uint16]string{
		32768:  "[IANA] Success (成功)",
		10500:  "[3GPP] APN not subscribed (APN 未签约)",
		0x4002: "未知通知码 16386 (阶段: 认证前, 类型: 需要 Success/Failure 结束)",
		0x8002: "未知通知码 32770 (阶段: 认证后, 类型: 纯通知)",
	}
	for code, want := range tests {
		if got := NotificationCodeToString(code); got != want {
			t.Errorf("NotificationCodeToString(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestOriginalFastReauthenticationContract(t *testing.T) {
	context := NewFastReauthContext()
	mk := []byte{1}
	kEncr := []byte{2}
	kAut := []byte{3}
	context.SaveReauthData("reauth@example", mk, kEncr, kAut)
	if !context.CanUseReauth() || context.ReauthID != "reauth@example" {
		t.Fatalf("saved context = %+v", context)
	}
	mk[0] = 9
	if context.MK[0] != 9 {
		t.Fatal("original SaveReauthData no longer retains supplied key slices")
	}
	nonce := []byte{4, 5}
	response, err := context.BuildReauthResponse(nonce, uint16(7), false)
	if err != nil {
		t.Fatal(err)
	}
	if context.Counter != 7 || &context.NonceS[0] != &nonce[0] {
		t.Fatalf("response state = counter:%d nonce:%x", context.Counter, context.NonceS)
	}
	want := append([]byte{AT_COUNTER, 1, 0, 7, AT_MAC, 5, 0, 0}, make([]byte, 16)...)
	if !bytes.Equal(response, want) {
		t.Fatalf("reauth response = %x, want %x", response, want)
	}
	_, _ = context.BuildReauthResponse([]byte{6}, uint16(6), true)
	if context.Counter != 7 || !context.CounterSmall {
		t.Fatalf("counter-too-small state = %+v", context)
	}
}
