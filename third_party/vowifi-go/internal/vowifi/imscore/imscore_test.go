package imscore

import (
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"
)

// base64Std encodes b as standard base64.
func base64Std(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// stubAKAProvider computes AKA deterministically.
type stubAKAProvider struct{}

func (stubAKAProvider) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	res := bytes.Repeat([]byte{0x33}, 16)
	ck := bytes.Repeat([]byte{0x11}, 16)
	ik := bytes.Repeat([]byte{0x22}, 16)
	return AKAResult{RES: res, CK: ck, IK: ik}, nil
}

func TestParseDigestChallenge(t *testing.T) {
	challenge := `Digest realm="ims.example.com", nonce="MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI=", algorithm=AKAv1-MD5, qop="auth"`
	c, err := ParseDigestChallenge(challenge)
	if err != nil {
		t.Fatalf("ParseDigestChallenge: %v", err)
	}
	if c.Realm != "ims.example.com" {
		t.Errorf("realm = %q", c.Realm)
	}
	if !c.AKA {
		t.Error("AKA should be detected")
	}
	if len(c.RAND) != 16 || len(c.AUTN) != 16 {
		t.Errorf("RAND/AUTN len = %d/%d", len(c.RAND), len(c.AUTN))
	}
}

func TestParseAKANonce(t *testing.T) {
	randBytes := bytes.Repeat([]byte{0x11}, 16)
	autnBytes := bytes.Repeat([]byte{0x22}, 16)
	nonce := base64Std(append(append([]byte{}, randBytes...), autnBytes...))
	r, a, err := ParseAKANonce(nonce)
	if err != nil {
		t.Fatalf("ParseAKANonce: %v", err)
	}
	if !bytes.Equal(r, randBytes) || !bytes.Equal(a, autnBytes) {
		t.Error("RAND/AUTN mismatch")
	}
}

func TestComputeAKAv1MD5DigestResponse(t *testing.T) {
	res, err := ComputeAKAv1MD5DigestResponse(
		"user", "realm", bytes.Repeat([]byte{0x33}, 16),
		"REGISTER", "sip:example.com", "nonce", "00000001", "cnonce", "auth",
	)
	if err != nil {
		t.Fatalf("ComputeAKAv1MD5DigestResponse: %v", err)
	}
	if len(res) != 32 {
		t.Errorf("response len = %d, want 32", len(res))
	}
}

// TestComputeAKAv1MD5DigestResponseVector verifies the Digest-AKA response
// against a hand-computed RFC 3310 vector: response = H(H(A1):nonce:nc:
// cnonce:qop:H(A2)) with A1 containing the raw RES octets.
func TestComputeAKAv1MD5DigestResponseVector(t *testing.T) {
	// RES = 16 bytes of 0x33 -> hex "3333...33".
	res := bytes.Repeat([]byte{0x33}, 16)
	username, realm := "user@example.com", "ims.example.com"
	method, uri := "REGISTER", "sip:ims.example.com"
	nonce, nc, cnonce, qop := "MTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTI=", "00000001", "abcdef0123456789", "auth"

	got, err := ComputeAKAv1MD5DigestResponse(username, realm, res, method, uri, nonce, nc, cnonce, qop)
	if err != nil {
		t.Fatalf("ComputeAKAv1MD5DigestResponse: %v", err)
	}
	const want = "3eb26de281e29349b9af845c57bed302"
	if got != want {
		t.Errorf("response = %s, want RFC 3310 raw-RES vector %s", got, want)
	}
}

func TestProcessAKAChallenge(t *testing.T) {
	randBytes := bytes.Repeat([]byte{0x11}, 16)
	autnBytes := bytes.Repeat([]byte{0x22}, 16)
	nonce := base64Std(append(append([]byte{}, randBytes...), autnBytes...))
	challenge := &DigestChallenge{
		Realm:     "ims.example.com",
		Nonce:     nonce,
		Algorithm: "AKAv1-MD5",
		QOP:       "auth",
		AKA:       true,
		RAND:      randBytes,
		AUTN:      autnBytes,
	}
	auth, err := ProcessAKAChallenge(challenge, stubAKAProvider{}, "user@example.com", "REGISTER", "sip:example.com")
	if err != nil {
		t.Fatalf("ProcessAKAChallenge: %v", err)
	}
	if !strings.HasPrefix(auth, "Digest ") || !strings.Contains(auth, "response=") {
		t.Errorf("auth = %q", auth)
	}
}

func TestProcessAKAChallengeWithoutQOPUsesLegacyDigest(t *testing.T) {
	randBytes := bytes.Repeat([]byte{0x11}, 16)
	autnBytes := bytes.Repeat([]byte{0x22}, 16)
	challenge := &DigestChallenge{
		Realm: "ims.example.com", Nonce: base64Std(append(append([]byte{}, randBytes...), autnBytes...)),
		Algorithm: "AKAv1-MD5", AKA: true, RAND: randBytes, AUTN: autnBytes,
	}
	auth, err := ProcessAKAChallenge(challenge, stubAKAProvider{}, "user@example.com", "REGISTER", "sip:ims.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(auth, "qop=") || strings.Contains(auth, "cnonce=") || strings.Contains(auth, "nc=") {
		t.Fatalf("legacy Digest-AKA unexpectedly included qop fields: %q", auth)
	}
	if !strings.Contains(auth, "algorithm=AKAv1-MD5") || !strings.Contains(auth, "response=\"") {
		t.Fatalf("legacy Digest-AKA is incomplete: %q", auth)
	}
}

func TestNewAndRegister(t *testing.T) {
	cfg := &IMSConfig{
		DeviceID:    "dev-1",
		IMSI:        "310260123456789",
		IMPI:        "310260123456789@ims.example.com",
		Domain:      "ims.example.com",
		LocalIP:     net.IPv4(10, 0, 0, 1),
		Transport:   "udp",
		Expires:     time.Hour,
		AKAProvider: stubAKAProvider{},
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.DeviceID() != "dev-1" {
		t.Errorf("device = %q", svc.DeviceID())
	}
	if svc.IsRegistered() {
		t.Error("should not be registered initially")
	}
	// Register needs a transport sender; wire a fake one.
	svc.transport.SetSendFn(func(req string) error {
		// Simulate a 401 challenge, then a 200.
		if strings.Contains(req, "CSeq: 1 REGISTER") {
			svc.transport.DeliverResponse(registerResponseForRequest(req, 401, map[string]string{
				"WWW-Authenticate": `Digest realm="ims.example.com", nonce="` + base64Std(append(append([]byte{}, bytes.Repeat([]byte{0x11}, 16)...), bytes.Repeat([]byte{0x22}, 16)...)) + `", algorithm=AKAv1-MD5, qop="auth"`,
			}))
		} else {
			svc.transport.DeliverResponse(registerResponseForRequest(req, 200, nil))
		}
		return nil
	})
	if err := svc.Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !svc.IsRegistered() {
		t.Error("should be registered after flow")
	}
	if svc.Status().RegState != regRegistered {
		t.Errorf("reg state = %q", svc.Status().RegState)
	}
}

func TestSendSMSNotRegistered(t *testing.T) {
	cfg := &IMSConfig{IMSI: "310260123456789"}
	svc, _ := New(cfg)
	if _, err := svc.SendSMSWithResult(context.Background(), "+8613800000000", "hi"); err == nil {
		t.Error("send before register should fail")
	}
}

func TestPAccessNetworkInfo(t *testing.T) {
	cfg := &IMSConfig{
		IMSI: "310260123456789",
		IMPI: "310260123456789@ims.example",
	}
	svc, _ := New(cfg)
	want := `IEEE-802.11; i-wlan-node-id="dec378667018"`
	if pani := svc.GetPAccessNetworkInfo(); pani != want {
		t.Errorf("pani = %q, want %q", pani, want)
	}
}

func TestUSSDLifecycle(t *testing.T) {
	cfg := &IMSConfig{IMSI: "310260123456789", DeviceID: "dev-1"}
	svc, _ := New(cfg)
	svc.transport.SetSendFn(func(string) error { return nil })
	res, err := svc.SendUSSD(context.Background(), "*100#")
	if err != nil {
		t.Fatalf("SendUSSD: %v", err)
	}
	if svc.GetActiveUSSDSession() != res.SessionID {
		t.Errorf("active = %q", svc.GetActiveUSSDSession())
	}
	if _, err := svc.ContinueUSSD(context.Background(), res.SessionID, "1"); err != nil {
		t.Fatalf("ContinueUSSD: %v", err)
	}
	if err := svc.CancelUSSD(context.Background(), res.SessionID); err != nil {
		t.Fatalf("CancelUSSD: %v", err)
	}
}

func TestDialogRegistry(t *testing.T) {
	r := newDialogRegistry()
	r.store("call-1", &dialogEntry{handle: &imscoreDialogHandle{callID: "call-1"}})
	if r.len() != 1 {
		t.Errorf("len = %d", r.len())
	}
	if r.load("call-1") == nil {
		t.Error("load failed")
	}
	r.delete("call-1")
	if r.len() != 0 {
		t.Errorf("len after delete = %d", r.len())
	}
}
