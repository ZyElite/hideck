package imsheaders

import (
	"strings"
	"testing"
)

func TestIcsIRefValue(t *testing.T) {
	if got := icsiRefValue("mmtel"); got != "urn:urn-7:3gpp-service.ims.icsi.mmtel" {
		t.Errorf("icsiRefValue = %q", got)
	}
	if got := icsiRefValue("urn:urn-7:3gpp-service.ims.icsi.mmtel"); got != "urn:urn-7:3gpp-service.ims.icsi.mmtel" {
		t.Errorf("icsiRefValue passthrough = %q", got)
	}
}

func TestFormatHostForSIP(t *testing.T) {
	if got := formatHostForSIP("2001:db8::1"); got != "[2001:db8::1]" {
		t.Errorf("IPv6 = %q", got)
	}
	if got := formatHostForSIP("example.com"); got != "example.com" {
		t.Errorf("host = %q", got)
	}
}

func TestSipInstanceIMEIDigits(t *testing.T) {
	if got := sipInstanceIMEIDigits("356938035643809"); got != "356938035643809" {
		t.Errorf("imei = %q", got)
	}
}

func TestNormalizeSipInstance(t *testing.T) {
	if got := NormalizeSipInstance("urn:uuid:abc"); got != "urn:uuid:abc" {
		t.Errorf("normalize = %q", got)
	}
	if got := NormalizeSipInstance("abc"); got != "urn:uuid:abc" {
		t.Errorf("normalize bare = %q", got)
	}
}

func TestContactURIWithOptions(t *testing.T) {
	got := ContactURIWithOptions("sip:user@example.com", "urn:uuid:abc", 3600, 1)
	if !strings.Contains(got, "expires=3600") || !strings.Contains(got, "reg-id=1") {
		t.Errorf("contact = %q", got)
	}
}

func TestExtractPhoneFromAssociatedMSISDN(t *testing.T) {
	if got := ExtractPhoneFromAssociatedMSISDN("tel:+8613800000000"); got != "8613800000000" {
		t.Errorf("tel = %q", got)
	}
	if got := ExtractPhoneFromAssociatedMSISDN("sip:8613800000000@example.com"); got != "8613800000000" {
		t.Errorf("sip = %q", got)
	}
	if got := ExtractPhoneFromAssociatedMSISDN("sip:user@example.com"); got != "" {
		t.Errorf("no phone = %q", got)
	}
}

func TestPreferredIdentityHeaderValue(t *testing.T) {
	if got := PreferredIdentityHeaderValue("8613800000000", "example.com"); got != "sip:8613800000000@example.com" {
		t.Errorf("identity = %q", got)
	}
	if got := PreferredIdentityHeaderValue("8613800000000", ""); got != "tel:8613800000000" {
		t.Errorf("identity no domain = %q", got)
	}
}

func TestRouteSet(t *testing.T) {
	got := RouteSet([]string{"sip:proxy.example.com", "<sip:proxy2.example.com>"})
	if !strings.Contains(got, "<sip:proxy.example.com>") || !strings.Contains(got, "<sip:proxy2.example.com>") {
		t.Errorf("route = %q", got)
	}
}

func TestSecAgreeProtectedHeaders(t *testing.T) {
	headers := SecAgreeProtectedHeaders()
	if len(headers) == 0 {
		t.Error("no protected headers")
	}
	found := false
	for _, h := range headers {
		if h == "Security-Client" {
			found = true
		}
	}
	if !found {
		t.Error("Security-Client not protected")
	}
}
