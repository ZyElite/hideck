package policy

import "github.com/iniwex5/vowifi-go/internal/vowifi/common"

func plmnKey(mcc, mnc string) string { return common.Plmn3(mcc) + common.Plmn3(mnc) }

func resolveMergedCarrierPreset(mcc, mnc string) (CarrierPreset, bool) {
	key := plmnKey(mcc, mnc)
	preset, found := embeddedCarrierPresets[key]
	if found {
		preset = cloneCarrierPreset(preset)
	} else {
		preset = CarrierPreset{MCC: common.Plmn3(mcc), MNC: common.Plmn3(mnc)}
	}
	override, external := carrierOverrideByKey(key)
	if external {
		preset = applyCarrierOverride(preset, override)
		if preset.ID == "" {
			preset.ID = "external_" + key
		}
		found = true
	}
	preset.MCC, preset.MNC = common.Plmn3(mcc), common.Plmn3(mnc)
	return preset, found
}

func ResolveEffectiveCarrierConfig(mcc, mnc string) EffectiveCarrierConfig {
	mcc, mnc = common.Plmn3(mcc), common.Plmn3(mnc)
	config := GetGlobalDefaultConfig(mcc, mnc)
	preset, found := resolveMergedCarrierPreset(mcc, mnc)
	if !found {
		return config
	}
	config.MergeFromPreset(preset)
	if hasExternalCarrierOverrideRegisterPolicyKey(plmnKey(mcc, mnc)) {
		config.IMSRegisterPolicySource = "external"
	}
	syncCompatibilityProjection(&config)
	return config
}
