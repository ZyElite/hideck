package swu

import (
	"sort"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const (
	AlgorithmPolicyStrict       = "strict"
	AlgorithmPolicyBalanced     = "balanced"
	AlgorithmPolicyLegacyPrefer = "legacy_prefer"

	ErrClassAlgorithmCapabilityMismatch = "algorithm_capability_mismatch"
	ErrClassAlgorithmPolicyRejected     = "algorithm_policy_rejected"
	ErrClassDriverUnsupported           = "driver_unsupported"
)

type algorithmPlan struct {
	policy        string
	allowLegacy   bool
	allowedLegacy map[ikev2.AlgorithmType]bool
}

func buildAlgorithmPlan(cfg *Config) algorithmPlan {
	policy := normalizeAlgorithmPolicy(cfg.AlgorithmPolicy)
	allowLegacy := cfg.EnableLegacyCiphers && policy != AlgorithmPolicyStrict
	allowedLegacy := make(map[ikev2.AlgorithmType]bool)
	if allowLegacy {
		populateAllowedLegacy(allowedLegacy, cfg.AllowedLegacyCiphers)
	}
	return algorithmPlan{policy: policy, allowLegacy: allowLegacy, allowedLegacy: allowedLegacy}
}

func populateAllowedLegacy(allowed map[ikev2.AlgorithmType]bool, configured []string) {
	if len(configured) == 0 {
		allowed[ikev2.ENCR_3DES] = true
		allowed[ikev2.ENCR_DES] = true
		return
	}
	for _, raw := range configured {
		switch normalizeLegacyName(raw) {
		case "3des":
			allowed[ikev2.ENCR_3DES] = true
		case "des":
			allowed[ikev2.ENCR_DES] = true
		}
	}
}

func normalizeAlgorithmPolicy(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case AlgorithmPolicyStrict:
		return AlgorithmPolicyStrict
	case AlgorithmPolicyLegacyPrefer:
		return AlgorithmPolicyLegacyPrefer
	default:
		return AlgorithmPolicyBalanced
	}
}

func normalizeLegacyName(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	if value == "tripledes" || value == "3des" {
		return "3des"
	}
	return value
}

func (p algorithmPlan) allowsEncryption(algorithm ikev2.AlgorithmType) bool {
	if !isLegacyEncryption(algorithm) {
		return true
	}
	if p.policy == AlgorithmPolicyStrict {
		return false
	}
	return p.allowLegacy && p.allowedLegacy[algorithm]
}

func (p algorithmPlan) policyLabel() string {
	if p.policy == AlgorithmPolicyStrict {
		return AlgorithmPolicyStrict
	}
	if p.allowLegacy && p.policy == AlgorithmPolicyLegacyPrefer {
		return AlgorithmPolicyLegacyPrefer
	}
	if p.allowLegacy {
		return "balanced+legacy"
	}
	return AlgorithmPolicyBalanced
}

func (p algorithmPlan) effectiveAlgSetLabel() []string {
	result := []string{"aes_cbc", "aes_gcm", "sha1/sha2", "prf_sha1/sha2", "dh_modp/ecp"}
	if p.allowsEncryption(ikev2.ENCR_3DES) {
		result = append(result, "3des")
	}
	if p.allowsEncryption(ikev2.ENCR_DES) {
		result = append(result, "des")
	}
	sort.Strings(result)
	return result
}

func isLegacyEncryption(algorithm ikev2.AlgorithmType) bool {
	return algorithm == ikev2.ENCR_DES || algorithm == ikev2.ENCR_3DES
}

func isAEADEncryption(algorithm ikev2.AlgorithmType) bool {
	switch algorithm {
	case ikev2.ENCR_AES_GCM_8, ikev2.ENCR_AES_GCM_12, ikev2.ENCR_AES_GCM_16,
		ikev2.ENCR_AES_CCM_8, ikev2.ENCR_AES_CCM_12, ikev2.ENCR_AES_CCM_16:
		return true
	default:
		return false
	}
}

func supportedByCryptoFactory(encryptionID uint16, keyLengthBits int) bool {
	_, err := crypto.GetEncrypterWithKeyLen(encryptionID, keyLengthBits)
	return err == nil
}

// AlgorithmPlan retains the explicit transform-ID API added by this module.
// Legacy proposal policy is resolved separately by buildAlgorithmPlan.
type AlgorithmPlan struct {
	IKEEncryption        uint16
	IKEEncryptionKeyBits uint16
	IKEPRF               uint16
	IKEIntegrity         uint16
	IKEDH                uint16
	ESPEncryption        uint16
	ESPEncryptionKeyBits uint16
	ESPIntegrity         uint16
}

func buildExplicitAlgorithmPlan(cfg *Config) *AlgorithmPlan {
	plan := &AlgorithmPlan{
		IKEEncryption: crypto.EncrAESCBC, IKEEncryptionKeyBits: 128,
		IKEPRF: uint16(ikev2.PRF_HMAC_SHA1), IKEIntegrity: uint16(ikev2.AUTH_HMAC_SHA1_96),
		IKEDH: uint16(ikev2.MODP_2048_bit), ESPEncryption: crypto.EncrAESCBC,
		ESPEncryptionKeyBits: 128, ESPIntegrity: uint16(ikev2.AUTH_HMAC_SHA1_96),
	}
	if cfg == nil {
		return plan
	}
	applyExplicitAlgorithmOverrides(plan, cfg)
	return plan
}

func applyExplicitAlgorithmOverrides(plan *AlgorithmPlan, cfg *Config) {
	if cfg.IKEEncryption != 0 {
		plan.IKEEncryption = cfg.IKEEncryption
	}
	if cfg.IKEEncryptionKeyBits != 0 {
		plan.IKEEncryptionKeyBits = cfg.IKEEncryptionKeyBits
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
	if cfg.ESPEncryptionKeyBits != 0 {
		plan.ESPEncryptionKeyBits = cfg.ESPEncryptionKeyBits
	}
	if cfg.ESPIntegrity != 0 {
		plan.ESPIntegrity = cfg.ESPIntegrity
	}
}
