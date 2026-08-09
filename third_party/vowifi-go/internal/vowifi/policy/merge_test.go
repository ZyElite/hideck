package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMergeFromPresetPortAndTimerBoundaries(t *testing.T) {
	portCases := []struct {
		name string
		port int
		want uint16
	}{
		{name: "negative", port: -1, want: 500},
		{name: "zero", port: 0, want: 500},
		{name: "minimum", port: 1, want: 1},
		{name: "maximum", port: 65535, want: 65535},
		{name: "overflow", port: 65536, want: 500},
	}
	for _, test := range portCases {
		t.Run("epdg_"+test.name, func(t *testing.T) {
			config := GetGlobalDefaultConfig("310", "260")
			config.MergeFromPreset(CarrierPreset{EPDGPort: &test.port})
			if config.EPDGPort != test.want {
				t.Fatalf("EPDG port = %d, want %d", config.EPDGPort, test.want)
			}
		})
	}

	dpdCases := []struct {
		name  string
		value int
		want  int
	}{
		{name: "below", value: 19, want: 120},
		{name: "minimum", value: 20, want: 20},
		{name: "maximum", value: 1800, want: 1800},
		{name: "above", value: 1801, want: 120},
	}
	for _, test := range dpdCases {
		t.Run("dpd_"+test.name, func(t *testing.T) {
			config := GetGlobalDefaultConfig("310", "260")
			config.MergeFromPreset(CarrierPreset{DPDIntervalSeconds: &test.value})
			if config.DPDIntervalSeconds != test.want {
				t.Fatalf("DPD interval = %d, want %d", config.DPDIntervalSeconds, test.want)
			}
		})
	}
}

func TestMergeFromPresetPreservesDefaultsForInvalidIMSValues(t *testing.T) {
	invalidPort, zero := 65536, 0
	config := GetGlobalDefaultConfig("310", "260")
	config.MergeFromPreset(CarrierPreset{
		IMSLocalPort:                  &invalidPort,
		IMSTCPKeepaliveSeconds:        &zero,
		IMSOptionsPingIntervalSeconds: &zero,
		DPDKeepaliveIntervalSeconds:   -1,
		ReauthIntervalSeconds:         -1,
	})
	if config.IMSLocalPort != 5060 || config.IMSTCPKeepaliveSeconds != 30 ||
		config.IMSOptionsPingIntervalSeconds != 45 {
		t.Fatalf("invalid IMS values changed defaults: %+v", config)
	}
	if config.DPDKeepaliveIntervalSeconds != 0 || config.ReauthIntervalSeconds != 0 {
		t.Fatalf("invalid timers were applied: %+v", config)
	}
}

func TestExternalOverrideMergesAfterEmbeddedPreset(t *testing.T) {
	ClearCarrierOverrides()
	t.Cleanup(ClearCarrierOverrides)
	emptyCodes := []int{}
	err := SetCarrierOverrides(map[string]CarrierOverride{
		"23410": {
			IMSDomain: "external.ims.example",
			IMSRegisterTemplate: IMSRegisterTemplateOverride{
				RegisterPolicy: IMSRegisterPolicyOverride{
					ID:                   "external-policy",
					ForbiddenStatusCodes: &emptyCodes,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SetCarrierOverrides() error = %v", err)
	}
	config := ResolveEffectiveCarrierConfig("234", "10")
	if config.PresetID != "giffgaff_23410" || config.DeviceModel != "rmx3366" {
		t.Fatalf("embedded preset was not retained: %+v", config)
	}
	if config.IMSDomain != "external.ims.example" || config.IMSRegisterPolicySource != "external" {
		t.Fatalf("external override was not applied last: %+v", config)
	}
	policy := config.IMSRegisterTemplate.RegisterPolicy
	if policy.ID != "external-policy" || len(policy.ForbiddenStatusCodes) != 0 {
		t.Fatalf("explicit empty register policy was lost: %+v", policy)
	}
}

func TestLoadCarrierOverridesFileDirectAndFailureCases(t *testing.T) {
	tempDir := t.TempDir()
	directPath := filepath.Join(tempDir, "direct.yaml")
	writeTestPolicyFile(t, directPath, "234-10:\n  id: direct\n  custom_epdg: epdg.direct\n")
	values, err := LoadCarrierOverridesFile(directPath)
	if err != nil {
		t.Fatalf("LoadCarrierOverridesFile() error = %v", err)
	}
	if values["234010"].ID != "direct" || values["234010"].CustomEPDG != "epdg.direct" {
		t.Fatalf("direct values = %+v", values)
	}

	missing, err := LoadCarrierOverridesFile(filepath.Join(tempDir, "missing.yaml"))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing file = %+v, %v", missing, err)
	}
	invalidKey := filepath.Join(tempDir, "invalid-key.yaml")
	writeTestPolicyFile(t, invalidKey, "invalid:\n  id: bad\n")
	if _, err := LoadCarrierOverridesFile(invalidKey); err == nil {
		t.Fatal("LoadCarrierOverridesFile() accepted an invalid PLMN key")
	}
	malformed := filepath.Join(tempDir, "malformed.yaml")
	writeTestPolicyFile(t, malformed, "carrier_overrides: [")
	if _, err := LoadCarrierOverridesFile(malformed); err == nil {
		t.Fatal("LoadCarrierOverridesFile() accepted malformed YAML")
	}
}

func TestCarrierPlanIsZero(t *testing.T) {
	if !(CarrierPlan{}).IsZero() {
		t.Fatal("zero CarrierPlan reported non-zero")
	}
	plan := CarrierPlanFromEffectiveConfig(GetGlobalDefaultConfig("310", "260"))
	if plan.IsZero() {
		t.Fatal("populated CarrierPlan reported zero")
	}
	copy := plan
	copy.IKE.IKEProposals = append([]string(nil), plan.IKE.IKEProposals...)
	if !reflect.DeepEqual(copy, plan) {
		t.Fatal("CarrierPlan copy unexpectedly changed values")
	}
}

func writeTestPolicyFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
