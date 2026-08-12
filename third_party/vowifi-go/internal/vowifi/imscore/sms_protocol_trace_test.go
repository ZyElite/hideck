package imscore

import (
	"encoding/hex"
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

func TestRPErrorDiagnosticTrace(t *testing.T) {
	rpdu, err := hex.DecodeString("052b0345dead")
	if err != nil {
		t.Fatal(err)
	}
	length, diagnostic := rpErrorDiagnosticTrace(rpdu)
	if length != 3 || diagnostic != "dead" {
		t.Fatalf("cause length=%d diagnostic=%q", length, diagnostic)
	}
}

func TestRPErrorDiagnosticTraceRejectsTruncatedCauseIE(t *testing.T) {
	length, diagnostic := rpErrorDiagnosticTrace([]byte{0x05, 0x2b, 0x0e, 0x45})
	if length != 14 || diagnostic != "invalid" {
		t.Fatalf("cause length=%d diagnostic=%q", length, diagnostic)
	}
}

func TestRPErrorSubmitReportTrace(t *testing.T) {
	rpdu := []byte{
		0x05, 0x2b, 0x02, 0x45, 0x00,
		0x41, 0x0a, 0x01, 0x90, 0x00, 0x51, 0x50, 0x71, 0x32, 0x20, 0x05, 0x23,
	}
	bytes, ok, fcs := rpErrorSubmitReportTrace(rpdu)
	if bytes != 10 || !ok || fcs != 0x90 {
		t.Fatalf("user data bytes=%d submit report=%v fcs=0x%02x", bytes, ok, fcs)
	}
}

func TestParseInboundSMSProtocolTraceIncludesRPErrorDiagnostics(t *testing.T) {
	body, err := hex.DecodeString("052b024500410a01900051507132200523")
	if err != nil {
		t.Fatal(err)
	}
	raw := "MESSAGE sip:user@example.test SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP pcscf.example.test;branch=z9hG4bK1\r\n" +
		"From: <sip:gateway@example.test>;tag=1\r\n" +
		"To: <sip:user@example.test>\r\nCall-ID: report-1\r\nCSeq: 1 MESSAGE\r\n" +
		"Content-Type: application/vnd.3gpp.sms\r\nContent-Length: 17\r\n\r\n" + string(body)
	trace, err := parseInboundSMSProtocolTrace(raw)
	if err != nil {
		t.Fatal(err)
	}
	if trace.rpKind != "RP-ERROR" || trace.rpType != 5 || trace.rpMR != 43 || trace.rpCause != 69 ||
		trace.causeIEBytes != 2 || trace.causeDiagnostic != "00" ||
		trace.rpUserDataBytes != 10 || !trace.tpSubmitReport || trace.tpFCS != 0x90 {
		t.Fatalf("trace = %+v", trace)
	}
}
