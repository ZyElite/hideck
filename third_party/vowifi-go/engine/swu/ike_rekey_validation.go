package swu

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func (s *Session) validateIKESARekeyResponse(payloads []ikev2.Payload) (*ikeSARekeySelection, error) {
	var sa *ikev2.EncryptedPayloadSA
	var nonce, peerKey []byte
	for _, payload := range payloads {
		if payload == nil {
			return nil, errors.New("swu: IKE rekey contains a nil payload")
		}
		switch payload.Type() {
		case ikev2.PayloadSA:
			value, ok := payload.(*ikev2.EncryptedPayloadSA)
			if !ok || sa != nil {
				return nil, errors.New("swu: invalid or duplicate IKE rekey SA payload")
			}
			sa = value
		case ikev2.PayloadNi:
			if nonce != nil {
				return nil, errors.New("swu: duplicate IKE rekey nonce payload")
			}
			nonce = childSANonceData(payload)
		case ikev2.PayloadKE:
			var err error
			peerKey, err = s.ikeRekeyPeerKey(payload, peerKey)
			if err != nil {
				return nil, err
			}
		}
	}
	if sa == nil || len(sa.Proposals) != 1 || len(nonce) == 0 || len(peerKey) == 0 {
		return nil, errors.New("swu: IKE rekey response missing SA, nonce, or KE")
	}
	if err := s.validateIKERekeyProposal(sa.Proposals[0]); err != nil {
		return nil, err
	}
	var responderSPI [8]byte
	copy(responderSPI[:], sa.Proposals[0].SPI)
	return &ikeSARekeySelection{
		responderSPI: responderSPI, nonce: append([]byte(nil), nonce...), peerKey: peerKey,
	}, nil
}

func (s *Session) ikeRekeyPeerKey(payload ikev2.Payload, existing []byte) ([]byte, error) {
	if existing != nil {
		return nil, errors.New("swu: duplicate IKE rekey KE payload")
	}
	group, key, err := parseKEPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("swu: parse IKE rekey KE: %w", err)
	}
	if group != s.dhGroup {
		return nil, fmt.Errorf("swu: invalid IKE rekey DH group %d, want %d", group, s.dhGroup)
	}
	return append([]byte(nil), key...), nil
}

func (s *Session) validateIKERekeyProposal(proposal *ikev2.Proposal) error {
	if proposal == nil || proposal.ProtocolID != ikev2.ProtoIKE || len(proposal.SPI) != 8 {
		return errors.New("swu: invalid IKE rekey proposal")
	}
	expected := map[ikev2.TransformType]ikev2.AlgorithmType{
		ikev2.TransformTypeEncr: ikev2.AlgorithmType(s.encrAlg),
		ikev2.TransformTypePRF:  ikev2.AlgorithmType(s.prfAlg),
		ikev2.TransformTypeDH:   ikev2.AlgorithmType(s.dhGroup),
	}
	if !s.aead {
		expected[ikev2.TransformTypeInteg] = ikev2.AlgorithmType(s.integAlg)
	}
	seen := make(map[ikev2.TransformType]bool, len(expected))
	for _, transform := range proposal.Transforms {
		if err := s.validateIKERekeyTransform(transform, expected, seen); err != nil {
			return err
		}
	}
	if len(seen) != len(expected) || binary.BigEndian.Uint64(proposal.SPI) == 0 {
		return errors.New("swu: incomplete IKE rekey proposal")
	}
	return nil
}

func (s *Session) validateIKERekeyTransform(
	transform *ikev2.Transform,
	expected map[ikev2.TransformType]ikev2.AlgorithmType,
	seen map[ikev2.TransformType]bool,
) error {
	if transform == nil {
		return errors.New("swu: IKE rekey proposal contains a nil transform")
	}
	want, ok := expected[transform.Type]
	if !ok || seen[transform.Type] || transform.ID != want {
		return fmt.Errorf("swu: unexpected IKE rekey transform type=%d id=%d", transform.Type, transform.ID)
	}
	if transform.Type == ikev2.TransformTypeEncr {
		if err := validateEncryptionKeyLength(transform, s.encKeyBits); err != nil {
			return err
		}
	} else if len(transform.Attributes) != 0 {
		return errors.New("swu: non-encryption IKE rekey transform has attributes")
	}
	seen[transform.Type] = true
	return nil
}
