package swu

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func configuredIKEProposalSummary(configured []string) []string {
	if len(configured) == 0 {
		return []string{"default-multi-proposal"}
	}
	return append([]string(nil), configured...)
}

func configuredESPProposalSummary(configured []string) []string {
	if len(configured) == 0 {
		return []string{"default-multi-proposal"}
	}
	return append([]string(nil), configured...)
}

func buildIKEProposals(
	cfg *Config,
	spi []byte,
	profileOffset int,
) ([]*ikev2.Proposal, []string, []string, error) {
	plan := buildAlgorithmPlan(cfg)
	source, err := configuredIKEProposals(cfg, spi, plan)
	if err != nil {
		return nil, nil, nil, err
	}
	if profileOffset > 0 && profileOffset < len(source) {
		source = source[profileOffset:]
	}
	if len(source) == 0 {
		return nil, nil, nil, fmt.Errorf("无可用 IKE 提议(profile_offset=%d)", profileOffset)
	}
	proposals, profiles := filterIKEProposals(source)
	if len(proposals) == 0 {
		return nil, nil, nil, fmt.Errorf("IKE 提议经过能力过滤后为空")
	}
	return proposals, profiles, plan.effectiveAlgSetLabel(), nil
}

func configuredIKEProposals(cfg *Config, spi []byte, plan algorithmPlan) ([]*ikev2.Proposal, error) {
	if len(cfg.IKEProposals) == 0 {
		if hasExplicitIKEAlgorithms(cfg) {
			return explicitIKEProposals(cfg, spi), nil
		}
		return ikev2.CreateMultiProposalIKE(spi), nil
	}
	result := make([]*ikev2.Proposal, 0, len(cfg.IKEProposals))
	for index, raw := range cfg.IKEProposals {
		proposal, err := parseIKEProposal(raw, uint8(index+1), spi, plan)
		if err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	return result, nil
}

func buildESPProposals(cfg *Config, spi []byte) ([]*ikev2.Proposal, error) {
	plan := buildAlgorithmPlan(cfg)
	source, err := configuredESPProposals(cfg, spi, plan)
	if err != nil {
		return nil, err
	}
	result := make([]*ikev2.Proposal, 0, len(source))
	for _, proposal := range source {
		if filtered, ok := filterESPProposal(proposal); ok {
			result = append(result, filtered)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("ESP 提议经过能力过滤后为空")
	}
	return result, nil
}

func configuredESPProposals(cfg *Config, spi []byte, plan algorithmPlan) ([]*ikev2.Proposal, error) {
	if len(cfg.ESPProposals) == 0 {
		if hasExplicitESPAlgorithms(cfg) {
			return explicitESPProposals(cfg, spi), nil
		}
		return ikev2.CreateMultiProposalESP(spi), nil
	}
	result := make([]*ikev2.Proposal, 0, len(cfg.ESPProposals))
	for index, raw := range cfg.ESPProposals {
		proposal, err := parseESPProposal(raw, uint8(index+1), spi, plan)
		if err != nil {
			return nil, err
		}
		result = append(result, proposal)
	}
	return result, nil
}

func hasExplicitIKEAlgorithms(cfg *Config) bool {
	return cfg.IKEEncryption != 0 || cfg.IKEPRF != 0 || cfg.IKEIntegrity != 0 || cfg.IKEDH != 0
}

func hasExplicitESPAlgorithms(cfg *Config) bool {
	return cfg.ESPEncryption != 0 || cfg.ESPIntegrity != 0
}

func explicitIKEProposals(cfg *Config, spi []byte) []*ikev2.Proposal {
	plan := buildExplicitAlgorithmPlan(cfg)
	proposals := ikev2.CreateIKEProposals(ikev2.IKEProposalAlgorithms{
		Encryption: plan.IKEEncryption, EncryptionKeyBits: plan.IKEEncryptionKeyBits,
		PRF: plan.IKEPRF, Integrity: plan.IKEIntegrity, DH: plan.IKEDH,
	})
	proposals[0].SPI = append([]byte(nil), spi...)
	return proposals
}

func explicitESPProposals(cfg *Config, spi []byte) []*ikev2.Proposal {
	plan := buildExplicitAlgorithmPlan(cfg)
	proposals := ikev2.CreateESPProposals(ikev2.ESPProposalAlgorithms{
		Encryption: plan.ESPEncryption, EncryptionKeyBits: plan.ESPEncryptionKeyBits,
		Integrity: plan.ESPIntegrity,
	})
	proposals[0].SPI = append([]byte(nil), spi...)
	return proposals
}

func summarizeIKEProposal(proposal *ikev2.Proposal, index int) string {
	algorithms := firstProposalAlgorithmNames(proposal)
	return fmt.Sprintf("p%d:%s-%s-%s-%s", index, algorithms.encryption,
		algorithms.integrity, algorithms.prf, algorithms.dh)
}

type proposalAlgorithmNames struct {
	encryption string
	integrity  string
	prf        string
	dh         string
}

func firstProposalAlgorithmNames(proposal *ikev2.Proposal) proposalAlgorithmNames {
	result := proposalAlgorithmNames{}
	for _, transform := range proposal.Transforms {
		switch transform.Type {
		case ikev2.TransformTypeEncr:
			if result.encryption == "" {
				result.encryption = ikev2.EncrToString(uint16(transform.ID))
			}
		case ikev2.TransformTypeInteg:
			if result.integrity == "" {
				result.integrity = ikev2.IntegToString(uint16(transform.ID))
			}
		case ikev2.TransformTypePRF:
			if result.prf == "" {
				result.prf = ikev2.PRFToString(uint16(transform.ID))
			}
		case ikev2.TransformTypeDH:
			if result.dh == "" {
				result.dh = ikev2.DHToString(uint16(transform.ID))
			}
		}
	}
	result.fillDefaults()
	return result
}

func (names *proposalAlgorithmNames) fillDefaults() {
	if names.encryption == "" {
		names.encryption = "UNKNOWN"
	}
	if names.integrity == "" {
		names.integrity = "AEAD"
	}
	if names.prf == "" {
		names.prf = "UNKNOWN"
	}
	if names.dh == "" {
		names.dh = "UNKNOWN"
	}
	names.encryption = strings.ToLower(names.encryption)
	names.integrity = strings.ToLower(names.integrity)
	names.prf = strings.ToLower(names.prf)
	names.dh = strings.ToLower(names.dh)
}

func firstDHGroupFromProposals(proposals []*ikev2.Proposal) ikev2.AlgorithmType {
	for _, proposal := range proposals {
		for _, transform := range proposal.Transforms {
			if transform.Type == ikev2.TransformTypeDH {
				return transform.ID
			}
		}
	}
	return ikev2.MODP_2048_bit
}

// buildIKEProposalsForSession builds the single already-negotiated proposal
// used by IKE SA rekey. Initial negotiation uses buildIKEProposals instead.
func buildIKEProposalsForSession(session *Session) []*ikev2.Proposal {
	return ikev2.CreateIKEProposals(ikev2.IKEProposalAlgorithms{
		Encryption: session.encrAlg, EncryptionKeyBits: session.encKeyBits,
		PRF: session.prfAlg, Integrity: session.integAlg, DH: session.dhGroup,
	})
}

// buildESPProposalsForSession builds the single already-negotiated proposal
// used by CHILD_SA rekey responses.
func buildESPProposalsForSession(session *Session, spi uint32) []*ikev2.Proposal {
	proposals := ikev2.CreateESPProposals(ikev2.ESPProposalAlgorithms{
		Encryption: session.espCipher, EncryptionKeyBits: session.espEncKeyBits,
		Integrity: session.espInteg,
	})
	if spi != 0 {
		proposals[0].SPI = spiBytes(spi)
	}
	return proposals
}
