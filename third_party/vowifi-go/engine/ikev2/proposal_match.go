package ikev2

import "fmt"

type ProposalMatcher struct {
	SupportedEncr  []AlgorithmType
	SupportedInteg []AlgorithmType
	SupportedPRF   []AlgorithmType
	SupportedDH    []AlgorithmType
}

type MatchedAlgorithms struct {
	ProposalNum uint8
	ProtocolID  ProtocolID
	SPI         []byte
	Encr        AlgorithmType
	EncrKeyLen  uint16
	Integ       AlgorithmType
	PRF         AlgorithmType
	DH          AlgorithmType
}

func DefaultProposalMatcher() *ProposalMatcher {
	return &ProposalMatcher{
		SupportedEncr: []AlgorithmType{
			ENCR_AES_GCM_16, ENCR_AES_GCM_12, ENCR_AES_GCM_8,
			ENCR_AES_CCM_16, ENCR_AES_CBC, ENCR_AES_CTR, ENCR_3DES,
		},
		SupportedInteg: []AlgorithmType{
			AUTH_NONE, AUTH_HMAC_SHA2_512_256, AUTH_HMAC_SHA2_384_192,
			AUTH_HMAC_SHA2_256_128, AUTH_AES_XCBC_96, AUTH_HMAC_SHA1_96,
		},
		SupportedPRF: []AlgorithmType{
			PRF_HMAC_SHA2_512, PRF_HMAC_SHA2_384, PRF_HMAC_SHA2_256,
			PRF_AES128_XCBC, PRF_HMAC_SHA1,
		},
		SupportedDH: []AlgorithmType{
			ECP_384_bit, ECP_256_bit, MODP_4096_bit, MODP_3072_bit,
			MODP_2048_bit, MODP_1536_bit, MODP_1024_bit,
		},
	}
}

func (m *ProposalMatcher) SelectBestProposal(sa *EncryptedPayloadSA) (*MatchedAlgorithms, error) {
	if sa == nil {
		return nil, fmt.Errorf("SA payload is nil")
	}
	for _, proposal := range sa.Proposals {
		if matched := m.matchProposal(proposal); matched != nil {
			return matched, nil
		}
	}
	return nil, nil
}

func (m *ProposalMatcher) matchProposal(proposal *Proposal) *MatchedAlgorithms {
	if proposal == nil {
		return nil
	}
	encryption := selectTransform(proposal.Transforms, TransformTypeEncr, m.SupportedEncr)
	if encryption == nil {
		return nil
	}
	result := &MatchedAlgorithms{
		ProposalNum: proposal.ProposalNum, ProtocolID: proposal.ProtocolID,
		SPI: proposal.SPI, Encr: encryption.ID,
	}
	for _, attribute := range encryption.Attributes {
		if attribute.Type == AttributeKeyLength {
			result.EncrKeyLen = attribute.Val
		}
	}
	integrity := selectTransform(proposal.Transforms, TransformTypeInteg, m.SupportedInteg)
	if integrity != nil {
		result.Integ = integrity.ID
	}
	if proposal.ProtocolID == ProtoESP {
		if isAEAD(result.Encr) || integrity != nil {
			return result
		}
		return nil
	}
	if proposal.ProtocolID != ProtoIKE {
		return nil
	}
	prf := selectTransform(proposal.Transforms, TransformTypePRF, m.SupportedPRF)
	dh := selectTransform(proposal.Transforms, TransformTypeDH, m.SupportedDH)
	if prf == nil || dh == nil || (!isAEAD(result.Encr) && integrity == nil) {
		return nil
	}
	result.PRF = prf.ID
	result.DH = dh.ID
	return result
}

func selectTransform(transforms []*Transform, transformType TransformType, priorities []AlgorithmType) *Transform {
	for _, preferred := range priorities {
		for _, transform := range transforms {
			if transform == nil {
				continue
			}
			transform.syncOriginalFields()
			if transform.Type == transformType && transform.ID == preferred {
				return transform
			}
		}
	}
	return nil
}

func isAEAD(encryption AlgorithmType) bool {
	switch encryption {
	case ENCR_AES_GCM_8, ENCR_AES_GCM_12, ENCR_AES_GCM_16,
		ENCR_AES_CCM_8, ENCR_AES_CCM_12, ENCR_AES_CCM_16:
		return true
	default:
		return false
	}
}

// CreateMultiProposalIKE accepts the original SPI form and the interim
// four-algorithm form retained for source compatibility.
func CreateMultiProposalIKE(arguments ...any) []*Proposal {
	if len(arguments) == 1 {
		spi, ok := arguments[0].([]byte)
		if !ok {
			panic("CreateMultiProposalIKE: expected []byte SPI")
		}
		return createOriginalIKEProposals(spi)
	}
	if len(arguments) == 4 {
		return CreateIKEProposals(IKEProposalAlgorithms{
			Encryption: algorithmArgument(arguments[0]), PRF: algorithmArgument(arguments[1]),
			Integrity: algorithmArgument(arguments[2]), DH: algorithmArgument(arguments[3]),
			EncryptionKeyBits: 128,
		})
	}
	panic("CreateMultiProposalIKE: expected SPI or four algorithm IDs")
}

func createOriginalIKEProposals(spi []byte) []*Proposal {
	strict := NewProposal(1, ProtoIKE, spi)
	strict.AddTransformWithKeyLen(TransformTypeEncr, ENCR_AES_CBC, 128)
	strict.AddTransform(TransformTypeInteg, AUTH_HMAC_SHA2_256_128)
	strict.AddTransform(TransformTypePRF, PRF_HMAC_SHA2_256)
	strict.AddTransform(TransformTypeDH, MODP_2048_bit)
	return []*Proposal{
		strict,
		createCompatibleIKEProposal(2, spi, AUTH_HMAC_SHA2_256_128, PRF_HMAC_SHA2_256),
		createCompatibleIKEProposal(3, spi, AUTH_HMAC_SHA1_96, PRF_HMAC_SHA1),
		createCompatibleIKEProposal(4, spi, AUTH_AES_XCBC_96, PRF_AES128_XCBC),
	}
}

func createCompatibleIKEProposal(number uint8, spi []byte, integrity, prf AlgorithmType) *Proposal {
	proposal := NewProposal(number, ProtoIKE, spi)
	proposal.AddTransformWithKeyLen(TransformTypeEncr, ENCR_AES_CBC, 128)
	proposal.AddTransformWithKeyLen(TransformTypeEncr, ENCR_AES_CBC, 256)
	proposal.AddTransform(TransformTypeInteg, integrity)
	proposal.AddTransform(TransformTypePRF, prf)
	for _, dh := range []AlgorithmType{
		MODP_1024_bit, MODP_1536_bit, MODP_2048_bit, MODP_3072_bit, ECP_256_bit, ECP_384_bit,
	} {
		proposal.AddTransform(TransformTypeDH, dh)
	}
	return proposal
}

// CreateMultiProposalESP accepts the original SPI form and the interim
// four-algorithm form retained for source compatibility.
func CreateMultiProposalESP(arguments ...any) []*Proposal {
	if len(arguments) == 1 {
		spi, ok := arguments[0].([]byte)
		if !ok {
			panic("CreateMultiProposalESP: expected []byte SPI")
		}
		return createOriginalESPProposals(spi)
	}
	if len(arguments) == 4 {
		return CreateESPProposals(ESPProposalAlgorithms{
			Encryption: algorithmArgument(arguments[0]), Integrity: algorithmArgument(arguments[1]),
			DH: algorithmArgument(arguments[2]), ESN: algorithmArgument(arguments[3]),
			EncryptionKeyBits: 128,
		})
	}
	panic("CreateMultiProposalESP: expected SPI or four algorithm IDs")
}

func createOriginalESPProposals(spi []byte) []*Proposal {
	return []*Proposal{
		newESPProposal(1, spi, ENCR_AES_GCM_16, 256, AUTH_NONE),
		newESPProposal(2, spi, ENCR_AES_GCM_16, 128, AUTH_NONE),
		newESPProposal(3, spi, ENCR_AES_CBC, 128, AUTH_HMAC_SHA2_256_128),
		newESPProposal(4, spi, ENCR_AES_CBC, 128, AUTH_HMAC_SHA1_96),
	}
}

func newESPProposal(number uint8, spi []byte, encryption AlgorithmType, bits int, integrity AlgorithmType) *Proposal {
	proposal := NewProposal(number, ProtoESP, spi)
	proposal.AddTransformWithKeyLen(TransformTypeEncr, encryption, bits)
	if integrity != AUTH_NONE {
		proposal.AddTransform(TransformTypeInteg, integrity)
	}
	proposal.AddTransform(TransformTypeESN, 0)
	return proposal
}

type IKEProposalAlgorithms struct {
	Encryption        uint16
	EncryptionKeyBits uint16
	PRF               uint16
	Integrity         uint16
	DH                uint16
}

func CreateIKEProposals(algorithms IKEProposalAlgorithms) []*Proposal {
	proposal := NewProposal(1, ProtoIKE, nil)
	addEncryptionTransform(proposal, algorithms.Encryption, algorithms.EncryptionKeyBits)
	proposal.AddTransform(TransformTypePRF, AlgorithmType(algorithms.PRF))
	proposal.AddTransform(TransformTypeInteg, AlgorithmType(algorithms.Integrity))
	proposal.AddTransform(TransformTypeDH, AlgorithmType(algorithms.DH))
	return []*Proposal{proposal}
}

type ESPProposalAlgorithms struct {
	Encryption        uint16
	EncryptionKeyBits uint16
	Integrity         uint16
	DH                uint16
	ESN               uint16
}

func CreateESPProposals(algorithms ESPProposalAlgorithms) []*Proposal {
	proposal := NewProposal(1, ProtoESP, nil)
	addEncryptionTransform(proposal, algorithms.Encryption, algorithms.EncryptionKeyBits)
	if algorithms.Integrity != 0 {
		proposal.AddTransform(TransformTypeInteg, AlgorithmType(algorithms.Integrity))
	}
	if algorithms.DH != 0 {
		proposal.AddTransform(TransformTypeDH, AlgorithmType(algorithms.DH))
	}
	proposal.AddTransform(TransformTypeESN, AlgorithmType(algorithms.ESN))
	return []*Proposal{proposal}
}

func addEncryptionTransform(proposal *Proposal, transformID, keyLengthBits uint16) {
	switch AlgorithmType(transformID) {
	case ENCR_AES_CBC, ENCR_AES_GCM_8, ENCR_AES_GCM_12, ENCR_AES_GCM_16:
		if keyLengthBits == 0 {
			keyLengthBits = 128
		}
		proposal.AddTransformWithKeyLen(TransformTypeEncr, AlgorithmType(transformID), int(keyLengthBits))
	default:
		proposal.AddTransform(TransformTypeEncr, AlgorithmType(transformID))
	}
}

func algorithmArgument(argument any) uint16 {
	switch value := argument.(type) {
	case uint16:
		return value
	case AlgorithmType:
		return uint16(value)
	case int:
		if value >= 0 && value <= maxPayloadLength {
			return uint16(value)
		}
	}
	panic(fmt.Sprintf("algorithm ID has unsupported type/value %T", argument))
}
