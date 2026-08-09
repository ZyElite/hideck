package swu

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const fragmentTestMTU = 300

func TestSKFPacketFormatAndCBCIntegrity(t *testing.T) {
	session := newFragmentTestSession(false)
	payload := fragmentTestPayload(900)
	parts, err := session.fragmentMessage([]ikev2.Payload{payload}, ikev2.IKE_AUTH)
	if err != nil {
		t.Fatalf("fragmentMessage() error = %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("fragment count = %d, want at least 2", len(parts))
	}

	var reassembled []byte
	var messageID uint32
	for index, part := range parts {
		header, generic, number, total := decodeFragmentMetadata(t, part)
		if index == 0 {
			messageID = header.MessageID
		}
		if header.NextPayload != ikev2.EncryptedFragment || header.MessageID != messageID {
			t.Fatalf("fragment %d header = %#v", index+1, header)
		}
		if number != uint16(index+1) || total != uint16(len(parts)) {
			t.Fatalf("fragment metadata = %d/%d", number, total)
		}
		wantNext := ikev2.NoNextPayload
		if index == 0 {
			wantNext = payload.Type()
		}
		if generic.NextPayload != wantNext {
			t.Fatalf("fragment %d next payload = %d, want %d", index+1, generic.NextPayload, wantNext)
		}
		if len(part)+20+8 > fragmentTestMTU {
			t.Fatalf("fragment %d wire size = %d, exceeds MTU %d", index+1, len(part)+28, fragmentTestMTU)
		}
		plain, gotNumber, gotTotal, gotID, err := session.decryptSKF(part)
		if err != nil {
			t.Fatalf("decryptSKF(%d) error = %v", index+1, err)
		}
		if gotNumber != number || gotTotal != total || gotID != messageID {
			t.Fatalf("decrypted metadata = %d/%d id=%d", gotNumber, gotTotal, gotID)
		}
		reassembled = append(reassembled, plain...)
	}
	assertFragmentPayload(t, reassembled, payload)

	tampered := append([]byte(nil), parts[0]...)
	tampered[len(tampered)-1] ^= 0xff
	if _, _, _, _, err := session.decryptSKF(tampered); err == nil {
		t.Fatal("decryptSKF() accepted a tampered CBC fragment")
	}
}

func TestSKFOutOfOrderReassemblyPreservesFirstPayload(t *testing.T) {
	session := newFragmentTestSession(false)
	payload := fragmentTestPayload(900)
	parts, err := session.fragmentMessage([]ikev2.Payload{payload}, ikev2.CREATE_CHILD_SA)
	if err != nil {
		t.Fatalf("fragmentMessage() error = %v", err)
	}
	var normalized []byte
	for index := len(parts) - 1; index >= 0; index-- {
		var complete bool
		normalized, complete, err = session.normalizeInboundIKE(parts[index])
		if err != nil {
			t.Fatalf("normalizeInboundIKE(%d) error = %v", index+1, err)
		}
		if complete != (index == 0) {
			t.Fatalf("fragment %d complete = %t", index+1, complete)
		}
	}
	packet, err := ikev2.DecodePacket(normalized)
	if err != nil {
		t.Fatalf("DecodePacket() error = %v", err)
	}
	payloads, err := session.decryptAndParse(packet)
	if err != nil {
		t.Fatalf("decryptAndParse() error = %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("reassembled payload count = %d", len(payloads))
	}
	got, ok := payloads[0].(*ikev2.RawPayload)
	if !ok || got.Type() != payload.Type() || !bytes.Equal(got.Data, payload.Data) {
		t.Fatal("out-of-order SKF reassembly changed the inner payload")
	}
}

func TestSKFAEADAuthenticatesFragmentHeader(t *testing.T) {
	session := newFragmentTestSession(true)
	payload := fragmentTestPayload(700)
	parts, err := session.fragmentMessage([]ikev2.Payload{payload}, ikev2.INFORMATIONAL)
	if err != nil {
		t.Fatalf("fragmentMessage() error = %v", err)
	}
	for index, part := range parts {
		if _, _, _, _, err := session.decryptSKF(part); err != nil {
			t.Fatalf("decryptSKF(%d) error = %v", index+1, err)
		}
	}
	tampered := append([]byte(nil), parts[0]...)
	total := binary.BigEndian.Uint16(tampered[34:36])
	binary.BigEndian.PutUint16(tampered[34:36], total+1)
	if _, _, _, _, err := session.decryptSKF(tampered); err == nil {
		t.Fatal("decryptSKF() accepted an AEAD fragment-header modification")
	}
}

func TestFragmentBufferRejectsInvalidAndConflictingFragments(t *testing.T) {
	buffer := newFragmentBuffer()
	invalid := []struct {
		number uint16
		total  uint16
	}{
		{number: 0, total: 1},
		{number: 1, total: 0},
		{number: 2, total: 1},
		{number: 1, total: maxFragments + 1},
	}
	for _, test := range invalid {
		if _, err := buffer.addFragment(1, test.number, test.total, []byte("x")); err == nil {
			t.Fatalf("addFragment(%d/%d) succeeded", test.number, test.total)
		}
	}
	if _, err := buffer.addReceivedFragment(receivedFragment{
		messageID: 2, number: 2, total: 2, firstPayload: ikev2.V, plaintext: []byte("b"),
	}); err == nil {
		t.Fatal("non-initial fragment declared a next payload")
	}
	if _, err := buffer.addFragment(3, 1, 2, []byte("a")); err != nil {
		t.Fatalf("addFragment() error = %v", err)
	}
	if _, err := buffer.addFragment(3, 1, 2, []byte("changed")); err == nil {
		t.Fatal("conflicting duplicate fragment was accepted")
	}
	firstEnvelope := &fragmentEnvelope{exchangeType: ikev2.IKE_AUTH, flags: ikeResponseFlag}
	if _, err := buffer.addReceivedFragment(receivedFragment{
		messageID: 4, number: 1, total: 2, firstPayload: ikev2.V,
		plaintext: []byte("a"), envelope: firstEnvelope,
	}); err != nil {
		t.Fatalf("addReceivedFragment() error = %v", err)
	}
	differentEnvelope := &fragmentEnvelope{exchangeType: ikev2.INFORMATIONAL, flags: ikeResponseFlag}
	if _, err := buffer.addReceivedFragment(receivedFragment{
		messageID: 4, number: 2, total: 2,
		plaintext: []byte("b"), envelope: differentEnvelope,
	}); err == nil {
		t.Fatal("fragment with a conflicting IKE envelope was accepted")
	}
}

func newFragmentTestSession(aead bool) *Session {
	session := NewSession(&Config{})
	session.spiI = [8]byte{7}
	session.spiR = [8]byte{9}
	session.ikeKeys = testIKEKeys()
	session.mu.Lock()
	session.fragmentationSupported = true
	session.ikeFragmentMTU = fragmentTestMTU
	session.mu.Unlock()
	if aead {
		session.encrAlg = uint16(ikev2.ENCR_AES_GCM_16)
		session.integAlg = uint16(ikev2.AUTH_NONE)
		session.aead = true
		session.ikeKeys.SK_ei = bytes.Repeat([]byte{0x33}, 20)
		session.ikeKeys.SK_er = bytes.Repeat([]byte{0x44}, 20)
	}
	return session
}

func fragmentTestPayload(size int) *ikev2.RawPayload {
	return &ikev2.RawPayload{PType: ikev2.V, Data: bytes.Repeat([]byte{0xa5}, size)}
}

func decodeFragmentMetadata(
	t *testing.T,
	data []byte,
) (*ikev2.IKEHeader, *ikev2.PayloadHeader, uint16, uint16) {
	t.Helper()
	header, generic, err := decodeSKFHeaders(data)
	if err != nil {
		t.Fatalf("decodeSKFHeaders() error = %v", err)
	}
	return header, generic, binary.BigEndian.Uint16(data[32:34]), binary.BigEndian.Uint16(data[34:36])
}

func assertFragmentPayload(t *testing.T, encoded []byte, want *ikev2.RawPayload) {
	t.Helper()
	payloads, err := ikev2.DecodePayloadChainWithFirst(want.Type(), encoded)
	if err != nil {
		t.Fatalf("DecodePayloadChainWithFirst() error = %v", err)
	}
	got, ok := payloads[0].(*ikev2.RawPayload)
	if len(payloads) != 1 || !ok || !bytes.Equal(got.Data, want.Data) {
		t.Fatal("decrypted fragments changed the inner payload")
	}
}
