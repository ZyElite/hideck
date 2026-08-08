package swu

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const transformAttributeKeyLength uint16 = 14

func validateESPSelection(proposal *ikev2.Proposal, offer childSAOffer) (uint32, uint16, uint16, uint16, error) {
	if proposal == nil || proposal.ProposalNum != 1 || proposal.ProtocolID != ikev2.ProtoESP {
		return 0, 0, 0, 0, errors.New("swu: CHILD_SA response selected an invalid ESP proposal")
	}
	if len(proposal.SPI) != 4 {
		return 0, 0, 0, 0, errors.New("swu: CHILD_SA response SPI must be four bytes")
	}
	spi := binary.BigEndian.Uint32(proposal.SPI)
	if spi == 0 {
		return 0, 0, 0, 0, errors.New("swu: CHILD_SA response SPI is zero")
	}
	transforms, err := indexESPTransforms(proposal.Transforms)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	encryption, integrity, err := validateESPAlgorithms(transforms, offer)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if err := validateESPPFSAndESN(transforms, offer.dhGroup); err != nil {
		return 0, 0, 0, 0, err
	}
	return spi, encryption, integrity, offer.dhGroup, nil
}

func validateESPAlgorithms(
	transforms map[ikev2.TransformType]*ikev2.Transform,
	offer childSAOffer,
) (uint16, uint16, error) {
	encryption := transforms[ikev2.TypeEncryption]
	if encryption == nil || uint16(encryption.ID) != offer.encryption {
		return 0, 0, fmt.Errorf("swu: CHILD_SA encryption selection %v does not match offer %d", transformID(encryption), offer.encryption)
	}
	if err := validateEncryptionKeyLength(encryption, offer.encryptionKeyBits); err != nil {
		return 0, 0, err
	}
	integrity := transforms[ikev2.TypeIntegrity]
	if offer.integrity == 0 {
		if integrity != nil {
			return 0, 0, errors.New("swu: AEAD CHILD_SA response selected a separate integrity transform")
		}
	} else if integrity == nil || uint16(integrity.ID) != offer.integrity {
		return 0, 0, fmt.Errorf("swu: CHILD_SA integrity selection %v does not match offer %d", transformID(integrity), offer.integrity)
	}
	return uint16(encryption.ID), offer.integrity, nil
}

func validateESPPFSAndESN(
	transforms map[ikev2.TransformType]*ikev2.Transform,
	dhGroup uint16,
) error {
	dh := transforms[ikev2.TransformTypeDH]
	if dhGroup == 0 && dh != nil {
		return errors.New("swu: CHILD_SA response selected unoffered PFS")
	}
	if dhGroup != 0 && (dh == nil || uint16(dh.ID) != dhGroup) {
		return fmt.Errorf("swu: CHILD_SA PFS selection %v does not match offer %d", transformID(dh), dhGroup)
	}
	esn := transforms[ikev2.TypeESN]
	if esn == nil || esn.ID != 0 {
		return errors.New("swu: CHILD_SA response did not select ESN disabled")
	}
	return nil
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
