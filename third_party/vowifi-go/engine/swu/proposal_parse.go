package swu

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

type encrSpec struct {
	alg    ikev2.AlgorithmType
	keyLen int
	aead   bool
}

func parseIKEProposal(
	raw string,
	number uint8,
	spi []byte,
	plan algorithmPlan,
) (*ikev2.Proposal, error) {
	normalized := normalizeProposal(raw)
	if normalized == "" {
		return nil, fmt.Errorf("empty IKE proposal")
	}
	parts := strings.Split(normalized, "-")
	if len(parts) != 3 && len(parts) != 4 {
		return nil, fmt.Errorf("invalid IKE proposal %q, expected 3 or 4 parts", raw)
	}
	encryption, err := parseEncr(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid IKE proposal %q: %w", raw, err)
	}
	if !plan.allowsEncryption(encryption.alg) {
		return nil, fmt.Errorf("invalid IKE proposal %q: encryption %s rejected by policy",
			raw, ikev2.EncrToString(uint16(encryption.alg)))
	}
	proposal := ikev2.NewProposal(number, ikev2.ProtoIKE, spi)
	proposal.AddTransformWithKeyLen(ikev2.TransformTypeEncr, encryption.alg, encryption.keyLen)
	integrity, prf, dh, err := parseIKEAlgorithms(parts[1:], encryption.aead)
	if err != nil {
		return nil, fmt.Errorf("invalid IKE proposal %q: %w", raw, err)
	}
	if !encryption.aead {
		proposal.AddTransform(ikev2.TransformTypeInteg, integrity, 0)
	}
	proposal.AddTransform(ikev2.TransformTypePRF, prf, 0)
	proposal.AddTransform(ikev2.TransformTypeDH, dh, 0)
	return proposal, nil
}

func parseIKEAlgorithms(
	parts []string,
	aead bool,
) (ikev2.AlgorithmType, ikev2.AlgorithmType, ikev2.AlgorithmType, error) {
	if aead {
		prf, err := parsePRF(parts[0])
		if err != nil {
			return 0, 0, 0, err
		}
		dh, err := parseDH(parts[1])
		return 0, prf, dh, err
	}
	integrity, err := parseInteg(parts[0])
	if err != nil {
		return 0, 0, 0, err
	}
	prf := integToPRF(integrity)
	dhIndex := 1
	if len(parts) == 3 {
		prf, err = parsePRF(parts[1])
		if err != nil {
			return 0, 0, 0, err
		}
		dhIndex = 2
	}
	dh, err := parseDH(parts[dhIndex])
	return integrity, prf, dh, err
}

func parseESPProposal(
	raw string,
	number uint8,
	spi []byte,
	plan algorithmPlan,
) (*ikev2.Proposal, error) {
	normalized := normalizeProposal(raw)
	if normalized == "" {
		return nil, fmt.Errorf("empty ESP proposal")
	}
	parts := strings.Split(normalized, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return nil, fmt.Errorf("invalid ESP proposal %q, expected 1 or 2 parts", raw)
	}
	encryption, err := parseEncr(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid ESP proposal %q: %w", raw, err)
	}
	if !plan.allowsEncryption(encryption.alg) {
		return nil, fmt.Errorf("invalid ESP proposal %q: encryption %s rejected by policy",
			raw, ikev2.EncrToString(uint16(encryption.alg)))
	}
	proposal := ikev2.NewProposal(number, ikev2.ProtoESP, spi)
	proposal.AddTransformWithKeyLen(ikev2.TransformTypeEncr, encryption.alg, encryption.keyLen)
	if !encryption.aead {
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid ESP proposal %q: missing integrity algorithm", raw)
		}
		integrity, err := parseInteg(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid ESP proposal %q: %w", raw, err)
		}
		proposal.AddTransform(ikev2.TransformTypeInteg, integrity, 0)
	}
	proposal.AddTransform(ikev2.TransformTypeESN, 0, 0)
	return proposal, nil
}

func normalizeProposal(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

func parseEncr(value string) (encrSpec, error) {
	switch value {
	case "aes128":
		return encrSpec{alg: ikev2.ENCR_AES_CBC, keyLen: 128}, nil
	case "aes256":
		return encrSpec{alg: ikev2.ENCR_AES_CBC, keyLen: 256}, nil
	case "3des":
		return encrSpec{alg: ikev2.ENCR_3DES}, nil
	case "des":
		return encrSpec{alg: ikev2.ENCR_DES}, nil
	case "aes128gcm16":
		return encrSpec{alg: ikev2.ENCR_AES_GCM_16, keyLen: 128, aead: true}, nil
	case "aes256gcm16":
		return encrSpec{alg: ikev2.ENCR_AES_GCM_16, keyLen: 256, aead: true}, nil
	default:
		return encrSpec{}, fmt.Errorf("unsupported encryption %q", value)
	}
}

func parseInteg(value string) (ikev2.AlgorithmType, error) {
	switch value {
	case "sha1":
		return ikev2.AUTH_HMAC_SHA1_96, nil
	case "sha256":
		return ikev2.AUTH_HMAC_SHA2_256_128, nil
	case "sha384":
		return ikev2.AUTH_HMAC_SHA2_384_192, nil
	case "sha512":
		return ikev2.AUTH_HMAC_SHA2_512_256, nil
	default:
		return 0, fmt.Errorf("unsupported integrity %q", value)
	}
}

func parsePRF(value string) (ikev2.AlgorithmType, error) {
	switch strings.TrimPrefix(value, "prf") {
	case "sha1":
		return ikev2.PRF_HMAC_SHA1, nil
	case "sha256":
		return ikev2.PRF_HMAC_SHA2_256, nil
	case "sha384":
		return ikev2.PRF_HMAC_SHA2_384, nil
	case "sha512":
		return ikev2.PRF_HMAC_SHA2_512, nil
	default:
		return 0, fmt.Errorf("unsupported prf %q", value)
	}
}

func parseDH(value string) (ikev2.AlgorithmType, error) {
	switch value {
	case "modp1024":
		return ikev2.MODP_1024_bit, nil
	case "modp1536":
		return ikev2.MODP_1536_bit, nil
	case "modp2048":
		return ikev2.MODP_2048_bit, nil
	case "modp3072":
		return ikev2.MODP_3072_bit, nil
	case "modp4096":
		return ikev2.MODP_4096_bit, nil
	case "ecp256":
		return ikev2.ECP_256_bit, nil
	case "ecp384":
		return ikev2.ECP_384_bit, nil
	default:
		return 0, fmt.Errorf("unsupported dh group %q", value)
	}
}

func integToPRF(integrity ikev2.AlgorithmType) ikev2.AlgorithmType {
	switch integrity {
	case ikev2.AUTH_HMAC_SHA1_96:
		return ikev2.PRF_HMAC_SHA1
	case ikev2.AUTH_HMAC_SHA2_384_192:
		return ikev2.PRF_HMAC_SHA2_384
	case ikev2.AUTH_HMAC_SHA2_512_256:
		return ikev2.PRF_HMAC_SHA2_512
	default:
		return ikev2.PRF_HMAC_SHA2_256
	}
}
