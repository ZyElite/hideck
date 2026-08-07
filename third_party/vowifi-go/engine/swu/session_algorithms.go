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
	ikeEncryption, err := supportedEncryption(plan.IKEEncryption)
	if err != nil {
		return fmt.Errorf("IKE encryption: %w", err)
	}
	espEncryption, err := supportedEncryption(plan.ESPEncryption)
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
	s.integAlg, s.dhGroup = plan.IKEIntegrity, plan.IKEDH
	s.espCipher, s.espInteg = plan.ESPEncryption, plan.ESPIntegrity
	s.prf, s.dh = prf, dh
	s.encKeyLen, s.integKeyLen, s.aead = ikeEncryption.keyLen, ikeIntegrity.KeySize(), ikeEncryption.aead
	s.espEncKeyLen, s.espIntegKeyLen, s.espAEAD = espEncryption.keyLen, espIntegrity.KeySize(), espEncryption.aead
	return nil
}

func supportedEncryption(transformID uint16) (encryptionParameters, error) {
	var params encryptionParameters
	switch transformID {
	case crypto.EncrNull:
	case crypto.EncrAESCBC:
		params.keyLen = 16
	case crypto.Encr3DESCBC:
		params.keyLen = 24
	case crypto.EncrAESGCM16:
		params.keyLen = 20
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
