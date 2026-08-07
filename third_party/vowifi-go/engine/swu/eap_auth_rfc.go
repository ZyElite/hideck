package swu

import (
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

const eapTypeIdentity byte = 1

func (s *Session) handleRFCEAP(data []byte) error {
	packet, err := eapaka.ParsePacket(data)
	if err != nil {
		return fmt.Errorf("parse EAP: %w", err)
	}
	switch packet.Code {
	case eapaka.CodeRequest:
		return s.handleRFCEAPRequest(packet, data)
	case eapaka.CodeSuccess:
		if len(s.eapKeys.MSK) == 0 {
			return errors.New("swu: EAP success without derived MSK")
		}
		s.stage = stageFinal
		return nil
	case eapaka.CodeFailure:
		return errors.New("swu: EAP authentication failed")
	default:
		return fmt.Errorf("swu: unexpected EAP code %d", packet.Code)
	}
}

func (s *Session) handleRFCEAPRequest(packet eapaka.Packet, raw []byte) error {
	s.eapID = packet.Identifier
	s.eapType = packet.Type
	switch {
	case packet.Type == eapTypeIdentity:
		response := eapaka.Packet{
			Code: eapaka.CodeResponse, Identifier: packet.Identifier,
			Type: eapTypeIdentity, Data: []byte(s.currentEAPIdentity()),
		}
		return s.sendEAPPacket(response)
	case isAKAIdentityRequest(packet):
		response := eapaka.Packet{
			Code: eapaka.CodeResponse, Identifier: packet.Identifier,
			Type: packet.Type, Subtype: eapaka.SubtypeIdentity,
			Attributes: []eapaka.Attribute{eapaka.IdentityAttribute(s.currentEAPIdentity())},
		}
		encoded, err := response.MarshalBinary()
		if err != nil {
			return err
		}
		s.eapIdentityTranscript = append(s.eapIdentityTranscript,
			append([]byte(nil), raw...), append([]byte(nil), encoded...))
		return s.sendEAPBytes(encoded)
	case isAKAChallenge(packet):
		return s.handleRFCChallenge(packet)
	default:
		return fmt.Errorf("swu: unsupported EAP request type=%d subtype=%d", packet.Type, packet.Subtype)
	}
}

func isAKAIdentityRequest(packet eapaka.Packet) bool {
	return (packet.Type == eapaka.TypeAKA || packet.Type == eapaka.TypeAKAPrime) &&
		packet.Subtype == eapaka.SubtypeIdentity
}

func isAKAChallenge(packet eapaka.Packet) bool {
	return (packet.Type == eapaka.TypeAKA || packet.Type == eapaka.TypeAKAPrime) &&
		packet.Subtype == eapaka.SubtypeChallenge
}

func (s *Session) handleRFCChallenge(packet eapaka.Packet) error {
	if response, negotiated, err := eapaka.BuildAKAPrimeKDFNegotiationResponse(packet); err != nil {
		return err
	} else if negotiated {
		return s.sendEAPPacket(response)
	}
	if s.cfg == nil || s.cfg.AKAProvider == nil {
		return errors.New("swu: no AKA provider configured")
	}
	rand16, autn16, err := eapaka.ChallengeRANDAndAUTN(packet)
	if err != nil {
		return err
	}
	aka, err := s.cfg.AKAProvider.CalculateAKA(rand16, autn16)
	if err != nil {
		if errors.Is(err, enginesim.ErrSyncFailure) && len(aka.AUTS) > 0 {
			response, buildErr := eapaka.BuildSynchronizationFailureResponse(packet, aka.AUTS)
			if buildErr != nil {
				return buildErr
			}
			return s.sendEAPPacket(response)
		}
		return fmt.Errorf("swu: AKA computation failed: %w", err)
	}
	response, keys, err := eapaka.BuildChallengeResponseWithCheckcode(
		s.currentEAPIdentity(), packet, aka, s.eapIdentityTranscript,
	)
	if err != nil {
		return err
	}
	s.eapKeys = keys
	return s.sendEAPPacket(response)
}

func (s *Session) sendEAPPacket(packet eapaka.Packet) error {
	raw, err := packet.MarshalBinary()
	if err != nil {
		return err
	}
	return s.sendEAPBytes(raw)
}

func (s *Session) sendEAPBytes(raw []byte) error {
	return s.sendIKEAuthRequest([]ikev2.Payload{
		&ikev2.EncryptedPayloadEAP{Data: append([]byte(nil), raw...)},
	})
}
