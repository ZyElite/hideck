package swu

import (
	"bytes"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func TestNormalizeAKAChallengeModeMatchesLegacyAliases(t *testing.T) {
	tests := map[string]string{
		"": "minimal", "minimal": "minimal",
		"off": "off", "NONE": "off", "omit": "off", "no_checkcode": "off",
		"echo": "checkcode", "checkcode": "checkcode",
		"recalc": "recompute", " ReCompute ": "recompute",
		"custom": "custom",
	}
	for input, want := range tests {
		if got := normalizeAKAChallengeMode(input); got != want {
			t.Errorf("normalizeAKAChallengeMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLegacyIdentitySelectionAndKeyDerivationFallback(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376", FastReauthID: "fast@example"})
	if got := session.currentIKEIdentity(); got != "fast@example" {
		t.Fatalf("IKE identity = %q", got)
	}
	if got := session.currentEAPIdentity(); got != "fast@example" {
		t.Fatalf("EAP identity = %q", got)
	}
	session.eapIdentity, session.eapIdentitySet = "", true
	if got := session.currentEAPIdentity(); got != "" {
		t.Fatalf("explicit empty EAP identity = %q", got)
	}
	if got := session.currentEAPIdentityForKeyDerivation(); got != buildNAI(session.cfg.IMSI, session.cfg) {
		t.Fatalf("key derivation identity = %q", got)
	}
}

func TestBuildCPRequestPayloadHonorsIPStack(t *testing.T) {
	tests := []struct {
		mode string
		want []uint16
	}{
		{"ipv4", []uint16{1, 3, 20}},
		{"ipv6", []uint16{8, 10, 21, 16390}},
		{"", []uint16{1, 3, 20, 8, 10, 21, 16390}},
	}
	for _, test := range tests {
		session := NewSession(&Config{IPStackType: test.mode})
		payload := session.buildCPRequestPayload()
		if len(payload.Attributes) != len(test.want) {
			t.Fatalf("IPStack %q attributes = %d", test.mode, len(payload.Attributes))
		}
		for index, want := range test.want {
			attribute := payload.Attributes[index]
			if attribute.Type != want {
				t.Errorf("IPStack %q attribute[%d] = %d, want %d", test.mode, index, attribute.Type, want)
			}
			if want == ikev2.CPAttrIP6Address && (len(attribute.Value) != 17 || attribute.Value[16] != 64) {
				t.Errorf("IPv6 request = %x", attribute.Value)
			}
		}
	}
}

func TestInitialIKEAuthRestoresNotifyOrderAndDeviceIdentity(t *testing.T) {
	session := NewSession(&Config{
		IMSI: "234102356143376", APN: "ims", DeviceIdentityIMEI: "358983361433761",
	})
	payloads, err := session.buildIKEAuthInitPayloads()
	if err != nil {
		t.Fatalf("buildIKEAuthInitPayloads: %v", err)
	}
	wantNotify := []uint16{
		ikev2.EAP_ONLY_AUTHENTICATION, ikev2.MOBIKE_SUPPORTED,
		ikev2.TICKET_REQUEST, ikev2.INITIAL_CONTACT,
		ikev2.DEVICE_IDENTITY_3GPP, ikev2.DEVICE_IDENTITY,
	}
	var notifications []*ikev2.EncryptedPayloadNotify
	for _, payload := range payloads {
		if notification, ok := payload.(*ikev2.EncryptedPayloadNotify); ok {
			notifications = append(notifications, notification)
		}
	}
	if len(notifications) != len(wantNotify) {
		t.Fatalf("notifications = %d, want %d", len(notifications), len(wantNotify))
	}
	for index, want := range wantNotify {
		if notifications[index].NotifyType != want {
			t.Errorf("notification[%d] = %d, want %d", index, notifications[index].NotifyType, want)
		}
	}
	if !bytes.Equal(notifications[4].NotifyData, notifications[5].NotifyData) || len(notifications[4].NotifyData) != 10 {
		t.Fatalf("device identity notify data = %x / %x", notifications[4].NotifyData, notifications[5].NotifyData)
	}
}

func TestSpoofAppleIMEIRestoresFixedTACAndLuhn(t *testing.T) {
	if got := spoofAppleIMEI("234102356143376"); got != "358983361433761" {
		t.Fatalf("spoofAppleIMEI = %q", got)
	}
	if got := spoofAppleIMEI("short"); got != "358983361234565" {
		t.Fatalf("short IMSI fallback = %q", got)
	}
}

func TestHandleEAPReturnsPayloadWithoutSending(t *testing.T) {
	transport := newTestIKETransport()
	session := NewSession(&Config{IMSI: "234102356143376"})
	session.socket = transport
	request := []byte{eapaka.CodeRequest, 7, 0, 5, eapTypeIdentity}
	payloads, err := session.handleEAP(request)
	if err != nil {
		t.Fatalf("handleEAP: %v", err)
	}
	if transport.sendCount.Load() != 0 {
		t.Fatal("handleEAP sent on the transport")
	}
	if len(payloads) != 1 || payloads[0].Type() != ikev2.PayloadEAP {
		t.Fatalf("response payloads = %#v", payloads)
	}
	response, err := eapaka.ParsePacket(payloads[0].(*ikev2.EncryptedPayloadEAP).EAPMessage)
	if err != nil {
		t.Fatalf("parse EAP response: %v", err)
	}
	if response.Code != eapaka.CodeResponse || response.Identifier != 7 || string(response.Data) != buildNAI(session.cfg.IMSI, session.cfg) {
		t.Fatalf("EAP identity response = %#v", response)
	}
}

func TestAKAIdentityWithoutIDRequestReturnsNoIdentityAttribute(t *testing.T) {
	session := NewSession(&Config{IMSI: "234102356143376"})
	request := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: 9, Type: eapaka.TypeAKA,
		Subtype: eapaka.SubtypeIdentity,
	}
	raw, err := request.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	payloads, err := session.handleEAP(raw)
	if err != nil {
		t.Fatalf("handleEAP: %v", err)
	}
	response, err := eapaka.ParsePacket(payloads[0].(*ikev2.EncryptedPayloadEAP).EAPMessage)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(response.Attributes) != 0 || !session.eapIdentitySet || session.eapIdentity != "" {
		t.Fatalf("identity response = %#v, stored=%q/%t", response.Attributes, session.eapIdentity, session.eapIdentitySet)
	}
}

func TestDisableEAPMACValidationIsExplicitAndScoped(t *testing.T) {
	result := testAKAResult()
	strict := NewSession(&Config{
		IMSI: "234102356143376", AKAProvider: &recordingAKAProvider{result: result},
	})
	challenge := signedAKAChallenge(
		t, strict.currentEAPIdentity(), bytes.Repeat([]byte{0x31}, 16),
		bytes.Repeat([]byte{0x42}, 16), result,
	)
	challenge.Attributes[len(challenge.Attributes)-1].Data[2] ^= 0xff
	if _, err := strict.handleRFCChallenge(challenge); err == nil {
		t.Fatal("strict session accepted an invalid challenge MAC")
	}

	diagnostic := NewSession(&Config{
		IMSI: "234102356143376", SIM: &recordingAKAProvider{result: result},
		DisableEAPMACValidation: true,
	})
	if payloads, err := diagnostic.handleRFCChallenge(challenge); err != nil || len(payloads) != 1 {
		t.Fatalf("explicit diagnostic mode payloads=%d err=%v", len(payloads), err)
	}
}

func testAKAResult() AKAResult {
	return AKAResult{
		RES: bytes.Repeat([]byte{0x53}, 8),
		CK:  bytes.Repeat([]byte{0x14}, 16),
		IK:  bytes.Repeat([]byte{0x25}, 16),
	}
}
