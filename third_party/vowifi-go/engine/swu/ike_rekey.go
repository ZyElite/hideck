package swu

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

type ikeSARekeySelection struct {
	responderSPI [8]byte
	nonce        []byte
	peerKey      []byte
}

func (s *Session) performIKESARekey(ctx context.Context) error {
	s.ikeExchangeMu.Lock()
	defer s.ikeExchangeMu.Unlock()
	if s.socket == nil || s.ikeKeys == nil || s.State() != stateEstablished {
		return errors.New("swu: session not established")
	}
	dh, initiatorSPI, nonce, err := s.newIKESARekeyMaterial()
	if err != nil {
		return err
	}
	proposals := buildIKEProposalsForSession(s)
	proposals[0].SPI = append([]byte(nil), initiatorSPI[:]...)
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.CREATE_CHILD_SA, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{
			&ikev2.EncryptedPayloadSA{Proposals: proposals},
			&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), nonce...)},
			&ikev2.EncryptedPayloadKE{DHGroup: ikev2.AlgorithmType(s.dhGroup), KEData: dh.PublicKeyBytes()},
		},
	}
	payloads, err := s.exchangeEstablishedIKE(ctx, request)
	if err != nil {
		return err
	}
	selection, err := s.validateIKESARekeyResponse(payloads)
	if err != nil {
		return err
	}
	sharedSecret, err := dh.ComputeSharedSecret(selection.peerKey)
	if err != nil {
		return fmt.Errorf("swu: compute IKE rekey DH secret: %w", err)
	}
	newKeys, err := s.deriveIKESARekeyKeys(
		s.ikeKeys.SK_d, sharedSecret, nonce, selection.nonce,
		initiatorSPI, selection.responderSPI,
	)
	if err != nil {
		return fmt.Errorf("swu: derive rekeyed IKE SA keys: %w", err)
	}
	if err := s.deleteOldIKESA(ctx); err != nil {
		return fmt.Errorf("swu: delete old IKE SA: %w", err)
	}
	s.mu.Lock()
	s.SPIi, s.SPIr = initiatorSPI, selection.responderSPI
	s.localIKEInitiator = true
	s.ikeKeys = newKeys
	s.dh = dh
	s.dhSharedSecret = append([]byte(nil), sharedSecret...)
	s.Ni = append([]byte(nil), nonce...)
	s.nr = append([]byte(nil), selection.nonce...)
	s.nextOutboundID = 0
	s.mu.Unlock()
	return nil
}

func (s *Session) handlePeerIKESARekey(packet *ikev2.IKEPacket, payloads []ikev2.Payload) error {
	selection, err := s.validateIKESARekeyResponse(payloads)
	if err != nil {
		return err
	}
	dh, responderSPI, responderNonce, err := s.newIKESARekeyMaterial()
	if err != nil {
		return err
	}
	sharedSecret, err := dh.ComputeSharedSecret(selection.peerKey)
	if err != nil {
		return fmt.Errorf("swu: compute peer IKE rekey DH secret: %w", err)
	}
	newKeys, err := s.deriveIKESARekeyKeys(
		s.ikeKeys.SK_d, sharedSecret, selection.nonce, responderNonce,
		selection.responderSPI, responderSPI,
	)
	if err != nil {
		return fmt.Errorf("swu: derive peer-rekeyed IKE SA keys: %w", err)
	}
	proposals := buildIKEProposalsForSession(s)
	proposals[0].SPI = append([]byte(nil), responderSPI[:]...)
	responsePayloads := []ikev2.Payload{
		&ikev2.EncryptedPayloadSA{Proposals: proposals},
		&ikev2.EncryptedPayloadNonce{NonceData: append([]byte(nil), responderNonce...)},
		&ikev2.EncryptedPayloadKE{DHGroup: ikev2.AlgorithmType(s.dhGroup), KEData: dh.PublicKeyBytes()},
	}
	if err := s.sendEstablishedIKEResponse(packet, responsePayloads); err != nil {
		return err
	}
	s.mu.Lock()
	s.SPIi, s.SPIr = selection.responderSPI, responderSPI
	s.localIKEInitiator = false
	s.ikeKeys = newKeys
	s.dh = dh
	s.dhSharedSecret = append([]byte(nil), sharedSecret...)
	s.Ni = append([]byte(nil), selection.nonce...)
	s.nr = append([]byte(nil), responderNonce...)
	s.nextOutboundID = 0
	s.mu.Unlock()
	return nil
}

func (s *Session) newIKESARekeyMaterial() (*enginecrypto.DiffieHellman, [8]byte, []byte, error) {
	dh, err := enginecrypto.NewDiffieHellman(s.dhGroup)
	if err != nil {
		return nil, [8]byte{}, nil, err
	}
	if err := dh.GenerateKey(); err != nil {
		return nil, [8]byte{}, nil, err
	}
	var spi [8]byte
	for attempts := 0; attempts < 3 && spi == ([8]byte{}); attempts++ {
		if _, err := rand.Read(spi[:]); err != nil {
			return nil, [8]byte{}, nil, err
		}
	}
	if spi == ([8]byte{}) {
		return nil, [8]byte{}, nil, errors.New("swu: generated zero IKE rekey SPI")
	}
	nonce := make([]byte, s.nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, [8]byte{}, nil, err
	}
	return dh, spi, nonce, nil
}

func (s *Session) validateIKESARekeyResponse(payloads []ikev2.Payload) (*ikeSARekeySelection, error) {
	var sa *ikev2.EncryptedPayloadSA
	var nonce, peerKey []byte
	for _, payload := range payloads {
		switch payload.Type() {
		case ikev2.PayloadSA:
			var ok bool
			sa, ok = payload.(*ikev2.EncryptedPayloadSA)
			if !ok {
				return nil, errors.New("swu: invalid IKE rekey SA payload")
			}
		case ikev2.PayloadNi:
			nonce = childSANonceData(payload)
		case ikev2.PayloadKE:
			group, key, err := parseKEPayload(payload)
			if err != nil || group != s.dhGroup {
				return nil, fmt.Errorf("swu: invalid IKE rekey DH group %d: %w", group, err)
			}
			peerKey = append([]byte(nil), key...)
		}
	}
	if sa == nil || len(sa.Proposals) != 1 || len(nonce) == 0 || len(peerKey) == 0 {
		return nil, errors.New("swu: IKE rekey response missing SA, nonce, or KE")
	}
	proposal := sa.Proposals[0]
	if err := s.validateIKERekeyProposal(proposal); err != nil {
		return nil, err
	}
	var responderSPI [8]byte
	copy(responderSPI[:], proposal.SPI)
	return &ikeSARekeySelection{
		responderSPI: responderSPI,
		nonce:        append([]byte(nil), nonce...),
		peerKey:      peerKey,
	}, nil
}

func (s *Session) validateIKERekeyProposal(proposal *ikev2.Proposal) error {
	if proposal == nil || proposal.ProtocolID != ikev2.ProtoIKE || len(proposal.SPI) != 8 {
		return errors.New("swu: invalid IKE rekey proposal")
	}
	expected := map[ikev2.TransformType]ikev2.AlgorithmType{
		ikev2.TransformTypeEncr:  ikev2.AlgorithmType(s.encrAlg),
		ikev2.TransformTypePRF:   ikev2.AlgorithmType(s.prfAlg),
		ikev2.TransformTypeInteg: ikev2.AlgorithmType(s.integAlg),
		ikev2.TransformTypeDH:    ikev2.AlgorithmType(s.dhGroup),
	}
	seen := make(map[ikev2.TransformType]bool, len(expected))
	for _, transform := range proposal.Transforms {
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
	}
	if len(seen) != len(expected) || binary.BigEndian.Uint64(proposal.SPI) == 0 {
		return errors.New("swu: incomplete IKE rekey proposal")
	}
	return nil
}

func (s *Session) deleteOldIKESA(ctx context.Context) error {
	request := &ikev2.IKEPacket{
		Header: newIKEHeader(
			s.SPIi, s.SPIr, ikev2.INFORMATIONAL, s.localIKEFlags(false), s.nextMessageID(),
		),
		Payloads: []ikev2.Payload{&ikev2.EncryptedPayloadDelete{
			ProtocolID: ikev2.ProtoIKE,
		}},
	}
	payloads, err := s.exchangeEstablishedIKE(ctx, request)
	if err != nil {
		return err
	}
	if len(payloads) != 0 {
		return fmt.Errorf("swu: old IKE SA delete response contains payloads %s", ikePayloadTypes(payloads))
	}
	return nil
}
