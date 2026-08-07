package runtimehost

import (
	"reflect"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

func TestIMSRegisterTemplateForGiffgaff(t *testing.T) {
	template := imsRegisterTemplateForProfile(identity.Profile{MCC: "234", MNC: "10"})
	wantOrder := []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
		"mid_call", "srvcc_alerting", "ps2cs_srvcc_orig_pre_alerting",
	}
	if template.AccessType != "wlan1" || !reflect.DeepEqual(template.ContactOrder, wantOrder) {
		t.Fatalf("giffgaff IMS REGISTER template = %+v", template)
	}
	if template.ICSIRef != defaultIMSICSIRef || len(template.ICSIRef) != 137 {
		t.Fatalf("giffgaff ICSI ref = %q", template.ICSIRef)
	}
	if template.Expires != 600000*time.Second || template.SupportedHeader != "path,sec-agree" ||
		template.ContactMode != "android_default" || template.AllowHeader != defaultIMSAllowHeader {
		t.Fatalf("giffgaff inherited defaults = %+v", template)
	}
}

func TestIMSContactConfigReturnsIndependentOrder(t *testing.T) {
	first := imsRegisterTemplateForProfile(identity.Profile{MCC: "234", MNC: "010"})
	first.ContactOrder[0] = "changed"
	second := imsRegisterTemplateForProfile(identity.Profile{MCC: "234", MNC: "10"})
	if second.ContactOrder[0] != "access_type" {
		t.Fatalf("carrier Contact order was mutated: %+v", second.ContactOrder)
	}
}
