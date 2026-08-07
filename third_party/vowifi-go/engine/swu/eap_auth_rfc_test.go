package swu

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

type recordingAKAProvider struct {
	result     enginesim.AKAResult
	rand, autn []byte
}

func (p *recordingAKAProvider) CalculateAKA(rand16, autn16 []byte) (enginesim.AKAResult, error) {
	p.rand = append([]byte(nil), rand16...)
	p.autn = append([]byte(nil), autn16...)
	return p.result, nil
}

func TestRFCChallengePassesExactSIMInputsAndStoresMSK(t *testing.T) {
	rand16 := bytes.Repeat([]byte{0x33}, 16)
	autn16 := bytes.Repeat([]byte{0x44}, 16)
	result := enginesim.AKAResult{
		RES: bytes.Repeat([]byte{0x55}, 8),
		CK:  bytes.Repeat([]byte{0x11}, 16),
		IK:  bytes.Repeat([]byte{0x22}, 16),
	}
	provider := &recordingAKAProvider{result: result}
	session := NewSession(&Config{IMSI: "234102356143376", AKAProvider: provider})
	session.socket = newTestIKETransport()
	session.ikeKeys = testIKEKeys()

	challenge := signedAKAChallenge(t, session.currentEAPIdentity(), rand16, autn16, result)
	if err := session.handleRFCChallenge(challenge); err != nil {
		t.Fatalf("handleRFCChallenge: %v", err)
	}
	if !bytes.Equal(provider.rand, rand16) || !bytes.Equal(provider.autn, autn16) {
		t.Fatalf("SIM input RAND=%x AUTN=%x", provider.rand, provider.autn)
	}
	wantKeys, err := eapaka.DeriveKeys(session.currentEAPIdentity(), result)
	if err != nil {
		t.Fatalf("derive expected keys: %v", err)
	}
	if !bytes.Equal(session.eapKeys.MSK, wantKeys.MSK) {
		t.Fatalf("MSK = %x, want %x", session.eapKeys.MSK, wantKeys.MSK)
	}
}

func signedAKAChallenge(t *testing.T, identity string, rand16, autn16 []byte, result enginesim.AKAResult) eapaka.Packet {
	t.Helper()
	keys, err := eapaka.DeriveKeys(identity, result)
	if err != nil {
		t.Fatalf("derive keys: %v", err)
	}
	packet := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: 7, Type: eapaka.TypeAKA,
		Subtype: eapaka.SubtypeChallenge,
		Attributes: []eapaka.Attribute{
			eapaka.RANDAttribute(rand16), eapaka.AUTNAttribute(autn16), eapaka.MACAttribute(nil),
		},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal challenge: %v", err)
	}
	mac, err := eapaka.CalculateMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	return packet
}

func TestComputeEAPInitiatorAuthUsesSignedOctetsAndMSK(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	session.ikeKeys = testIKEKeys()
	session.ikeSAInitRequest = []byte("ike-sa-init-request")
	session.nr = []byte("responder-nonce")
	session.eapKeys = eapaka.Keys{MSK: bytes.Repeat([]byte{0x7a}, eapaka.KeyLengthMSK)}

	auth, err := session.computeEAPInitiatorAuth()
	if err != nil {
		t.Fatalf("computeEAPInitiatorAuth: %v", err)
	}
	idType, idData := session.currentIKEIdentity()
	macedID := hmacSHA1(session.ikeKeys.SK_pi, identityPayloadBody(idType, idData))
	signed := append([]byte(nil), session.ikeSAInitRequest...)
	signed = append(signed, session.nr...)
	signed = append(signed, macedID...)
	sharedKey := hmacSHA1(session.eapKeys.MSK, []byte(ikev2KeyPad))
	want := hmacSHA1(sharedKey, signed)
	if auth.AuthMethod != ikev2.AuthMethodPSK || !bytes.Equal(auth.Data, want) {
		t.Fatalf("AUTH method=%d data=%x, want method=%d data=%x", auth.AuthMethod, auth.Data, ikev2.AuthMethodPSK, want)
	}
}

func TestInitialEAPIKEAuthOmitsAuthAndEAP(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	payloads, err := session.buildIKEAuthInitPayloads()
	if err != nil {
		t.Fatalf("buildIKEAuthInitPayloads: %v", err)
	}
	want := []byte{ikev2.PayloadIDi, ikev2.PayloadCP, ikev2.PayloadSA, ikev2.PayloadTSi, ikev2.PayloadTSr}
	if len(payloads) != len(want) {
		t.Fatalf("payload count = %d, want %d", len(payloads), len(want))
	}
	for index, payload := range payloads {
		if payload.Type() != want[index] {
			t.Fatalf("payload[%d] = %d, want %d", index, payload.Type(), want[index])
		}
	}
}

func hmacSHA1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
