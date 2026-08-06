package ts43

import (
	"strings"
	"testing"
)

func TestBuildPermanentNAIIdentity(t *testing.T) {
	got := BuildPermanentNAIIdentity("310260123456789", "310", "26")
	if got != "310260123456789@nai.epc.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("NAI = %q", got)
	}
}

func TestBuildChallengePayload(t *testing.T) {
	payload, err := BuildChallengePayload("310260123456789", []byte("rand"), []byte("autn"))
	if err != nil {
		t.Fatalf("BuildChallengePayload: %v", err)
	}
	if !strings.Contains(string(payload), "rand") {
		t.Errorf("payload = %s", payload)
	}
}

func TestDeriveKAut(t *testing.T) {
	key := deriveKAut([]byte{1, 2, 3}, []byte{4, 5, 6})
	if len(key) != 16 {
		t.Errorf("kaut len = %d, want 16", len(key))
	}
}

func TestBuildSignedEAPResponse(t *testing.T) {
	out, err := buildSignedEAPResponse([]byte("rand"), []byte("autn"), []byte("res"), []byte{1, 2, 3}, []byte{4, 5, 6})
	if err != nil {
		t.Fatalf("buildSignedEAPResponse: %v", err)
	}
	if !strings.Contains(string(out), "signature") {
		t.Errorf("response = %s", out)
	}
}

func TestBuildAuthAction(t *testing.T) {
	action, err := BuildAuthAction("310260123456789", []byte("rand"), []byte("autn"))
	if err != nil {
		t.Fatalf("BuildAuthAction: %v", err)
	}
	if action["name"] != "eap_aka" {
		t.Errorf("name = %v", action["name"])
	}
}

func TestParseResponse(t *testing.T) {
	data := []byte(`{"entitlements":[{"name":"vowifi","enabled":true}]}`)
	resp, err := ParseResponse(200, data)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if !resp.IsVoWiFiEntitled() {
		t.Error("vowifi should be entitled")
	}
	data = []byte(`{"entitlements":[{"name":"vowifi","enabled":false}]}`)
	resp, _ = ParseResponse(200, data)
	if resp.IsVoWiFiEntitled() {
		t.Error("vowifi should not be entitled")
	}
}
