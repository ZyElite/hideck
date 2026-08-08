package swu

import (
	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func filterIKEProposals(source []*ikev2.Proposal) ([]*ikev2.Proposal, []string) {
	proposals := make([]*ikev2.Proposal, 0, len(source))
	profiles := make([]string, 0, len(source))
	for index, proposal := range source {
		filtered, ok := filterIKEProposal(proposal)
		if !ok {
			continue
		}
		filtered.ProposalNum = uint8(len(proposals) + 1)
		proposals = append(proposals, filtered)
		profiles = append(profiles, summarizeIKEProposal(filtered, index+1))
	}
	return proposals, profiles
}

func filterIKEProposal(proposal *ikev2.Proposal) (*ikev2.Proposal, bool) {
	filtered := cloneProposal(proposal)
	if filtered == nil {
		return nil, false
	}
	transforms := make([]*ikev2.Transform, 0, len(filtered.Transforms))
	capabilities := ikeProposalCapabilities{}
	for _, transform := range filtered.Transforms {
		if supportedIKETransform(transform, &capabilities) {
			transforms = append(transforms, cloneTransform(transform))
		}
	}
	if !capabilities.valid() {
		return nil, false
	}
	filtered.Transforms = transforms
	return filtered, true
}

type ikeProposalCapabilities struct {
	encryption bool
	prf        bool
	integrity  bool
	dh         bool
	nonAEAD    bool
}

func (c ikeProposalCapabilities) valid() bool {
	return c.encryption && c.prf && c.dh && (!c.nonAEAD || c.integrity)
}

func supportedIKETransform(transform *ikev2.Transform, capabilities *ikeProposalCapabilities) bool {
	if transform == nil {
		return false
	}
	switch transform.Type {
	case ikev2.TransformTypeEncr:
		if !supportedEncryptionTransform(transform) {
			return false
		}
		capabilities.encryption = true
		capabilities.nonAEAD = capabilities.nonAEAD || !isAEADEncryption(transform.ID)
	case ikev2.TransformTypePRF:
		if _, err := crypto.GetPRF(uint16(transform.ID)); err != nil {
			return false
		}
		capabilities.prf = true
	case ikev2.TransformTypeInteg:
		if _, err := crypto.GetIntegrityAlgorithm(uint16(transform.ID)); err != nil {
			return false
		}
		capabilities.integrity = true
	case ikev2.TransformTypeDH:
		if _, err := crypto.NewDiffieHellman(uint16(transform.ID)); err != nil {
			return false
		}
		capabilities.dh = true
	}
	return true
}

func supportedEncryptionTransform(transform *ikev2.Transform) bool {
	keyBits := 0
	for _, attribute := range transform.Attributes {
		if attribute != nil && attribute.Type == ikev2.AttributeKeyLength {
			keyBits = int(attribute.Val)
			break
		}
	}
	return supportedByCryptoFactory(uint16(transform.ID), keyBits)
}

func filterESPProposal(proposal *ikev2.Proposal) (*ikev2.Proposal, bool) {
	filtered := cloneProposal(proposal)
	if filtered == nil {
		return nil, false
	}
	transforms := make([]*ikev2.Transform, 0, len(filtered.Transforms))
	hasEncryption, hasIntegrity, hasNonAEAD := false, false, false
	for _, transform := range filtered.Transforms {
		if transform == nil {
			continue
		}
		supported := true
		switch transform.Type {
		case ikev2.TransformTypeEncr:
			supported = supportedEncryptionTransform(transform)
			hasEncryption = hasEncryption || supported
			hasNonAEAD = hasNonAEAD || supported && !isAEADEncryption(transform.ID)
		case ikev2.TransformTypeInteg:
			_, err := crypto.GetIntegrityAlgorithm(uint16(transform.ID))
			supported = err == nil
			hasIntegrity = hasIntegrity || supported
		}
		if supported {
			transforms = append(transforms, cloneTransform(transform))
		}
	}
	if !hasEncryption || hasNonAEAD && !hasIntegrity {
		return nil, false
	}
	filtered.Transforms = transforms
	return filtered, true
}

func cloneProposal(proposal *ikev2.Proposal) *ikev2.Proposal {
	if proposal == nil {
		return nil
	}
	clone := *proposal
	clone.SPI = append([]byte(nil), proposal.SPI...)
	clone.Transforms = make([]*ikev2.Transform, 0, len(proposal.Transforms))
	for _, transform := range proposal.Transforms {
		clone.Transforms = append(clone.Transforms, cloneTransform(transform))
	}
	return &clone
}

func cloneTransform(transform *ikev2.Transform) *ikev2.Transform {
	if transform == nil {
		return nil
	}
	clone := *transform
	clone.Attributes = make([]*ikev2.TransformAttribute, 0, len(transform.Attributes))
	for _, attribute := range transform.Attributes {
		if attribute == nil {
			continue
		}
		attributeClone := *attribute
		attributeClone.Value = append([]byte(nil), attribute.Value...)
		clone.Attributes = append(clone.Attributes, &attributeClone)
	}
	return &clone
}
