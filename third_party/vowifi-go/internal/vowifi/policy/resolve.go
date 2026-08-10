package policy

import "github.com/iniwex5/vowifi-go/internal/vowifi/common"

func plmnKey(mcc, mnc string) string { return common.Plmn3(mcc) + common.Plmn3(mnc) }

func resolveMergedCarrierPreset(mcc, mnc string) (CarrierPreset, bool) {
	preset, found, _ := resolveCarrierPreset(mcc, mnc, nil)
	return preset, found
}

func resolveCarrierPreset(mcc, mnc string, candidate *CarrierOverride) (CarrierPreset, bool, bool) {
	key := plmnKey(mcc, mnc)
	preset, found := embeddedCarrierPresets[key]
	if found {
		preset = cloneCarrierPreset(preset)
	} else {
		preset = CarrierPreset{MCC: common.Plmn3(mcc), MNC: common.Plmn3(mnc)}
	}
	override, external := CarrierOverride{}, candidate != nil
	if candidate != nil {
		override = cloneCarrierOverride(*candidate)
	} else {
		override, external = carrierOverrideByKey(key)
	}
	if external {
		preset = applyCarrierOverride(preset, override)
		if preset.ID == "" {
			preset.ID = "external_" + key
		}
		found = true
	}
	preset.MCC, preset.MNC = common.Plmn3(mcc), common.Plmn3(mnc)
	return preset, found, external
}

func ResolveEffectiveCarrierConfig(mcc, mnc string) EffectiveCarrierConfig {
	return resolveEffectiveCarrierConfig(mcc, mnc, nil)
}

// ResolveEffectiveCarrierConfigWithOverride resolves one candidate without
// reading or mutating the process-wide override store.
func ResolveEffectiveCarrierConfigWithOverride(mcc, mnc string, override CarrierOverride) EffectiveCarrierConfig {
	return resolveEffectiveCarrierConfig(mcc, mnc, &override)
}

func resolveEffectiveCarrierConfig(mcc, mnc string, candidate *CarrierOverride) EffectiveCarrierConfig {
	mcc, mnc = common.Plmn3(mcc), common.Plmn3(mnc)
	config := GetGlobalDefaultConfig(mcc, mnc)
	preset, found, external := resolveCarrierPreset(mcc, mnc, candidate)
	if !found {
		return config
	}
	config.MergeFromPreset(preset)
	if external && (candidate == nil && hasExternalCarrierOverrideRegisterPolicyKey(plmnKey(mcc, mnc)) ||
		candidate != nil && hasExplicitIMSRegisterPolicyOverride(candidate.IMSRegisterTemplate.RegisterPolicy)) {
		config.IMSRegisterPolicySource = "external"
	}
	syncCompatibilityProjection(&config)
	return config
}
