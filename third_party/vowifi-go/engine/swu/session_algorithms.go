package swu

import (
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

type encryptionParameters struct {
	keyLen int
	aead   bool
}

func initializeSessionAlgorithms(s *Session, cfg *Config) error {
	mode := ""
	if cfg != nil {
		mode = cfg.DataplaneMode
	}
	normalizedMode, err := normalizeDataplaneMode(mode)
	if err != nil {
		return err
	}
	if err := validatePlatformDataplaneMode(normalizedMode); err != nil {
		return err
	}
	plan, err := initialAlgorithmPlan(cfg)
	if err != nil {
		return err
	}
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
	if err := validateDriverAlgorithms(plan, espEncryption.aead, cfg); err != nil {
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

func initialAlgorithmPlan(cfg *Config) (*AlgorithmPlan, error) {
	if hasExplicitIKEAlgorithms(cfg) || hasExplicitESPAlgorithms(cfg) {
		return buildExplicitAlgorithmPlan(cfg), nil
	}
	ikeProposals, _, _, err := buildIKEProposals(cfg, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("build IKE proposals: %w", err)
	}
	espProposals, err := buildESPProposals(cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("build ESP proposals: %w", err)
	}
	plan, err := algorithmPlanFromProposals(ikeProposals[0], espProposals[0])
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func algorithmPlanFromProposals(ikeProposal, espProposal *ikev2.Proposal) (*AlgorithmPlan, error) {
	ikeSelection, err := firstIKEAlgorithmSelection(ikeProposal)
	if err != nil {
		return nil, fmt.Errorf("select initial IKE algorithms: %w", err)
	}
	espSelection, err := firstESPAlgorithmSelection(espProposal)
	if err != nil {
		return nil, fmt.Errorf("select initial ESP algorithms: %w", err)
	}
	return &AlgorithmPlan{
		IKEEncryption: ikeSelection.encryption, IKEEncryptionKeyBits: ikeSelection.keyBits,
		IKEPRF: ikeSelection.prf, IKEIntegrity: ikeSelection.integrity, IKEDH: ikeSelection.dh,
		ESPEncryption: espSelection.encryption, ESPEncryptionKeyBits: espSelection.keyBits,
		ESPIntegrity: espSelection.integrity,
	}, nil
}

type selectedAlgorithms struct {
	encryption uint16
	keyBits    uint16
	integrity  uint16
	prf        uint16
	dh         uint16
}

func firstIKEAlgorithmSelection(proposal *ikev2.Proposal) (selectedAlgorithms, error) {
	selection := firstAlgorithmSelection(proposal)
	if selection.encryption == 0 || selection.prf == 0 || selection.dh == 0 {
		return selectedAlgorithms{}, fmt.Errorf("incomplete IKE proposal")
	}
	if !isAEADEncryption(ikev2.AlgorithmType(selection.encryption)) && selection.integrity == 0 {
		return selectedAlgorithms{}, fmt.Errorf("non-AEAD IKE proposal has no integrity transform")
	}
	return selection, nil
}

func firstESPAlgorithmSelection(proposal *ikev2.Proposal) (selectedAlgorithms, error) {
	selection := firstAlgorithmSelection(proposal)
	if selection.encryption == 0 {
		return selectedAlgorithms{}, fmt.Errorf("incomplete ESP proposal")
	}
	if !isAEADEncryption(ikev2.AlgorithmType(selection.encryption)) && selection.integrity == 0 {
		return selectedAlgorithms{}, fmt.Errorf("non-AEAD ESP proposal has no integrity transform")
	}
	return selection, nil
}

func firstAlgorithmSelection(proposal *ikev2.Proposal) selectedAlgorithms {
	selection := selectedAlgorithms{}
	if proposal == nil {
		return selection
	}
	for _, transform := range proposal.Transforms {
		if transform == nil {
			continue
		}
		applyFirstTransform(&selection, transform)
	}
	return selection
}

func applyFirstTransform(selection *selectedAlgorithms, transform *ikev2.Transform) {
	switch transform.Type {
	case ikev2.TransformTypeEncr:
		if selection.encryption == 0 {
			selection.encryption = uint16(transform.ID)
			selection.keyBits = encryptionKeyBits(transform)
		}
	case ikev2.TransformTypeInteg:
		if selection.integrity == 0 {
			selection.integrity = uint16(transform.ID)
		}
	case ikev2.TransformTypePRF:
		if selection.prf == 0 {
			selection.prf = uint16(transform.ID)
		}
	case ikev2.TransformTypeDH:
		if selection.dh == 0 {
			selection.dh = uint16(transform.ID)
		}
	}
}

func encryptionKeyBits(transform *ikev2.Transform) uint16 {
	for _, attribute := range transform.Attributes {
		if attribute != nil && attribute.Type == ikev2.AttributeKeyLength {
			return attribute.Val
		}
	}
	return 0
}

func validateDriverAlgorithms(plan *AlgorithmPlan, isAEAD bool, cfg *Config) error {
	if driver.IsAEADAlgorithm(plan.ESPEncryption) != isAEAD {
		return fmt.Errorf("ESP transform %d has inconsistent AEAD classification", plan.ESPEncryption)
	}
	mode := ""
	if cfg != nil {
		mode = cfg.DataplaneMode
	}
	normalizedMode, err := normalizeDataplaneMode(mode)
	if err != nil || normalizedMode != DataplaneModeXFRMI {
		return err
	}
	if isAEAD {
		if _, err = driver.IKEv2AlgToXFRMAead(plan.ESPEncryption, int(plan.ESPEncryptionKeyBits)); err != nil {
			return fmt.Errorf("ESP XFRM algorithm: %w", err)
		}
		return nil
	}
	if _, err := driver.IKEv2AlgToXFRMCrypt(plan.ESPEncryption, int(plan.ESPEncryptionKeyBits)); err != nil {
		return fmt.Errorf("ESP XFRM encryption algorithm: %w", err)
	}
	if _, err := driver.IKEv2AlgToXFRMAuth(plan.ESPIntegrity); err != nil {
		return fmt.Errorf("ESP XFRM integrity algorithm: %w", err)
	}
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
		if keyLengthBits != 128 && keyLengthBits != 192 && keyLengthBits != 256 {
			return params, fmt.Errorf("AES-GCM key length must be 128, 192, or 256 bits")
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
