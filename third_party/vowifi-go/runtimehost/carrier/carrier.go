package carrier

import "github.com/iniwex5/vowifi-go/internal/vowifi/policy"

const (
	IKEProposalAES256SHA512PRFSHA512MODP2048 = "aes256-sha512-prfsha512-modp2048"
	ESPProposalAES256SHA512                  = "aes256-sha512"
)

// ResolveEffectiveCarrierConfig resolves the recovered public carrier surface.
func ResolveEffectiveCarrierConfig(mcc, mnc string) EffectiveCarrierConfig {
	return CarrierConfigFromInternal(policy.ResolveEffectiveCarrierConfig(mcc, mnc))
}

// ResolveEffectiveCarrierConfigInput retains the post-v1.5.5 struct input API.
func ResolveEffectiveCarrierConfigInput(input EffectiveCarrierConfigInput) EffectiveCarrierConfig {
	return ResolveEffectiveCarrierConfig(input.MCC, input.MNC)
}

// ResolveEffectiveCarrierConfigCurrent is the explicit current-name alias.
func ResolveEffectiveCarrierConfigCurrent(input EffectiveCarrierConfigInput) EffectiveCarrierConfig {
	return ResolveEffectiveCarrierConfigInput(input)
}

// LoadCarrierOverrides restores the v1.5.5 YAML loader and return contract.
func LoadCarrierOverrides(path string) (resolvedPath string, count int, missing bool, err error) {
	return policy.LoadAndSetCarrierOverridesFile(path)
}

// ClearCarrierOverrides clears the shared policy override store.
func ClearCarrierOverrides() {
	policy.ClearCarrierOverrides()
}
