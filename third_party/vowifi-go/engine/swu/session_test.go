package swu

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"errors"
	"net"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestBuildAlgorithmPlan(t *testing.T) {
	cases := map[string]struct {
		policy string
		encr   uint16
		prf    uint16
	}{
		"strict":        {policy: "strict", encr: 18, prf: 5},
		"legacy_prefer": {policy: "legacy_prefer", encr: 3, prf: 2},
		"prefer":        {policy: "prefer", encr: 12, prf: 2},
		"":              {policy: "", encr: 12, prf: 2},
	}
	for name, tc := range cases {
		plan := buildAlgorithmPlan(tc.policy, nil)
		if plan.IKEEncryption != tc.encr || plan.IKEPRF != tc.prf {
			t.Errorf("%s: plan = encr %d prf %d, want encr %d prf %d",
				name, plan.IKEEncryption, plan.IKEPRF, tc.encr, tc.prf)
		}
	}
	// Explicit config overrides the policy.
	cfg := &Config{IKEEncryption: 7}
	plan := buildAlgorithmPlan("strict", cfg)
	if plan.IKEEncryption != 7 {
		t.Errorf("override: encr = %d, want 7", plan.IKEEncryption)
	}
}

func TestBuildESPProposals(t *testing.T) {
	proposals := buildESPProposals(0, 0)
	if len(proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(proposals))
	}
	encr, integ, err := parseESPProposal(proposals[0])
	if err != nil {
		t.Fatalf("parseESPProposal: %v", err)
	}
	if encr != 12 || integ != 2 {
		t.Errorf("esp = encr %d integ %d, want 12/2", encr, integ)
	}
}

func TestParseIKEProposal(t *testing.T) {
	proposals := buildIKEProposals(12, 2, 2, 14)
	encr, prf, integ, dh, err := parseIKEProposal(proposals[0])
	if err != nil {
		t.Fatalf("parseIKEProposal: %v", err)
	}
	if encr != 12 || prf != 2 || integ != 2 || dh != 14 {
		t.Errorf("ike = %d/%d/%d/%d, want 12/2/2/14", encr, prf, integ, dh)
	}
}

func TestPrioritizeDHGroup(t *testing.T) {
	got := prioritizeDHGroup([]uint16{2, 14, 19}, 14)
	if got[0] != 14 {
		t.Errorf("first = %d, want 14", got[0])
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestSpoofAppleIMEI(t *testing.T) {
	// 14-digit prefix; the check digit must make the IMEI Luhn-valid.
	imei := spoofAppleIMEI("35693803564380")
	if len(imei) != 15 {
		t.Fatalf("imei len = %d, want 15", len(imei))
	}
	sum := 0
	for i := 0; i < 15; i++ {
		d := int(imei[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	if sum%10 != 0 {
		t.Errorf("imei %s fails Luhn check", imei)
	}
}

func TestBuildNAIWithOverride(t *testing.T) {
	got := buildNAI("310260123456789", "310", "26")
	if got != "310260123456789@nai.epc.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("NAI = %q", got)
	}
}

func TestVerifyEAPAKAMAC(t *testing.T) {
	// Build a challenge, compute the MAC, and verify it.
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	randAttr := bytes.Repeat([]byte{0x33}, 16)
	autnAttr := bytes.Repeat([]byte{0x44}, 16)

	// AT_RAND + AT_AUTN attributes, each padded to a multiple of 4 bytes.
	body := make([]byte, 0, 60)
	body = append(body, eap.AttrATRAND, 5)
	body = append(body, randAttr...)
	body = append(body, 0, 0) // pad to 20 bytes
	body = append(body, eap.AttrATAUTN, 5)
	body = append(body, autnAttr...)
	body = append(body, 0, 0) // pad to 20 bytes
	// AT_MAC placeholder (zeroed), 20 bytes total.
	body = append(body, eap.AttrATMAC, 5)
	body = append(body, make([]byte, 18)...)

	pkt := &eap.EAPPacket{
		Code:       eap.CodeRequest,
		Identifier: 1,
		Type:       eap.TypeAKA,
		SubType:    eap.SubtypeAKAChallenge,
		Data:       body,
	}

	// Compute the MAC over the packet with AT_MAC zeroed.
	raw := pkt.Encode()
	if len(raw) >= 20 {
		for i := len(raw) - 20; i < len(raw); i++ {
			raw[i] = 0
		}
	}
	key := append(append([]byte{}, ck...), ik...)
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte("EAP-AKA"))
	mac.Write(raw)
	// Place the 16-byte MAC into the AT_MAC value (first 16 of the 18-byte value).
	copy(body[len(body)-18:len(body)-2], mac.Sum(nil)[:16])

	attrs, err := eap.ParseAttributes(pkt.Data, 0)
	if err != nil {
		t.Fatalf("ParseAttributes: %v", err)
	}
	if err := verifyEAPAKAMAC(pkt, attrs, ck, ik, eap.TypeAKA); err != nil {
		t.Errorf("verifyEAPAKAMAC: %v", err)
	}
	// Wrong key must fail.
	if err := verifyEAPAKAMAC(pkt, attrs, bytes.Repeat([]byte{0x99}, 16), ik, eap.TypeAKA); err == nil {
		t.Error("verifyEAPAKAMAC with wrong CK should fail")
	}
}

func TestBuildSignedEAPResponse(t *testing.T) {
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	aka := AKAResult{RES: bytes.Repeat([]byte{0x55}, 8), CK: ck, IK: ik}
	req := &eap.EAPPacket{
		Code:       eap.CodeRequest,
		Identifier: 2,
		Type:       eap.TypeAKA,
		SubType:    eap.SubtypeAKAChallenge,
		Data:       []byte{},
	}
	resp, err := buildSignedEAPResponse(req, nil, aka, eap.TypeAKA)
	if err != nil {
		t.Fatalf("buildSignedEAPResponse: %v", err)
	}
	if resp.Code != eap.CodeResponse || resp.Identifier != 2 {
		t.Errorf("resp = code %d id %d", resp.Code, resp.Identifier)
	}
	if len(resp.Data) < 4 {
		t.Error("response too short")
	}
}

func TestExtractDstTuple(t *testing.T) {
	// IPv4 packet: version 4, dst at bytes 16-20.
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	copy(pkt[16:20], net.IPv4(10, 0, 0, 1).To4())
	dst, _, err := extractDstTuple(pkt)
	if err != nil {
		t.Fatalf("extractDstTuple: %v", err)
	}
	if !dst.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("dst = %v", dst)
	}
}

func TestMatchSelectors(t *testing.T) {
	tsr := &ikev2.EncryptedPayloadTS{Selectors: []*ikev2.TrafficSelector{
		ikev2.NewTrafficSelectorIPV4(net.IPv4zero, 0, 0, 0xffff),
	}}
	pkt := make([]byte, 20)
	pkt[0] = 0x45
	if !matchSelectors(pkt, nil, tsr) {
		t.Error("IPv4 packet should match IPv4 selector")
	}
}

func TestSessionStateTransitions(t *testing.T) {
	s := NewSession(&Config{IMSI: "310260123456789"})
	if s.State() != stateIdle {
		t.Errorf("initial state = %q", s.State())
	}
	s.setState(stateConnecting)
	if s.State() != stateConnecting {
		t.Errorf("state = %q", s.State())
	}
	s.setTerminalError(errors.New("boom"))
	if s.State() != stateError {
		t.Errorf("state = %q", s.State())
	}
	if s.terminalError() == nil {
		t.Error("terminal error not recorded")
	}
	s.Shutdown()
	select {
	case <-s.done:
	default:
		t.Error("done channel not closed after Shutdown")
	}
}

func TestSessionManager(t *testing.T) {
	m := NewSessionManager()
	s := NewSession(&Config{IMSI: "310260123456789"})
	m.Start("dev-1", s)
	if m.Get("dev-1") != s {
		t.Error("Get did not return the session")
	}
	m.Stop("dev-1")
	if m.Get("dev-1") != nil {
		t.Error("Get after Stop should be nil")
	}
}

func TestFragmentMessage(t *testing.T) {
	s := NewSession(&Config{})
	raw := bytes.Repeat([]byte{0xaa}, 3000)
	if !s.shouldFragment(raw) {
		t.Error("3000-byte message should fragment")
	}
	parts, err := s.fragmentMessage(raw)
	if err != nil {
		t.Fatalf("fragmentMessage: %v", err)
	}
	var joined []byte
	for _, p := range parts {
		joined = append(joined, p...)
	}
	if !bytes.Equal(joined, raw) {
		t.Error("fragments do not reassemble")
	}
}
