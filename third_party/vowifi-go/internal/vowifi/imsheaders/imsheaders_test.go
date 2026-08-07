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
	if got := sipInstanceIMEIDigits("35-693803-564380-9"); got != "356938035643809" {
		t.Errorf("imei = %q", got)
	}
	if got := sipInstanceIMEIDigits("not-an-imei"); got != "" {
		t.Errorf("invalid IMEI = %q", got)
	}
}

func TestNormalizeSipInstance(t *testing.T) {
	if got := NormalizeSipInstance("356938035643809"); got != "<urn:gsma:imei:35693803-564380-9>" {
		t.Errorf("15-digit IMEI = %q", got)
	}
	if got := NormalizeSipInstance("35693803564380"); got != "<urn:gsma:imei:35693803-564380-9>" {
		t.Errorf("14-digit IMEI = %q", got)
	}
	if got := NormalizeSipInstance("urn:uuid:abc"); got != "<urn:uuid:abc>" {
		t.Errorf("UUID URN = %q", got)
	}
}

func TestContactURIWithOptions(t *testing.T) {
	got := ContactURIWithOptions("sip:user@example.com", "356938035643809", 3600, 1)
	if !strings.Contains(got, "expires=3600") || !strings.Contains(got, "reg-id=1") {
		t.Errorf("contact = %q", got)
	}
	if !strings.Contains(got, `+sip.instance="<urn:gsma:imei:35693803-564380-9>"`) {
		t.Errorf("contact instance = %q", got)
	}
}

func TestIMSContactURIUsesRecoveredParameterOrder(t *testing.T) {
	const icsi = "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel"
	got := IMSContactURI("sip:user@192.0.2.10:5060", IMSContactOptions{
		Transport: "UDP", AccessType: "IEEE-802.11",
		Instance: "356938035643809", ICSIRef: icsi,
		ParamOrder: []string{
			"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
			"mid_call", "srvcc_alerting", "ps2cs_srvcc_orig_pre_alerting",
		},
	})
	want := `<sip:user@192.0.2.10:5060;transport=udp>` +
		`;+g.3gpp.accesstype="IEEE-802.11"` +
		`;+sip.instance="<urn:gsma:imei:35693803-564380-9>"` +
		`;audio;+g.3gpp.smsip` +
		`;+g.3gpp.icsi-ref="` + icsi + `"` +
		`;+g.3gpp.mid-call;+g.3gpp.srvcc-alerting` +
		`;+g.3gpp.ps2cs-srvcc-orig-pre-alerting`
	if got != want {
		t.Fatalf("IMS Contact = %q\nwant        = %q", got, want)
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
