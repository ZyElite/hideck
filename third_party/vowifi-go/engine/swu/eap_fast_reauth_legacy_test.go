package swu

import (
	"bytes"
	"testing"

	engineeap "github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func TestConfiguredFastReauthenticationIdentityIsUsed(t *testing.T) {
	session := NewSession(&Config{
		IMSI: "001010123456789", FastReauthID: "reauth@example",
		FastReauthMK: []byte{1}, FastReauthKEncr: []byte{2}, FastReauthKAut: []byte{3},
	})
	if got := session.currentEAPIdentity(); got != "reauth@example" {
		t.Fatalf("currentEAPIdentity() = %q", got)
	}
}

func TestChallengeCapturesFastReauthenticationState(t *testing.T) {
	keys := eapaka.Keys{
		MK: bytes.Repeat([]byte{0x11}, 20), KEncr: bytes.Repeat([]byte{0x22}, 16),
		KAut: bytes.Repeat([]byte{0x33}, 16),
	}
	iv := bytes.Repeat([]byte{0x44}, 16)
	encrypted, err := eapaka.EncryptAttributes(keys.KEncr, iv, []eapaka.Attribute{
		eapaka.NextReauthIDAttribute("next@example"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var callbackID string
	session := NewSession(&Config{OnFastReauthUpdate: func(id string, _, _, _ []byte) {
		callbackID = id
	}})
	request := eapaka.Packet{Attributes: []eapaka.Attribute{eapaka.IVAttribute(iv), encrypted}}
	if err := session.captureFastReauthentication(request, keys); err != nil {
		t.Fatal(err)
	}
	if !session.fastReauthCtx.CanUseReauth() || session.fastReauthCtx.ReauthID != "next@example" {
		t.Fatalf("captured context = %+v", session.fastReauthCtx)
	}
	if callbackID != "next@example" {
		t.Fatalf("callback ID = %q", callbackID)
	}
}

func TestLegacyFastReauthenticationMACVerification(t *testing.T) {
	for _, test := range []struct {
		eapType uint8
		keySize int
	}{{eapaka.TypeAKA, 16}, {eapaka.TypeAKAPrime, 32}} {
		key := bytes.Repeat([]byte{0x31}, test.keySize)
		request := signedLegacyReauthenticationRequest(t, test.eapType, key)
		if err := verifyLegacyReauthenticationMAC(test.eapType, key, request); err != nil {
			t.Fatalf("type %d verify signed request: %v", test.eapType, err)
		}
		request[len(request)-1] ^= 1
		if err := verifyLegacyReauthenticationMAC(test.eapType, key, request); err == nil {
			t.Fatalf("type %d accepted tampered request", test.eapType)
		}
	}
}

func TestLegacyFastReauthenticationResponseIsSigned(t *testing.T) {
	key := bytes.Repeat([]byte{0x41}, 16)
	context := engineeap.NewFastReauthContext()
	context.SaveReauthData("reauth@example", []byte{1}, []byte{2}, key)
	data, err := context.BuildReauthResponse(bytes.Repeat([]byte{3}, 18), uint16(8), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signLegacyReauthentication(eapaka.Packet{
		Identifier: 9, Type: eapaka.TypeAKA,
	}, data, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyLegacyReauthenticationMAC(eapaka.TypeAKA, key, raw); err != nil {
		t.Fatalf("verify response: %v", err)
	}
}

func signedLegacyReauthenticationRequest(t *testing.T, eapType uint8, key []byte) []byte {
	t.Helper()
	counter := (&engineeap.Attribute{Type: engineeap.AT_COUNTER, Value: []byte{0, 4}}).Encode()
	nonce := (&engineeap.Attribute{
		Type: engineeap.AT_NONCE_S, Value: append([]byte{0, 0}, bytes.Repeat([]byte{0x51}, 16)...),
	}).Encode()
	mac := (&engineeap.Attribute{Type: engineeap.AT_MAC, Value: make([]byte, 18)}).Encode()
	data := append(append(counter, nonce...), mac...)
	raw := (&engineeap.EAPPacket{
		Code: engineeap.CodeRequest, Identifier: 7, Type: eapType,
		Subtype: engineeap.SubtypeReauthentication, Data: data,
	}).Encode()
	var signature []byte
	var err error
	if eapType == eapaka.TypeAKAPrime {
		signature, err = eapaka.CalculateAKAPrimeMAC(key, raw, nil)
	} else {
		signature, err = eapaka.CalculateMAC(key, raw, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	offset, ok := attributeOffset(data, engineeap.AT_MAC)
	if !ok {
		t.Fatal("MAC attribute not found")
	}
	copy(raw[8+offset+4:], signature)
	return raw
}
