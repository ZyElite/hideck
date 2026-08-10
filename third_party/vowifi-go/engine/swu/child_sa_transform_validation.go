package swu

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const transformAttributeKeyLength uint16 = 14

func validateESPSelection(proposal *ikev2.Proposal, offer childSAOffer) (uint32, selectedAlgorithms, uint16, bool, error) {
	if proposal == nil || proposal.ProtocolID != ikev2.ProtoESP ||
		(!offer.acceptNegotiatedAlgorithms && proposal.ProposalNum != 1) {
		return 0, selectedAlgorithms{}, 0, false, errors.New("swu: CHILD_SA response selected an invalid ESP proposal")
	}
	if len(proposal.SPI) != 4 {
		return 0, selectedAlgorithms{}, 0, false, errors.New("swu: CHILD_SA response SPI must be four bytes")
	}
	spi := binary.BigEndian.Uint32(proposal.SPI)
	if spi == 0 {
		return 0, selectedAlgorithms{}, 0, false, errors.New("swu: CHILD_SA response SPI is zero")
	}
	transforms, err := indexESPTransforms(proposal.Transforms)
	if err != nil {
		return 0, selectedAlgorithms{}, 0, false, err
	}
	algorithms, err := validateESPAlgorithms(proposal, transforms, offer)
	if err != nil {
		return 0, selectedAlgorithms{}, 0, false, err
	}
	esn, err := validateESPPFSAndESN(transforms, offer.dhGroup, offer.esn)
	if err != nil {
		return 0, selectedAlgorithms{}, 0, false, err
	}
	return spi, algorithms, offer.dhGroup, esn, nil
}

func validateESPAlgorithms(
	proposal *ikev2.Proposal,
	transforms map[ikev2.TransformType]*ikev2.Transform,
	offer childSAOffer,
) (selectedAlgorithms, error) {
	if offer.acceptNegotiatedAlgorithms {
		return validateNegotiatedESPAlgorithms(proposal, offer.offeredProposals)
	}
	encryption := transforms[ikev2.TypeEncryption]
	if encryption == nil || uint16(encryption.ID) != offer.encryption {
		return selectedAlgorithms{}, fmt.Errorf("swu: CHILD_SA encryption selection %v does not match offer %d", transformID(encryption), offer.encryption)
	}
	if err := validateEncryptionKeyLength(encryption, offer.encryptionKeyBits); err != nil {
		return selectedAlgorithms{}, err
	}
	integrity := transforms[ikev2.TypeIntegrity]
	if offer.integrity == 0 {
		if integrity != nil {
			return selectedAlgorithms{}, errors.New("swu: AEAD CHILD_SA response selected a separate integrity transform")
		}
	} else if integrity == nil || uint16(integrity.ID) != offer.integrity {
		return selectedAlgorithms{}, fmt.Errorf("swu: CHILD_SA integrity selection %v does not match offer %d", transformID(integrity), offer.integrity)
	}
	return selectedAlgorithms{
		encryption: uint16(encryption.ID), keyBits: offer.encryptionKeyBits, integrity: offer.integrity,
	}, nil
}

func validateNegotiatedESPAlgorithms(
	proposal *ikev2.Proposal,
	offered []*ikev2.Proposal,
) (selectedAlgorithms, error) {
	selection, err := firstESPAlgorithmSelection(proposal)
	if err != nil {
		return selectedAlgorithms{}, fmt.Errorf("swu: invalid selected ESP algorithms: %w", err)
	}
	if selection.keyBits == 0 {
		selection.keyBits = offeredESPKeyBitsForProposal(offered, proposal.ProposalNum, selection.encryption)
		if selection.keyBits == 0 {
			selection.keyBits = offeredESPKeyBits(offered, selection.encryption)
		}
	}
	encryption, err := supportedEncryption(selection.encryption, selection.keyBits)
	if err != nil {
		return selectedAlgorithms{}, capabilityNegotiationError("ESP Encr", selection.encryption, err)
	}
	integrity, err := crypto.GetIntegrityAlgorithm(selection.integrity)
	if err != nil {
		return selectedAlgorithms{}, capabilityNegotiationError("ESP Integ", selection.integrity, err)
	}
	if err := validateIntegrityMode("ESP", encryption.aead, selection.integrity); err != nil {
		return selectedAlgorithms{}, err
	}
	if integrity.KeySize() == 0 && !encryption.aead {
		return selectedAlgorithms{}, errors.New("swu: non-AEAD ESP selection has no integrity key")
	}
	return selection, nil
}

func offeredESPKeyBits(proposals []*ikev2.Proposal, encryption uint16) uint16 {
	for _, proposal := range proposals {
		selection := firstAlgorithmSelection(proposal)
		if selection.encryption == encryption && selection.keyBits != 0 {
			return selection.keyBits
		}
	}
	return 0
}

func offeredESPKeyBitsForProposal(proposals []*ikev2.Proposal, number uint8, encryption uint16) uint16 {
	for _, proposal := range proposals {
		if proposal == nil || proposal.ProposalNum != number {
			continue
		}
		selection := firstAlgorithmSelection(proposal)
		if selection.encryption == encryption && selection.keyBits != 0 {
			return selection.keyBits
		}
	}
	return 0
}

func validateESPPFSAndESN(
	transforms map[ikev2.TransformType]*ikev2.Transform,
	dhGroup uint16,
	expectESN bool,
) (bool, error) {
	dh := transforms[ikev2.TransformTypeDH]
	if dhGroup == 0 && dh != nil {
		return false, errors.New("swu: CHILD_SA response selected unoffered PFS")
	}
	if dhGroup != 0 && (dh == nil || uint16(dh.ID) != dhGroup) {
		return false, fmt.Errorf("swu: CHILD_SA PFS selection %v does not match offer %d", transformID(dh), dhGroup)
	}
	esn := transforms[ikev2.TypeESN]
	want := ikev2.AlgorithmType(0)
	if expectESN {
		want = 1
	}
	if esn == nil || esn.ID != want {
		return false, fmt.Errorf("swu: CHILD_SA response selected ESN %v, want %d", transformID(esn), want)
	}
	return esn.ID == 1, nil
}

func indexESPTransforms(transforms []*ikev2.Transform) (map[ikev2.TransformType]*ikev2.Transform, error) {
	indexed := make(map[ikev2.TransformType]*ikev2.Transform, len(transforms))
	for _, transform := range transforms {
		if transform == nil {
			return nil, errors.New("swu: CHILD_SA response contains a nil transform")
		}
		switch transform.Type {
		case ikev2.TransformTypeEncr, ikev2.TransformTypeInteg, ikev2.TransformTypeDH, ikev2.TransformTypeESN:
		default:
			return nil, fmt.Errorf("swu: CHILD_SA response selected unexpected transform type %d", transform.Type)
		}
		if indexed[transform.Type] != nil {
			return nil, fmt.Errorf("swu: CHILD_SA response selected duplicate transform type %d", transform.Type)
		}
		indexed[transform.Type] = transform
	}
	return indexed, nil
}

func validateEncryptionKeyLength(transform *ikev2.Transform, expectedBits uint16) error {
	if uint16(transform.ID) != crypto.EncrAESCBC && uint16(transform.ID) != crypto.EncrAESGCM16 {
		if len(transform.Attributes) != 0 {
			return errors.New("swu: non-AES CHILD_SA encryption selected unexpected attributes")
		}
		return nil
	}
	if len(transform.Attributes) != 1 || transform.Attributes[0].Type != transformAttributeKeyLength ||
		transform.Attributes[0].Val != expectedBits {
		return fmt.Errorf("swu: AES selection requires a %d-bit KEY_LENGTH", expectedBits)
	}
	return nil
}

func transformID(transform *ikev2.Transform) any {
	if transform == nil {
		return "missing"
	}
	return transform.ID
}
