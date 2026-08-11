package imscore

import (
	"strings"
	"testing"
)

func TestSMSProtocolTraceEnabledForSelectedDevice(t *testing.T) {
	t.Setenv(smsProtocolTraceDeviceEnv, "wwan0")
	selected := &Service{cfg: &IMSConfig{DeviceID: "wwan0"}}
	other := &Service{cfg: &IMSConfig{DeviceID: "wwan1"}}
	if !selected.smsProtocolTraceEnabled() {
		t.Fatal("selected device trace is disabled")
	}
	if other.smsProtocolTraceEnabled() {
		t.Fatal("unselected device trace is enabled")
	}
}

func TestSMSTraceHeaderDomainDoesNotExposeUser(t *testing.T) {
	header := `"Subscriber" <sip:447840000000@ims.example.test>;tag=secret`
	domain := smsTraceHeaderDomain(header)
	if domain != "ims.example.test" {
		t.Fatalf("domain = %q", domain)
	}
	if strings.Contains(domain, "447840000000") || strings.Contains(domain, "secret") {
		t.Fatalf("domain exposes identity: %q", domain)
	}
}

func TestSMSTraceTokenIsDeterministicAndRedacted(t *testing.T) {
	const value = "sensitive-call-id@example.test"
	first, second := smsTraceToken(value), smsTraceToken(value)
	if first == "" || first != second {
		t.Fatalf("unexpected trace tokens: %q %q", first, second)
	}
	if strings.Contains(first, value) || len(first) != 16 {
		t.Fatalf("trace token is not redacted: %q", first)
	}
}
