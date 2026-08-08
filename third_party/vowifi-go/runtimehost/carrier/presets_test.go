package carrier

import (
	"os"
	"path/filepath"
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

func TestResolveATTE911PresetIncludesProductionEndpoints(t *testing.T) {
	for _, mnc := range []string{"280", "410"} {
		cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "310", MNC: mnc})
		if !cfg.E911.Enabled || cfg.E911.Provider != "att-ts43" {
			t.Fatalf("AT&T %s E911 identity = %+v", mnc, cfg.E911)
		}
		if cfg.E911.Websheet != attE911Websheet || cfg.E911.EntitlementEndpoint != attE911Endpoint {
			t.Fatalf("AT&T %s E911 endpoints = %+v", mnc, cfg.E911)
		}
		if err := ValidateEffectiveCarrierConfig(cfg); err != nil {
			t.Fatalf("ValidateEffectiveCarrierConfig(%s): %v", mnc, err)
		}
	}
}

func TestValidateEffectiveCarrierRejectsMissingE911Endpoint(t *testing.T) {
	cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "310", MNC: "280"})
	cfg.E911.EntitlementEndpoint = ""
	if err := ValidateEffectiveCarrierConfig(cfg); err == nil || !strings.Contains(err.Error(), "entitlement endpoint") {
		t.Fatalf("ValidateEffectiveCarrierConfig() error = %v", err)
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

func TestValidateEffectiveCarrierRejectsInvalidIMSExpiry(t *testing.T) {
	for _, expires := range []int{0, -1} {
		cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
		cfg.IMS.ExpiresSeconds = expires
		err := ValidateEffectiveCarrierConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), "expiry must be positive") {
			t.Fatalf("ExpiresSeconds=%d error = %v", expires, err)
		}
	}
}

func TestValidateEffectiveCarrierRejectsIMSExpiryOverflow(t *testing.T) {
	maxExpires := int64(maxIMSExpiresSeconds)
	if int64(int(maxExpires)) != maxExpires {
		t.Skip("int is too small to represent an overflowing duration")
	}
	cfg := ResolveEffectiveCarrierConfig(EffectiveCarrierConfigInput{MCC: "234", MNC: "10"})
	cfg.IMS.ExpiresSeconds = int(maxExpires) + 1
	err := ValidateEffectiveCarrierConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "overflows duration") {
		t.Fatalf("ValidateEffectiveCarrierConfig() error = %v", err)
	}
}

func TestLoadCarrierOverridesRejectsExplicitZeroIMSExpiry(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	path := filepath.Join(t.TempDir(), "carrier_overrides.json")
	data := []byte(`[{"MCC":"234","MNC":"10","IMS":{"ExpiresSeconds":0}}]`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadCarrierOverrides(path)
	if err == nil || !strings.Contains(err.Error(), "expiry must be positive") {
		t.Fatalf("LoadCarrierOverrides() error = %v", err)
	}
}
