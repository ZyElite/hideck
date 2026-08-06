package sipkit

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
)

func TestParseURI(t *testing.T) {
	uri, err := ParseURI("sip:user@example.com")
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if uri.User != "user" || uri.Host != "example.com" {
		t.Errorf("uri = %+v", uri)
	}
}

func TestParseAORWithDefaultHost(t *testing.T) {
	uri, err := ParseAORWithDefaultHost("sip:user@", "example.com")
	if err != nil {
		t.Fatalf("ParseAOR: %v", err)
	}
	if uri.Host != "example.com" {
		t.Errorf("host = %q", uri.Host)
	}
}

func TestExtractURIFromHeaderValue(t *testing.T) {
	uri, err := ExtractURIFromHeaderValue("<sip:user@example.com>;tag=abc")
	if err != nil {
		t.Fatalf("ExtractURI: %v", err)
	}
	if uri.Host != "example.com" {
		t.Errorf("host = %q", uri.Host)
	}
}

func TestParseHostPortWithDefault(t *testing.T) {
	host, port, err := ParseHostPortWithDefault("example.com:5060", 5060)
	if err != nil || host != "example.com" || port != 5060 {
		t.Errorf("parse = %q %d %v", host, port, err)
	}
	host, port, err = ParseHostPortWithDefault("example.com", 5060)
	if err != nil || port != 5060 {
		t.Errorf("parse default = %q %d %v", host, port, err)
	}
}

func TestNormalizeHost(t *testing.T) {
	if got := NormalizeHost("2001:DB8::1"); got != "[2001:db8::1]" {
		t.Errorf("IPv6 = %q", got)
	}
	if got := NormalizeHost("EXAMPLE.com"); got != "example.com" {
		t.Errorf("host = %q", got)
	}
}

func TestHasURIScheme(t *testing.T) {
	if !hasURIScheme("sip:user@example.com") {
		t.Error("sip: should be detected")
	}
	if hasURIScheme("user@example.com") {
		t.Error("bare AOR should not have a scheme")
	}
}

func TestCSVHelpers(t *testing.T) {
	if !containsTokenFold("a, b, c", "B") {
		t.Error("token B should be found")
	}
	if got := removeCSVTokenFold("a, b, c", "b"); got != "a, c" {
		t.Errorf("remove = %q", got)
	}
	if got := ensureCSVToken("a", "b"); got != "a, b" {
		t.Errorf("ensure = %q", got)
	}
	if got := ensureCSVToken("a", "a"); got != "a" {
		t.Errorf("ensure existing = %q", got)
	}
}

func TestHeaderPolicy(t *testing.T) {
	if !requiresPANI("REGISTER") {
		t.Error("REGISTER should require PANI")
	}
	if requiresPANI("ACK") {
		t.Error("ACK should not require PANI")
	}
	if !requiresPPI("INVITE") {
		t.Error("INVITE should require PPI")
	}
	if !requiresSecurityClient("REGISTER") {
		t.Error("REGISTER should require Security-Client")
	}
}

func TestSecurityModeIsIPSec3GPP(t *testing.T) {
	if !securityModeIsIPSec3GPP("ipsec-3gpp") {
		t.Error("ipsec-3gpp should be detected")
	}
	if securityModeIsIPSec3GPP("tls") {
		t.Error("tls should not be ipsec-3gpp")
	}
}

func TestIsDialogOwnedHeader(t *testing.T) {
	if !isDialogOwnedHeader("Route") {
		t.Error("Route should be dialog-owned")
	}
	if isDialogOwnedHeader("Call-ID") {
		t.Error("Call-ID should not be dialog-owned")
	}
}

func TestBuildCancelFromInvite(t *testing.T) {
	uri, _ := ParseURI("sip:callee@example.com")
	invite, err := BuildIMSRequest("INVITE", uri, nil)
	if err != nil {
		t.Fatalf("BuildIMSRequest: %v", err)
	}
	invite.AppendHeader(sip.NewHeader("Call-ID", "abc"))
	invite.AppendHeader(sip.NewHeader("CSeq", "1 INVITE"))
	cancel, err := BuildCancelFromInvite(invite)
	if err != nil {
		t.Fatalf("BuildCancel: %v", err)
	}
	if !strings.Contains(cancel.String(), "CANCEL") {
		t.Error("cancel should be a CANCEL")
	}
}
