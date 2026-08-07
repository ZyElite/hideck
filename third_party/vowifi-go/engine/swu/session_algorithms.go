package swu

import (
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

type encryptionParameters struct {
	keyLen int
	aead   bool
}

func initializeSessionAlgorithms(s *Session, cfg *Config) error {
	plan := buildAlgorithmPlan(cfg.AlgorithmPolicy, cfg)
	ikeEncryption, err := supportedEncryption(plan.IKEEncryption, plan.IKEEncryptionKeyBits)
	if err != nil {
		return fmt.Errorf("IKE encryption: %w", err)
	}
	espEncryption, err := supportedEncryption(plan.ESPEncryption, plan.ESPEncryptionKeyBits)
	if err != nil {
		return fmt.Errorf("ESP encryption: %w", err)
	}
	prf := crypto.NewPRF(plan.IKEPRF)
	if prf == nil {
		return fmt.Errorf("unsupported IKE PRF transform %d", plan.IKEPRF)
	}
	ikeIntegrity := crypto.NewIntegrity(plan.IKEIntegrity)
	if ikeIntegrity == nil {
		return fmt.Errorf("unsupported IKE integrity transform %d", plan.IKEIntegrity)
	}
	espIntegrity := crypto.NewIntegrity(plan.ESPIntegrity)
	if espIntegrity == nil {
		return fmt.Errorf("unsupported ESP integrity transform %d", plan.ESPIntegrity)
	}
	if err := validateIntegrityMode("IKE", ikeEncryption.aead, plan.IKEIntegrity); err != nil {
		return err
	}
	if err := validateIntegrityMode("ESP", espEncryption.aead, plan.ESPIntegrity); err != nil {
		return err
	}
	dh, err := crypto.NewDiffieHellman(plan.IKEDH)
	if err != nil {
		return fmt.Errorf("IKE DH: %w", err)
	}

	s.encrAlg, s.prfAlg = plan.IKEEncryption, plan.IKEPRF
	s.encKeyBits = plan.IKEEncryptionKeyBits
	s.integAlg, s.dhGroup = plan.IKEIntegrity, plan.IKEDH
	s.espCipher, s.espInteg = plan.ESPEncryption, plan.ESPIntegrity
	s.espEncKeyBits = plan.ESPEncryptionKeyBits
	s.prf, s.dh = prf, dh
	s.encKeyLen, s.integKeyLen, s.aead = ikeEncryption.keyLen, ikeIntegrity.KeySize(), ikeEncryption.aead
	s.espEncKeyLen, s.espIntegKeyLen, s.espAEAD = espEncryption.keyLen, espIntegrity.KeySize(), espEncryption.aead
	return nil
}

func supportedEncryption(transformID, keyLengthBits uint16) (encryptionParameters, error) {
	var params encryptionParameters
	switch transformID {
	case crypto.EncrNull:
	case crypto.EncrAESCBC:
		if keyLengthBits != 128 && keyLengthBits != 192 && keyLengthBits != 256 {
			return params, fmt.Errorf("AES-CBC key length must be 128, 192, or 256 bits")
		}
		params.keyLen = int(keyLengthBits / 8)
	case crypto.Encr3DESCBC:
		if keyLengthBits != 0 {
			return params, fmt.Errorf("3DES does not accept an AES key length")
		}
		params.keyLen = 24
	case crypto.EncrAESGCM16:
		if keyLengthBits != 128 {
			return params, fmt.Errorf("AES-GCM currently supports only 128-bit keys")
		}
		params.keyLen = int(keyLengthBits/8) + 4
		params.aead = true
	case crypto.EncrAESGCM12, crypto.EncrAESGCM8:
		return params, fmt.Errorf("transform %d requires an unsupported non-16-byte GCM tag", transformID)
	default:
		return params, fmt.Errorf("unsupported transform %d", transformID)
	}
	if _, err := crypto.PrepareCipher(transformID, make([]byte, params.keyLen)); err != nil {
		return params, err
	}
	return params, nil
}

func validateIntegrityMode(layer string, aead bool, integrity uint16) error {
	if aead && integrity != 0 {
		return fmt.Errorf("%s AEAD transform requires integrity NONE", layer)
	}
	if !aead && integrity == 0 {
		return fmt.Errorf("%s non-AEAD transform requires an integrity algorithm", layer)
	}
	return nil
}
