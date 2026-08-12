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
		Profile:       identity.Profile{MCC: "234", MNC: "10"},
		CarrierConfig: carrier.ResolveEffectiveCarrierConfig("234", "10"),
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
	wantICSIRef := "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.sms," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.msg," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.sms"
	if template.Expires != 600000*time.Second || template.SupportedHeader != "path,sec-agree" ||
		template.ContactMode != "android_default" || template.ICSIRef != wantICSIRef {
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
	cfg := carrier.ResolveEffectiveCarrierConfig("234", "10")
	cfg.IMS.ContactMode = "unknown"
	_, _, err := imsRegisterConfigForPrepared(&identity.PreparedSession{CarrierConfig: cfg})
	if err == nil || !strings.Contains(err.Error(), "unsupported IMS Contact mode") {
		t.Fatalf("imsRegisterConfigForPrepared() error = %v", err)
	}
}

func TestRecoveredPreparedCarrierFeedsProductionConsumers(t *testing.T) {
	config := carrier.ResolveEffectiveCarrierConfig("234", "10")
	prepared := &identity.PreparedSession{
		Profile:          identity.Profile{MCC: "234", MNC: "10"},
		EffectiveCarrier: config,
	}
	template, userAgent, err := imsRegisterConfigForPrepared(prepared)
	if err != nil {
		t.Fatalf("imsRegisterConfigForPrepared() error = %v", err)
	}
	if userAgent != "iOS/18.2.1 iPhone (iPhone15,4)" ||
		template.ContactMode != "android_default" || !template.StrictSecurityServerOffer {
		t.Fatalf("IMS template = %+v, user agent = %q", template, userAgent)
	}
	corePrepared := preparedForRuntimeCore(prepared)
	if corePrepared.Carrier.PresetID != config.PresetID || corePrepared.Carrier.MCC != "234" {
		t.Fatalf("runtimecore carrier = %+v", corePrepared.Carrier)
	}
}
