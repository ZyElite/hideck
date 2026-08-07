package swu

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"net"
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
	session := NewSession(&Config{IMSI: "234102356143376", APN: "ims"})
	payloads, err := session.buildIKEAuthInitPayloads()
	if err != nil {
		t.Fatalf("buildIKEAuthInitPayloads: %v", err)
	}
	want := []byte{
		ikev2.PayloadIDi, ikev2.PayloadIDr, ikev2.PayloadSA, ikev2.PayloadTSi,
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
	idr := payloads[1].(*ikev2.EncryptedPayloadID)
	if idr.IDType != ikev2.IDTypeFQDN || string(idr.Data) != "ims" {
		t.Fatalf("APN IDr = %+v", idr)
	}
	notify := payloads[5].(*ikev2.EncryptedPayloadNotify)
	if notify.NotifyType != ikev2.NotifyTypeEAPOnlyAuthentication || notify.ProtocolID != 0 || notify.SPISize != 0 || len(notify.NotifyData) != 0 {
		t.Fatalf("EAP-only notify = %+v", notify)
	}
	cp := payloads[6].(*ikev2.EncryptedPayloadCP)
	wantCPTypes := []uint16{
		ikev2.CPAttrIP4Address, ikev2.CPAttrIP4DNS, ikev2.CPAttrPCSCFIP4,
		ikev2.CPAttrIP6Address, ikev2.CPAttrIP6DNS, ikev2.CPAttrPCSCFIP6,
	}
	if len(cp.Attrs) != len(wantCPTypes) {
		t.Fatalf("initial CP request address families: %+v", cp.Attrs)
	}
	for index, attribute := range cp.Attrs {
		if attribute.Type != wantCPTypes[index] || len(attribute.Value) != 0 {
			t.Fatalf("initial CP attribute[%d] = %+v, want type %d", index, attribute, wantCPTypes[index])
		}
	}
}

func TestInitialEAPIKEAuthOmitsIDrForDefaultAPN(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	payloads, err := session.buildIKEAuthInitPayloads()
	if err != nil {
		t.Fatalf("buildIKEAuthInitPayloads: %v", err)
	}
	for _, payload := range payloads {
		if payload.Type() == ikev2.PayloadIDr {
			t.Fatal("default APN request unexpectedly included IDr")
		}
	}
}

func TestEAPOnlyResponderAuthenticationRequiresFinalMSKProof(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376", APN: "ims", EPDGAddr: "epdg.example"})
	session.ikeKeys = testIKEKeys()
	session.prf = enginecrypto.NewPRF(2)
	session.ikeSAInitResponse = []byte("ike-sa-init-response")
	session.Ni = []byte("initiator-nonce")
	session.eapOnlyRequested = true
	initial := []ikev2.Payload{
		&ikev2.EncryptedPayloadEAP{Data: []byte{eapaka.CodeRequest, 1, 0, 5, eapTypeIdentity}},
	}
	deferred, err := session.authenticateInitialResponder(initial)
	if err != nil || !deferred || !session.eapOnlyAuthentication {
		t.Fatalf("authenticateInitialResponder deferred=%t err=%v", deferred, err)
	}
	if session.responderIDType != ikev2.IDTypeFQDN || string(session.responderID) != "epdg.example" {
		t.Fatalf("effective responder ID = type %d data %q", session.responderIDType, session.responderID)
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

func TestEAPOnlyResponderCannotOmitUnconfiguredIdentity(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	session.eapOnlyRequested = true
	payloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadEAP{Data: []byte{eapaka.CodeRequest, 1, 0, 5, eapTypeIdentity}},
	}
	if _, err := session.authenticateInitialResponder(payloads); err == nil {
		t.Fatal("accepted an omitted IDr without a configured ePDG identity")
	}
}

func TestConfiguredEPDGIdentity(t *testing.T) {
	tests := []struct {
		address string
		idType  byte
		want    []byte
	}{
		{address: "EPDG.Example.", idType: ikev2.IDTypeFQDN, want: []byte("epdg.example")},
		{address: "epdg.example:4500", idType: ikev2.IDTypeFQDN, want: []byte("epdg.example")},
		{address: "192.0.2.10:500", idType: ikev2.IDTypeIPv4Address, want: net.IPv4(192, 0, 2, 10).To4()},
		{address: "[2001:db8::1]:500", idType: ikev2.IDTypeIPv6Address, want: net.ParseIP("2001:db8::1").To16()},
	}
	for _, test := range tests {
		gotType, got, ok := configuredEPDGIdentity(test.address)
		if !ok || gotType != test.idType || !bytes.Equal(got, test.want) {
			t.Errorf("configuredEPDGIdentity(%q) = (%d, %x, %t), want (%d, %x, true)", test.address, gotType, got, ok, test.idType, test.want)
		}
	}
}

func hmacSHA1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
