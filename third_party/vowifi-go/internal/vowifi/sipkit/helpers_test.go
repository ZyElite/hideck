package sipkit

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestDebugHelpers(t *testing.T) {
	raw := strings.Repeat("A", 220) + "\r\nignored"
	if got := FirstLine(raw); len(got) != maxFirstLineLength {
		t.Fatalf("FirstLine length = %d", len(got))
	}
	body := []byte("  one\r\n two\tthree  ")
	if got := SanitizeBody(body); got != "one two three" {
		t.Fatalf("SanitizeBody = %q", got)
	}
	if got := DebugText("abcdef", 3); got != "abc" {
		t.Fatalf("DebugText = %q", got)
	}
}

func TestFirstHeaderValueTrimControl(t *testing.T) {
	response := sip.NewResponse(200, "OK")
	response.AppendHeader(sip.NewHeader("X-Test", "  value  "))
	if got := FirstHeaderValue(response, "X-Test", false); got != "  value  " {
		t.Fatalf("untrimmed value = %q", got)
	}
	if got := FirstHeaderValue(response, "X-Test", true); got != "value" {
		t.Fatalf("trimmed value = %q", got)
	}
	if got := HeaderValue(nil, true); got != "" {
		t.Fatalf("nil HeaderValue = %q", got)
	}
}

func TestCSVAndHeaderPolicy(t *testing.T) {
	if !containsTokenFold(splitCSV("a, B, c"), "b") {
		t.Fatal("case-folded token not found")
	}
	if got := ensureCSVToken(" a , B ", "b"); got != "a, B" {
		t.Fatalf("ensure existing = %q", got)
	}
	if got := removeCSVTokenFold("a, b, c", "B"); got != "a, c" {
		t.Fatalf("remove = %q", got)
	}
	include, allow, pani, ppi := ResolveSIPHeaderPolicy(
		sip.INVITE, RequestKindOutOfDialog, "ipsec3gpp", "INVITE, sec-agree",
	)
	if !include || allow != "INVITE, sec-agree" || !pani || !ppi {
		t.Fatalf("policy = %v %q %v %v", include, allow, pani, ppi)
	}
	include, allow, _, _ = ResolveSIPHeaderPolicy(sip.INVITE, RequestKindInDialog, "none", "INVITE")
	if include || allow != "" {
		t.Fatalf("in-dialog Allow = %v %q", include, allow)
	}
}

func TestAutomaticHeaderMethodRules(t *testing.T) {
	if requiresPANI(sip.ACK, "pani") || requiresPANI(sip.CANCEL, "pani") {
		t.Fatal("ACK/CANCEL must omit PANI")
	}
	if !requiresPANI(sip.BYE, "pani") || requiresPPI(sip.REGISTER, "<sip:a@b>") {
		t.Fatal("PANI/PPI method policy mismatch")
	}
	if !requiresSecurityClient(sip.REGISTER, "ipsec") || requiresSecurityClient(sip.INVITE, "ipsec") {
		t.Fatal("Security-Client method policy mismatch")
	}
	if requiresSecurityVerify(sip.CANCEL, "ipsec") || !requiresSecurityVerify(sip.BYE, "ipsec") {
		t.Fatal("Security-Verify method policy mismatch")
	}
}
