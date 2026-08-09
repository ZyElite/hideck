package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeIMSRegisterPolicy(t *testing.T) {
	cases := map[string]string{
		"auto":   "auto",
		"AUTO":   "auto",
		"manual": "manual",
		"":       "auto",
		"x":      "auto",
	}
	for in, want := range cases {
		if got := NormalizeIMSRegisterPolicy(in); got != want {
			t.Errorf("NormalizeIMSRegisterPolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeIMSTransport(t *testing.T) {
	if got := NormalizeIMSTransport("TCP"); got != "tcp" {
		t.Errorf("TCP = %q", got)
	}
	if got := NormalizeIMSTransport(""); got != "udp" {
		t.Errorf("default = %q", got)
	}
}

func TestDefaultCarrierIMSDomain(t *testing.T) {
	if got := DefaultCarrierIMSDomain("310", "26"); got != "ims.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("domain = %q", got)
	}
}

func TestResolveEffectiveCarrierConfig(t *testing.T) {
	cfg := ResolveEffectiveCarrierConfig("310", "280")
	if cfg.PresetID != "att" {
		t.Errorf("preset = %q", cfg.PresetID)
	}
	if !cfg.E911.Enabled {
		t.Error("att e911 should be enabled")
	}
	// Unknown PLMN gets defaults.
	cfg = ResolveEffectiveCarrierConfig("999", "99")
	if cfg.IMS.Domain == "" {
		t.Error("unknown PLMN should get a default domain")
	}
}

func TestIsVoWiFiBlockedMCC(t *testing.T) {
	if !IsVoWiFiBlockedMCC("460") {
		t.Error("460 should be blocked")
	}
	if IsVoWiFiBlockedMCC("310") {
		t.Error("310 should not be blocked")
	}
}

func TestVoWiFiPolicyBlockError(t *testing.T) {
	e := NewVoWiFiBlockedMCCError("460")
	if !strings.Contains(e.Error(), "460") {
		t.Errorf("error = %q", e.Error())
	}
}

func TestCarrierOverrides(t *testing.T) {
	ClearCarrierOverrides()
	SetCarrierOverrides([]CarrierOverride{
		{MCC: "310", MNC: "260", IMS: IMSRegisterTemplate{Domain: "override.example.com"}},
	})
	cfg := ResolveEffectiveCarrierConfig("310", "260")
	if cfg.IMS.Domain != "override.example.com" {
		t.Errorf("override domain = %q", cfg.IMS.Domain)
	}
	ClearCarrierOverrides()
	cfg = ResolveEffectiveCarrierConfig("310", "260")
	if cfg.IMS.Domain == "override.example.com" {
		t.Error("override should be cleared")
	}
}

func TestCarrierPlanRoundTrip(t *testing.T) {
	cfg := ResolveEffectiveCarrierConfig("310", "280")
	plan := CarrierPlanFromEffectiveConfig(cfg)
	back := EffectiveCarrierConfigFromCarrierPlan(plan)
	if back.MCC != cfg.MCC || back.PresetID != cfg.PresetID {
		t.Errorf("round trip = %+v", back)
	}
}

func TestIMSRegisterTemplateJSONSchemas(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		policyID   string
		policyMode string
		secMode    string
		secEnabled bool
		domain     string
	}{
		{
			name: "recovered", input: `{"Domain":"ims.example","RegisterPolicy":{"ID":"temporary"},"SecAgreeMode":"required"}`,
			policyID: "temporary", secMode: "required", domain: "ims.example",
		},
		{
			name: "interim", input: `{"RegisterPolicy":"manual","SecAgreeMode":true}`,
			policyMode: "manual", secEnabled: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var template IMSRegisterTemplate
			if err := json.Unmarshal([]byte(test.input), &template); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if template.RegisterPolicy.ID != test.policyID || template.RegisterPolicyMode != test.policyMode ||
				template.SecAgreeMode != test.secMode || template.SecAgreeEnabled != test.secEnabled ||
				template.Domain != test.domain {
				t.Fatalf("template = %+v", template)
			}
		})
	}
}

func TestIMSRegisterTemplateJSONRejectsInvalidUnionTypes(t *testing.T) {
	for _, input := range []string{
		`{"RegisterPolicy":true}`,
		`{"SecAgreeMode":42}`,
	} {
		var template IMSRegisterTemplate
		if err := json.Unmarshal([]byte(input), &template); err == nil {
			t.Fatalf("Unmarshal(%s) succeeded", input)
		}
	}
}
