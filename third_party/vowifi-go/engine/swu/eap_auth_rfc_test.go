package swu

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"testing"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
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
	return signedAKAChallengeWithResultIndication(t, identity, rand16, autn16, result, false)
}

func signedAKAChallengeWithResultIndication(t *testing.T, identity string, rand16, autn16 []byte, result enginesim.AKAResult, resultIndication bool) eapaka.Packet {
	t.Helper()
	keys, err := eapaka.DeriveKeys(identity, result)
	if err != nil {
		t.Fatalf("derive keys: %v", err)
	}
	attributes := []eapaka.Attribute{
		eapaka.RANDAttribute(rand16), eapaka.AUTNAttribute(autn16),
	}
	if resultIndication {
		attributes = append(attributes, eapaka.ResultIndAttribute())
	}
	attributes = append(attributes, eapaka.MACAttribute(nil))
	packet := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: 7, Type: eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeChallenge,
		Attributes: attributes,
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

func TestRFCResultIndicationWaitsForAuthenticatedNotificationAndSuccess(t *testing.T) {
	result := enginesim.AKAResult{
		RES: bytes.Repeat([]byte{0x55}, 8),
		CK:  bytes.Repeat([]byte{0x11}, 16),
		IK:  bytes.Repeat([]byte{0x22}, 16),
	}
	session := NewSession(&Config{IMSI: "234102356143376", AKAProvider: &recordingAKAProvider{result: result}})
	session.socket = newTestIKETransport()
	session.ikeKeys = testIKEKeys()
	session.stage = stageEAP
	challenge := signedAKAChallengeWithResultIndication(t, session.currentEAPIdentity(),
		bytes.Repeat([]byte{0x33}, 16), bytes.Repeat([]byte{0x44}, 16), result, true)
	if err := session.handleRFCChallenge(challenge); err != nil {
		t.Fatalf("handleRFCChallenge: %v", err)
	}
	if !session.eapResultIndicated || session.eapResultConfirmed {
		t.Fatalf("result state indicated=%t confirmed=%t", session.eapResultIndicated, session.eapResultConfirmed)
	}
	if err := session.handleRFCEAP([]byte{eapaka.CodeSuccess, 8, 0, 4}); err == nil {
		t.Fatal("accepted EAP Success before authenticated result notification")
	}

	notification := signedSuccessNotification(t, 8, session.eapKeys.KAut)
	rawNotification, err := notification.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	if err := session.handleRFCEAP(rawNotification); err != nil {
		t.Fatalf("handle notification: %v", err)
	}
	if !session.eapResultConfirmed || session.stage != stageEAP {
		t.Fatalf("notification state confirmed=%t stage=%d", session.eapResultConfirmed, session.stage)
	}
	if err := session.handleRFCEAP([]byte{eapaka.CodeSuccess, 9, 0, 4}); err != nil {
		t.Fatalf("handle EAP Success: %v", err)
	}
	if session.stage != stageFinal {
		t.Fatalf("stage=%d, want final", session.stage)
	}
}

func TestRFCResultIndicationRejectsBadNotificationMAC(t *testing.T) {
	session := NewSession(&Config{})
	session.socket = newTestIKETransport()
	session.ikeKeys = testIKEKeys()
	session.eapKeys = eapaka.Keys{KAut: bytes.Repeat([]byte{0x6a}, eapaka.KeyLengthKAut)}
	session.eapResultIndicated = true
	notification := signedSuccessNotification(t, 8, session.eapKeys.KAut)
	notification.Attributes[len(notification.Attributes)-1].Data[2] ^= 0xff
	raw, err := notification.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	if err := session.handleRFCEAP(raw); err == nil {
		t.Fatal("accepted result notification with invalid AT_MAC")
	}
}

func TestGenericEAPIdentityIsExcludedFromCheckcodeTranscript(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	session.socket = newTestIKETransport()
	session.ikeKeys = testIKEKeys()
	request := []byte{eapaka.CodeRequest, 3, 0, 5, eapTypeIdentity}
	if err := session.handleRFCEAP(request); err != nil {
		t.Fatalf("handle identity: %v", err)
	}
	if len(session.eapIdentityTranscript) != 0 {
		t.Fatalf("generic Identity entered AKA checkcode transcript: %x", session.eapIdentityTranscript)
	}
}

func TestAKAIdentityCheckcodeTranscriptDeduplicatesRetransmission(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	session.socket = newTestIKETransport()
	session.ikeKeys = testIKEKeys()
	request := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: 7, Type: eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeIdentity,
		Attributes: []eapaka.Attribute{eapaka.IdentityAttribute("any")},
	}
	raw, err := request.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal AKA Identity: %v", err)
	}
	if err := session.handleRFCEAP(raw); err != nil {
		t.Fatalf("first AKA Identity: %v", err)
	}
	if err := session.handleRFCEAP(raw); err != nil {
		t.Fatalf("retransmitted AKA Identity: %v", err)
	}
	if len(session.eapIdentityTranscript) != 2 || !bytes.Equal(session.eapIdentityTranscript[0], raw) {
		t.Fatalf("AKA Identity transcript = %x", session.eapIdentityTranscript)
	}
}

func signedSuccessNotification(t *testing.T, identifier byte, kAut []byte) eapaka.Packet {
	t.Helper()
	packet := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: identifier, Type: eapaka.TypeAKA,
		Subtype: eapaka.SubtypeNotification,
		Attributes: []eapaka.Attribute{
			eapaka.NotificationAttribute(eapaka.NotificationSuccess), eapaka.MACAttribute(nil),
		},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal unsigned notification: %v", err)
	}
	mac, err := eapaka.CalculateMAC(kAut, raw, nil)
	if err != nil {
		t.Fatalf("sign notification: %v", err)
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
	want := []byte{
		ikev2.PayloadIDi, ikev2.PayloadSA, ikev2.PayloadTSi,
		ikev2.PayloadTSr, ikev2.PayloadNotify, ikev2.PayloadCP,
	}
	if len(payloads) != len(want) {
		t.Fatalf("payload count = %d, want %d", len(payloads), len(want))
	}
	for index, payload := range payloads {
		if payload.Type() != want[index] {
			t.Fatalf("payload[%d] = %d, want %d", index, payload.Type(), want[index])
		}
	}
	notify := payloads[4].(*ikev2.EncryptedPayloadNotify)
	if notify.NotifyType != ikev2.NotifyTypeEAPOnlyAuthentication || notify.ProtocolID != 0 || notify.SPISize != 0 || len(notify.NotifyData) != 0 {
		t.Fatalf("EAP-only notify = %+v", notify)
	}
}

func TestEAPOnlyResponderAuthenticationRequiresFinalMSKProof(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	session.ikeKeys = testIKEKeys()
	session.prf = enginecrypto.NewPRF(2)
	session.ikeSAInitResponse = []byte("ike-sa-init-response")
	session.Ni = []byte("initiator-nonce")
	session.eapOnlyRequested = true
	initial := []ikev2.Payload{
		&ikev2.EncryptedPayloadID{PayloadType: ikev2.PayloadIDr, IDType: 2, Data: []byte("epdg.example")},
		&ikev2.EncryptedPayloadEAP{Data: []byte{eapaka.CodeRequest, 1, 0, 5, eapTypeIdentity}},
	}
	deferred, err := session.authenticateInitialResponder(initial)
	if err != nil || !deferred || !session.eapOnlyAuthentication {
		t.Fatalf("authenticateInitialResponder deferred=%t err=%v", deferred, err)
	}
	session.eapType = eapaka.TypeAKA
	session.eapKeys = eapaka.Keys{MSK: bytes.Repeat([]byte{0x71}, eapaka.KeyLengthMSK)}
	signed, err := session.responderSignedOctets(session.responderIDType, session.responderID)
	if err != nil {
		t.Fatalf("responderSignedOctets: %v", err)
	}
	sharedKey := session.prf.Compute(session.eapKeys.MSK, []byte(ikev2KeyPad))
	auth := session.prf.Compute(sharedKey, signed)
	final := []ikev2.Payload{&ikev2.EncryptedPayloadAuth{AuthMethod: ikev2.AuthMethodPSK, Data: auth}}
	if err := session.verifyEAPResponderAuth(final); err != nil {
		t.Fatalf("verifyEAPResponderAuth: %v", err)
	}
	final[0].(*ikev2.EncryptedPayloadAuth).Data[0] ^= 0xff
	if err := session.verifyEAPResponderAuth(final); err == nil {
		t.Fatal("accepted invalid final EAP-only AUTH")
	}
}

func hmacSHA1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
