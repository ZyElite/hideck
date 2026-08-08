package logging

import (
	"strings"
	"testing"
)

func TestRedactSIPRaw(t *testing.T) {
	raw := "INVITE sip:1234567890@example.com SIP/2.0\r\n" +
		"Authorization: Digest username=\"310260123456789\"\r\n" +
		"Proxy-Authorization: Digest response=\"12345678\"\r\n"
	want := "INVITE sip:123*****90@example.com SIP/2.0\r\n" +
		"Authorization: [REDACTED]\r\n" +
		"Proxy-Authorization: [REDACTED]\r\n"
	if got := RedactSIPRaw(raw); got != want {
		t.Fatalf("RedactSIPRaw() = %q, want %q", got, want)
	}
}

func TestRedactSIPRawPreservesHeaderNameAndMasksNonSensitiveLines(t *testing.T) {
	raw := "authorization: secret\nX-Identity: 12345678"
	got := RedactSIPRaw(raw)
	if got != "authorization: [REDACTED]\r\nX-Identity: 123***78" {
		t.Fatalf("RedactSIPRaw() = %q", got)
	}
}

func TestRedactSMSContent(t *testing.T) {
	t.Setenv(smsLogContentEnvironment, "")
	for content, want := range map[string]string{
		"":          "[REDACTED len=0]",
		"   ":       "[REDACTED len=0]",
		" message ": "[REDACTED len=7]",
		" 你好世界 ":    "[REDACTED len=4]",
	} {
		if got := RedactSMSContent(content); got != want {
			t.Errorf("RedactSMSContent(%q) = %q, want %q", content, got, want)
		}
	}
}

func TestRedactSMSContentExplicitlyEnabled(t *testing.T) {
	t.Setenv(smsLogContentEnvironment, "yes")
	const content = "  private message  "
	if got := RedactSMSContent(content); got != content {
		t.Fatalf("enabled content = %q, want original", got)
	}
}

func TestMaskLongDigits(t *testing.T) {
	got := longDigitPattern.ReplaceAllStringFunc("imsi 310260123456789 pin 1234567", maskLongDigits)
	if strings.Contains(got, "310260123456789") {
		t.Fatalf("long digits not masked: %q", got)
	}
	if got != "imsi 310**********89 pin 1234567" {
		t.Fatalf("masked digits = %q", got)
	}
	if got := maskLongDigits("123456"); got != "******" {
		t.Fatalf("short direct mask = %q", got)
	}
	if got := maskLongDigits("1234567"); got != "123**67" {
		t.Fatalf("seven-digit direct mask = %q", got)
	}
}

func TestEnvEnabledValues(t *testing.T) {
	for _, value := range []string{"1", "true", " TRUE ", "yes", "On"} {
		t.Setenv("LOGGING_TEST_ENABLED", value)
		if !envEnabled("LOGGING_TEST_ENABLED") {
			t.Errorf("envEnabled(%q) = false", value)
		}
	}
	t.Setenv("LOGGING_TEST_ENABLED", "t")
	if envEnabled("LOGGING_TEST_ENABLED") {
		t.Fatal("unsupported truthy value was accepted")
	}
}
