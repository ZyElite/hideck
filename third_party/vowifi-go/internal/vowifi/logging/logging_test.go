package logging

import (
	"strings"
	"testing"
)

func TestRedactSIPRaw(t *testing.T) {
	raw := "INVITE sip:1234567890@example.com SIP/2.0\r\nAuthorization: Digest username=\"310260123456789\"\r\n"
	got := RedactSIPRaw(raw)
	if strings.Contains(got, "310260123456789") {
		t.Errorf("IMSI not redacted: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("authorization not redacted: %q", got)
	}
}

func TestRedactSMSContent(t *testing.T) {
	if got := RedactSMSContent("1234567890"); got == "1234567890" {
		t.Error("content not redacted")
	}
	if got := RedactSMSContent("short"); got != "****" {
		t.Errorf("short content = %q", got)
	}
}

func TestMaskLongDigits(t *testing.T) {
	got := maskLongDigits("imsi 310260123456789 end")
	if strings.Contains(got, "310260123456789") {
		t.Errorf("long digits not masked: %q", got)
	}
}

func TestDedupeNonEmpty(t *testing.T) {
	got := dedupeNonEmpty([]string{"a", "", "b", "a", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupe = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupe[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestShouldEmitRateLimited(t *testing.T) {
	if !shouldEmitRateLimited("k1") {
		t.Error("first emission should be allowed")
	}
	if shouldEmitRateLimited("k1") {
		t.Error("second emission within window should be suppressed")
	}
	if !shouldEmitRateLimited("k2") {
		t.Error("different key should be allowed")
	}
}
