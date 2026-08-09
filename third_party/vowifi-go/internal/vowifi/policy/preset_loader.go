package policy

import (
	"embed"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"gopkg.in/yaml.v3"
)

//go:embed presets/*.yaml
var carrierPresetFiles embed.FS

type embeddedCarrierPresetFile struct {
	MCC             string `yaml:"mcc"`
	MNC             string `yaml:"mnc"`
	CarrierOverride `yaml:",inline"`
}

var embeddedCarrierPresets = mustLoadEmbeddedCarrierPresetRegistry()

func loadEmbeddedCarrierPresets() (map[string]CarrierPreset, error) {
	entries, err := carrierPresetFiles.ReadDir("presets")
	if err != nil {
		return nil, err
	}
	result := make(map[string]CarrierPreset, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := carrierPresetFiles.ReadFile("presets/" + entry.Name())
		if err != nil {
			return nil, err
		}
		var file embeddedCarrierPresetFile
		if err := yaml.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("parse carrier preset %s: %w", entry.Name(), err)
		}
		mcc, mnc := common.Plmn3(file.MCC), common.Plmn3(file.MNC)
		if mcc == "" || mnc == "" {
			return nil, fmt.Errorf("carrier preset %s has invalid PLMN", entry.Name())
		}
		preset := applyCarrierOverride(CarrierPreset{MCC: mcc, MNC: mnc}, file.CarrierOverride)
		if strings.TrimSpace(preset.ID) == "" {
			return nil, fmt.Errorf("carrier preset %s has empty id", entry.Name())
		}
		result[plmnKey(mcc, mnc)] = preset
	}
	return result, nil
}

func mustLoadEmbeddedCarrierPresetRegistry() map[string]CarrierPreset {
	values, err := loadEmbeddedCarrierPresets()
	if err != nil {
		panic(err)
	}
	return values
}

func mustBuildCarrierPresetRegistry() map[string]CarrierPreset {
	result := make(map[string]CarrierPreset, len(embeddedCarrierPresets))
	for key, value := range embeddedCarrierPresets {
		result[key] = cloneCarrierPreset(value)
	}
	return result
}

func cloneCarrierPreset(value CarrierPreset) CarrierPreset {
	value.EPDGPort = cloneIntPointer(value.EPDGPort)
	value.DeviceIdentityEnabled = cloneBoolPointer(value.DeviceIdentityEnabled)
	value.NATKeepaliveSeconds = cloneIntPointer(value.NATKeepaliveSeconds)
	value.DPDIntervalSeconds = cloneIntPointer(value.DPDIntervalSeconds)
	value.EnableLegacyCiphers = cloneBoolPointer(value.EnableLegacyCiphers)
	value.IMSLocalPort = cloneIntPointer(value.IMSLocalPort)
	value.IMSTCPKeepaliveSeconds = cloneIntPointer(value.IMSTCPKeepaliveSeconds)
	value.IMSOptionsPingIntervalSeconds = cloneIntPointer(value.IMSOptionsPingIntervalSeconds)
	value.ForceSMSCAuth = cloneBoolPointer(value.ForceSMSCAuth)
	value.AllowedLegacyCiphers = cloneStrings(value.AllowedLegacyCiphers)
	value.IKEProposals = cloneStrings(value.IKEProposals)
	value.ESPProposals = cloneStrings(value.ESPProposals)
	value.IMSRegisterTemplate = cloneIMSRegisterTemplate(value.IMSRegisterTemplate)
	return value
}
