package swu

import (
	"fmt"
	"strings"
)

// Algorithm policy names recovered from the decompiled
// normalizeAlgorithmPolicy: the client offers a "strict" (strong algorithms
// only), "legacy_prefer" (prefer legacy such as 3DES) or default policy.
const (
	policyStrict       = "strict"
	policyLegacyPrefer = "legacy_prefer"
	policyPrefer       = "prefer"
)

// normalizeAlgorithmPolicy lower-cases and trims the policy string and maps it
// to one of the canonical policy names. Unknown values map to "prefer".
func normalizeAlgorithmPolicy(policy string) string {
	p := strings.TrimSpace(strings.ToLower(policy))
	switch p {
	case policyStrict:
		return policyStrict
	case policyLegacyPrefer:
		return policyLegacyPrefer
	default:
		return policyPrefer
	}
}

// normalizeLegacyName normalises a legacy algorithm name: it lower-cases,
// strips separators and maps "triple-des" / "3des" variants to "3des".
func normalizeLegacyName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "3des" || s == "tripledes" {
		return "3des"
	}
	return s
}

// AlgorithmPlan is the resolved IKE/ESP algorithm offer set for a session.
type AlgorithmPlan struct {
	// IKE SA transforms.
	IKEEncryption uint16
	IKEPRF        uint16
	IKEIntegrity  uint16
	IKEDH         uint16
	// ESP transforms.
	ESPEncryption uint16
	ESPIntegrity  uint16
}

// buildAlgorithmPlan resolves the algorithm plan from the configured policy
// (recovered from the decompiled buildAlgorithmPlan). The "strict" policy
// offers AES-GCM + SHA-256 + MODP-2048; "legacy_prefer" offers 3DES + SHA-1;
// the default "prefer" offers AES-CBC + SHA-1 + MODP-2048.
func buildAlgorithmPlan(policy string, cfg *Config) *AlgorithmPlan {
	plan := &AlgorithmPlan{}
	switch normalizeAlgorithmPolicy(policy) {
	case policyStrict:
		plan.IKEEncryption = 18 // ENCR_AES_GCM_16
		plan.IKEPRF = 5         // PRF_HMAC_SHA2_256
		plan.IKEIntegrity = 0   // AEAD (no separate integrity)
		plan.IKEDH = 14         // MODP-2048
		plan.ESPEncryption = 18 // ENCR_AES_GCM_16
		plan.ESPIntegrity = 0
	case policyLegacyPrefer:
		plan.IKEEncryption = 3 // ENCR_3DES
		plan.IKEPRF = 2        // PRF_HMAC_SHA1
		plan.IKEIntegrity = 2  // AUTH_HMAC_SHA1_96
		plan.IKEDH = 2         // MODP-1024
		plan.ESPEncryption = 3 // ENCR_3DES
		plan.ESPIntegrity = 2
	default: // prefer
		plan.IKEEncryption = 12 // ENCR_AES_CBC
		plan.IKEPRF = 2         // PRF_HMAC_SHA1
		plan.IKEIntegrity = 2   // AUTH_HMAC_SHA1_96
		plan.IKEDH = 14         // MODP-2048
		plan.ESPEncryption = 12 // ENCR_AES_CBC
		plan.ESPIntegrity = 2
	}
	// Explicit configuration overrides the policy.
	if cfg != nil {
		if cfg.IKEEncryption != 0 {
			plan.IKEEncryption = cfg.IKEEncryption
		}
		if cfg.IKEPRF != 0 {
			plan.IKEPRF = cfg.IKEPRF
		}
		if cfg.IKEIntegrity != 0 {
			plan.IKEIntegrity = cfg.IKEIntegrity
		}
		if cfg.IKEDH != 0 {
			plan.IKEDH = cfg.IKEDH
		}
		if cfg.ESPEncryption != 0 {
			plan.ESPEncryption = cfg.ESPEncryption
		}
		if cfg.ESPIntegrity != 0 {
			plan.ESPIntegrity = cfg.ESPIntegrity
		}
	}
	return plan
}

// buildNAI builds the EAP-AKA Network Access Identifier (3GPP TS 23.003) from
// an IMSI. The MCC (3 digits) and MNC (2-3 digits) are taken from the IMSI
// unless mccOverride/mncOverride are non-empty. A 2-digit MNC is zero-padded
// to 3 digits.
//
//	NAI = "0<IMSI>@nai.epc.mnc<MNC>.mcc<MCC>.3gppnetwork.org"
func buildNAI(imsi string, mccOverride, mncOverride string) string {
	if len(imsi) < 5 {
		return ""
	}
	mcc := imsi[:3]
	mnc := imsi[3:5]
	if mccOverride != "" {
		mcc = mccOverride
	}
	if mncOverride != "" {
		mnc = mncOverride
	}
	if len(mnc) == 2 {
		mnc = "0" + mnc
	}
	return fmt.Sprintf("0%s@nai.epc.mnc%s.mcc%s.3gppnetwork.org", imsi, mnc, mcc)
}
