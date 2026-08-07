package runtimehost

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

func TestIMSRegisterConfigForGiffgaff(t *testing.T) {
	prepared := &identity.PreparedSession{
		Profile: identity.Profile{MCC: "234", MNC: "10"},
		CarrierConfig: carrier.ResolveEffectiveCarrierConfig(carrier.EffectiveCarrierConfigInput{
			MCC: "234", MNC: "10",
		}),
	}
	template, userAgent, err := imsRegisterConfigForPrepared(prepared)
	if err != nil {
		t.Fatalf("imsRegisterConfigForPrepared() error = %v", err)
	}
	wantOrder := []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
		"mid_call", "srvcc_alerting", "ps2cs_srvcc_orig_pre_alerting",
	}
	if !reflect.DeepEqual(template.ContactOrder, wantOrder) || template.AccessType != "wlan1" {
		t.Fatalf("giffgaff IMS template = %+v", template)
	}
	if template.Expires != 600000*time.Second || template.SupportedHeader != "path,sec-agree" ||
		template.ContactMode != "android_default" || len(template.ICSIRef) != 137 {
		t.Fatalf("giffgaff IMS defaults = %+v", template)
	}
	if userAgent != "iOS/18.2.1 iPhone (iPhone15,4)" {
		t.Fatalf("giffgaff User-Agent = %q", userAgent)
	}
	if !template.IncludePANIAuthenticated || !template.StrictSecurityServerOffer {
		t.Fatalf("giffgaff security policy = %+v", template)
	}
}

func TestIMSRegisterConfigReturnsIndependentOrder(t *testing.T) {
	prepared := &identity.PreparedSession{Profile: identity.Profile{MCC: "234", MNC: "010"}}
	first, _, err := imsRegisterConfigForPrepared(prepared)
	if err != nil {
		t.Fatal(err)
	}
	first.ContactOrder[0] = "changed"
	second, _, err := imsRegisterConfigForPrepared(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if second.ContactOrder[0] != "access_type" {
		t.Fatalf("carrier Contact order was mutated: %+v", second.ContactOrder)
	}
}

func TestIMSRegisterConfigRejectsUnknownContactMode(t *testing.T) {
	cfg := carrier.ResolveEffectiveCarrierConfig(carrier.EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
	cfg.IMS.ContactMode = "unknown"
	_, _, err := imsRegisterConfigForPrepared(&identity.PreparedSession{CarrierConfig: cfg})
	if err == nil || !strings.Contains(err.Error(), "unsupported IMS Contact mode") {
		t.Fatalf("imsRegisterConfigForPrepared() error = %v", err)
	}
}
