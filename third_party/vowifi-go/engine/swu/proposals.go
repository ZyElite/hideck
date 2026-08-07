package swu

import (
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// IKEv2 transform IDs (RFC 7296 §3.3.2) used by the proposal helpers.
const (
	transformEncryption uint16 = 1
	transformPRF        uint16 = 2
	transformIntegrity  uint16 = 3
	transformDH         uint16 = 4
	transformESN        uint16 = 5
)

// buildESPProposals builds the ESP proposal list for the CHILD_SA (RFC 7296
// §3.3.2). Zero transform IDs select the default (AES-CBC + HMAC-SHA1).
func buildESPProposals(encr, integ uint16, spi ...uint32) []*ikev2.Proposal {
	if encr == 0 {
		encr = 12 // ENCR_AES_CBC
	}
	if integ == 0 && encr != crypto.EncrAESGCM16 {
		integ = 2 // AUTH_HMAC_SHA1_96
	}
	proposals := ikev2.CreateMultiProposalESP(encr, integ, 0, 0)
	if len(spi) > 0 && spi[0] != 0 {
		proposals[0].SPI = spiBytes(spi[0])
		proposals[0].SPISize = 4
	}
	return proposals
}

// parseEncr extracts the encryption transform ID from a proposal.
func parseEncr(p *ikev2.Proposal) (uint16, error) {
	for _, t := range p.Transforms {
		if t.TransformType == byte(transformEncryption) {
			return t.TransformID, nil
		}
	}
	return 0, errors.New("swu: proposal missing encryption transform")
}

// parseInteg extracts the integrity transform ID from a proposal.
func parseInteg(p *ikev2.Proposal) (uint16, error) {
	for _, t := range p.Transforms {
		if t.TransformType == byte(transformIntegrity) {
			return t.TransformID, nil
		}
	}
	return 0, errors.New("swu: proposal missing integrity transform")
}

// parsePRF extracts the PRF transform ID from a proposal.
func parsePRF(p *ikev2.Proposal) (uint16, error) {
	for _, t := range p.Transforms {
		if t.TransformType == byte(transformPRF) {
			return t.TransformID, nil
		}
	}
	return 0, errors.New("swu: proposal missing PRF transform")
}

// parseDH extracts the DH group transform ID from a proposal.
func parseDH(p *ikev2.Proposal) (uint16, error) {
	for _, t := range p.Transforms {
		if t.TransformType == byte(transformDH) {
			return t.TransformID, nil
		}
	}
	return 0, errors.New("swu: proposal missing DH transform")
}

// parseIKEProposal parses an IKE proposal into the four algorithm IDs.
func parseIKEProposal(p *ikev2.Proposal) (encr, prf, integ, dh uint16, err error) {
	encr, err = parseEncr(p)
	if err != nil {
		return
	}
	prf, err = parsePRF(p)
	if err != nil {
		return
	}
	integ, err = parseInteg(p)
	if err != nil {
		return
	}
	dh, err = parseDH(p)
	return
}

// parseESPProposal parses an ESP proposal into encryption/integrity IDs.
func parseESPProposal(p *ikev2.Proposal) (encr, integ uint16, err error) {
	encr, err = parseEncr(p)
	if err != nil {
		return
	}
	integ, err = parseOptionalInteg(p)
	return
}

func parseOptionalInteg(p *ikev2.Proposal) (uint16, error) {
	for _, transform := range p.Transforms {
		if transform.TransformType == byte(transformIntegrity) {
			return transform.TransformID, nil
		}
	}
	return 0, nil
}

// normalizeProposal normalises a proposal's transform list (dedupe, order).
func normalizeProposal(p *ikev2.Proposal) *ikev2.Proposal {
	if p == nil {
		return nil
	}
	seen := make(map[byte]bool)
	var out []*ikev2.Transform
	for _, t := range p.Transforms {
		if t == nil || seen[t.TransformType] {
			continue
		}
		seen[t.TransformType] = true
		out = append(out, t)
	}
	p.Transforms = out
	return p
}

// cloneProposal returns a deep copy of a proposal.
func cloneProposal(p *ikev2.Proposal) *ikev2.Proposal {
	if p == nil {
		return nil
	}
	cp := &ikev2.Proposal{
		ProposalNum: p.ProposalNum,
		ProtocolID:  p.ProtocolID,
		SPISize:     p.SPISize,
		SPI:         append([]byte{}, p.SPI...),
	}
	for _, t := range p.Transforms {
		cp.Transforms = append(cp.Transforms, &ikev2.Transform{
			TransformType: t.TransformType,
			TransformID:   t.TransformID,
			Attributes:    t.Attributes,
		})
	}
	return cp
}

// filterIKEProposals keeps only the proposals whose algorithms are supported.
func filterIKEProposals(proposals []*ikev2.Proposal) []*ikev2.Proposal {
	var out []*ikev2.Proposal
	for _, p := range proposals {
		if filterIKEProposal(p) {
			out = append(out, p)
		}
	}
	return out
}

// filterIKEProposal reports whether an IKE proposal is supported.
func filterIKEProposal(p *ikev2.Proposal) bool {
	_, _, _, _, err := parseIKEProposal(p)
	return err == nil
}

// filterESPProposal reports whether an ESP proposal is supported.
func filterESPProposal(p *ikev2.Proposal) bool {
	_, _, err := parseESPProposal(p)
	return err == nil
}

// summarizeIKEProposal returns a short human-readable summary of a proposal.
func summarizeIKEProposal(p *ikev2.Proposal) string {
	if p == nil {
		return "<nil>"
	}
	encr, prf, integ, dh, err := parseIKEProposal(p)
	if err != nil {
		return fmt.Sprintf("proposal %d (unparsed)", p.ProposalNum)
	}
	return fmt.Sprintf("proposal %d: encr=%d prf=%d integ=%d dh=%d", p.ProposalNum, encr, prf, integ, dh)
}

// prioritizeDHGroup reorders the DH group preference list so the preferred
// group comes first (recovered from the decompiled prioritizeDHGroup).
func prioritizeDHGroup(groups []uint16, preferred uint16) []uint16 {
	if preferred == 0 {
		return groups
	}
	out := make([]uint16, 0, len(groups))
	out = append(out, preferred)
	for _, g := range groups {
		if g != preferred {
			out = append(out, g)
		}
	}
	return out
}
