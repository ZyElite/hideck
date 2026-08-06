// Package carrier resolves the effective carrier (PLMN) configuration for a
// VoWiFi session: e911 availability, IMS registration template and VoWiFi
// policy (blocked MCCs).
//
// Reconstructed from the decompiled engine/runtimehost/carrier.
package carrier

import (
	"encoding/json"
	"errors"
	"os"
)

// EffectiveCarrierConfigInput selects the carrier by PLMN.
type EffectiveCarrierConfigInput struct {
	MCC string
	MNC string
}

// E911Config describes the carrier's e911 (emergency address) service.
type E911Config struct {
	Enabled  bool
	Provider string
}

// IMSRegisterTemplate is the IMS registration template for the carrier.
type IMSRegisterTemplate struct {
	// (recovered as needed)
}

// EffectiveCarrierConfig is the resolved carrier configuration.
type EffectiveCarrierConfig struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Config
	IMS      IMSRegisterTemplate
}

// CarrierOverride overrides a carrier's configuration at runtime.
type CarrierOverride struct {
	MCC      string
	MNC      string
	PresetID string
	E911     E911Config
}

// ErrVoWiFiBlockedMCC is returned when the carrier's MCC is blocked for
// VoWiFi.
type ErrVoWiFiBlockedMCC struct {
	MCC string
}

func (e *ErrVoWiFiBlockedMCC) Error() string {
	return "vowifi blocked for MCC " + e.MCC
}

// NewVoWiFiBlockedMCCError returns an error indicating VoWiFi is blocked for
// the given MCC.
func NewVoWiFiBlockedMCCError(mcc string) error {
	return &ErrVoWiFiBlockedMCC{MCC: mcc}
}

// IsVoWiFiPolicyBlockedError reports whether err is a VoWiFi policy block.
func IsVoWiFiPolicyBlockedError(err error) bool {
	var e *ErrVoWiFiBlockedMCC
	return errors.As(err, &e)
}

// blockedMCCs is the set of MCCs where VoWiFi is not offered.
var blockedMCCs = map[string]bool{
	// China Mobile: VoWiFi not offered on the standard IMS path.
	"460": true,
}

// IsVoWiFiBlockedMCC reports whether VoWiFi is blocked for the given MCC.
func IsVoWiFiBlockedMCC(mcc string) bool {
	return blockedMCCs[mcc]
}

// overrides holds the runtime carrier overrides.
var overrides []CarrierOverride

// LoadResult describes the outcome of loading carrier overrides.
type LoadResult struct {
	Path    string
	Missing bool
	Count   int
}

// LoadCarrierOverrides loads carrier overrides from a JSON file.
func LoadCarrierOverrides(path string) (LoadResult, error) {
	if path == "" {
		path = "carrier_overrides.json"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LoadResult{Path: path, Missing: true}, nil
		}
		return LoadResult{Path: path}, err
	}
	var list []CarrierOverride
	if err := json.Unmarshal(data, &list); err != nil {
		return LoadResult{Path: path}, err
	}
	overrides = list
	return LoadResult{Path: path, Count: len(list)}, nil
}

// ClearCarrierOverrides clears the loaded overrides.
func ClearCarrierOverrides() {
	overrides = nil
}

// ResolveEffectiveCarrierConfig resolves the carrier configuration for the
// given PLMN, applying any runtime overrides.
func ResolveEffectiveCarrierConfig(input EffectiveCarrierConfigInput) EffectiveCarrierConfig {
	cfg := EffectiveCarrierConfig{MCC: input.MCC, MNC: input.MNC}
	for _, o := range overrides {
		if o.MCC == input.MCC && o.MNC == input.MNC {
			cfg.PresetID = o.PresetID
			cfg.E911 = o.E911
			return cfg
		}
	}
	// Built-in presets: AT&T (310/280) offers e911.
	if input.MCC == "310" && input.MNC == "280" {
		cfg.PresetID = "att"
		cfg.E911 = E911Config{Enabled: true, Provider: "att"}
	}
	return cfg
}
