package swu

import (
	"bytes"
	"strings"
	"testing"
)

func TestNegotiationError(t *testing.T) {
	e := &NegotiationError{Context: "IKE_SA_INIT", Reason: "no proposal chosen"}
	if got := e.Error(); got != "IKE_SA_INIT: no proposal chosen" {
		t.Errorf("Error() = %q", got)
	}
	var nilE *NegotiationError
	if nilE.Error() != "" {
		t.Error("nil receiver should return empty string")
	}
}

func TestRedirectError(t *testing.T) {
	e := &RedirectError{Target: "epdg.example.com:4500"}
	if !strings.Contains(e.Error(), "epdg.example.com:4500") {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestErrInvalidKEGroup(t *testing.T) {
	e := &ErrInvalidKEGroup{Group: 21}
	if got := e.Error(); !strings.Contains(got, "21") {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestCreateChildSARejectError(t *testing.T) {
	e := &createChildSARejectError{NotifyType: 16388} // NAT_DETECTION_SOURCE_IP
	got := e.Error()
	if !strings.Contains(got, "16388") {
		t.Errorf("Error() = %q", got)
	}
}

func TestNormalizeAlgorithmPolicy(t *testing.T) {
	cases := map[string]string{
		"strict":        "strict",
		"STRICT":        "strict",
		"  strict  ":    "strict",
		"legacy_prefer": "legacy_prefer",
		"LEGACY_PREFER": "legacy_prefer",
		"anything-else": "balanced",
		"":              "balanced",
	}
	for in, want := range cases {
		if got := normalizeAlgorithmPolicy(in); got != want {
			t.Errorf("normalizeAlgorithmPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLegacyName(t *testing.T) {
	cases := map[string]string{
		"3des":       "3des",
		"3DES":       "3des",
		"3-des":      "3des",
		"3 des":      "3 des",
		"triple-des": "3des",
		"TRIPLEDES":  "3des",
		"aes-cbc":    "aescbc",
	}
	for in, want := range cases {
		if got := normalizeLegacyName(in); got != want {
			t.Errorf("normalizeLegacyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildNAI(t *testing.T) {
	// IMSI 310260123456789: MCC=310, MNC=26 (2 digits, padded to 026).
	got := buildNAI("310260123456789", &Config{})
	want := "0310260123456789@nai.epc.mnc026.mcc310.3gppnetwork.org"
	if got != want {
		t.Errorf("buildNAI = %q, want %q", got, want)
	}
	// Override MCC/MNC.
	got = buildNAI("310260123456789", &Config{MCC: "999", MNC: "88"})
	if !strings.Contains(got, "mnc088.mcc999") {
		t.Errorf("override: %q", got)
	}
	// 3-digit MNC override is not padded.
	got = buildNAI("310260123456789", &Config{MCC: "310", MNC: "260"})
	if !strings.Contains(got, "mnc260.mcc310") {
		t.Errorf("3-digit mnc: %q", got)
	}
	// Too-short IMSI is rejected.
	if got := buildNAI("310", &Config{}); got != "0310@nai.epc.mnc.mcc.3gppnetwork.org" {
		t.Errorf("short IMSI = %q", got)
	}
}

func TestFragmentBufferReassemble(t *testing.T) {
	fb := newFragmentBuffer()
	msgID := uint32(7)
	total := uint16(3)
	parts := [][]byte{
		bytes.Repeat([]byte{0x01}, 10),
		bytes.Repeat([]byte{0x02}, 20),
		bytes.Repeat([]byte{0x03}, 30),
	}
	for i, p := range parts {
		complete, err := fb.addFragment(msgID, uint16(i+1), total, p)
		if err != nil {
			t.Fatalf("addFragment %d: %v", i, err)
		}
		if i < len(parts)-1 && complete {
			t.Errorf("fragment %d reported complete", i)
		}
	}
	complete, err := fb.addFragment(msgID, 1, total, parts[0])
	if err != nil {
		t.Fatalf("dup addFragment: %v", err)
	}
	if complete {
		t.Error("duplicate fragment should not complete the message")
	}

	out, err := fb.reassemble(msgID)
	if err != nil {
		t.Fatalf("reassemble: %v", err)
	}
	want := append(append([]byte{}, parts[0]...), parts[1]...)
	want = append(want, parts[2]...)
	if !bytes.Equal(out, want) {
		t.Errorf("reassembled = %x, want %x", out, want)
	}
	// The set is consumed.
	if _, err := fb.reassemble(msgID); err == nil {
		t.Error("second reassemble should fail")
	}
}

func TestFragmentBufferMismatch(t *testing.T) {
	fb := newFragmentBuffer()
	if _, err := fb.addFragment(1, 1, 3, []byte("a")); err != nil {
		t.Fatalf("first: %v", err)
	}
	// A fragment declaring fewer total than recorded must error.
	if _, err := fb.addFragment(1, 2, 2, []byte("b")); err == nil {
		t.Error("smaller total should error")
	}
	// A fragment declaring more total resets the set.
	if _, err := fb.addFragment(1, 1, 4, []byte("a")); err != nil {
		t.Fatalf("larger total reset: %v", err)
	}
}

func TestFragmentBufferIncompleteReassemble(t *testing.T) {
	fb := newFragmentBuffer()
	fb.addFragment(1, 1, 3, []byte("a"))
	fb.addFragment(1, 2, 3, []byte("b"))
	// Fragment 3 missing.
	if _, err := fb.reassemble(1); err == nil {
		t.Error("reassemble of incomplete message should fail")
	}
}

func TestFragmentBufferOversize(t *testing.T) {
	fb := newFragmentBuffer()
	// A single fragment larger than the cap is rejected.
	big := make([]byte, maxFragmentedMessageSize+1)
	if _, err := fb.addFragment(1, 1, 1, big); err == nil {
		t.Error("oversize fragment should be rejected")
	}
}
