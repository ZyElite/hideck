package carrier

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveGiffgaffPreset(t *testing.T) {
	cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "234", MNC: "010"})
	if cfg.PresetID != "giffgaff_23410" || cfg.DeviceModel != "rmx3366" {
		t.Fatalf("giffgaff identity = %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.IKEProposals, []string{"aes256-sha512-prfsha512-modp2048"}) ||
		!reflect.DeepEqual(cfg.ESPProposals, []string{"aes256-sha512"}) || cfg.ReauthIntervalSeconds != 180 {
		t.Fatalf("giffgaff tunnel policy = %+v", cfg)
	}
	wantOrder := []string{
		"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
		"mid_call", "srvcc_alerting", "ps2cs_srvcc_orig_pre_alerting",
	}
	if !reflect.DeepEqual(cfg.IMS.ContactOrder, wantOrder) {
		t.Fatalf("giffgaff Contact order = %+v", cfg.IMS.ContactOrder)
	}
}

func TestResolveCarrierReturnsIndependentSlices(t *testing.T) {
	first := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
	first.IMS.ContactOrder[0] = "changed"
	first.IKEProposals[0] = "changed"
	second := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
	if second.IMS.ContactOrder[0] != "access_type" || second.IKEProposals[0] != IKEProposalAES256SHA512PRFSHA512MODP2048 {
		t.Fatalf("carrier preset was mutated: %+v", second)
	}
}

func TestValidateEffectiveCarrierRejectsUnknownContact(t *testing.T) {
	cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
	cfg.IMS.ContactOrder = append(cfg.IMS.ContactOrder, "unknown")
	err := ValidateEffectiveCarrierConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "unsupported IMS Contact parameter") {
		t.Fatalf("ValidateEffectiveCarrierConfig() error = %v", err)
	}
}
