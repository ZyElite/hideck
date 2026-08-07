package swu

import (
	"bytes"
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
		if s.eapResultIndicated && !s.eapResultConfirmed {
			return errors.New("swu: EAP success before authenticated result indication")
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
		encoded, err := response.MarshalBinary()
		if err != nil {
			return err
		}
		return s.sendEAPBytes(encoded)
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
		s.recordAKAIdentityExchange(raw, encoded)
		return s.sendEAPBytes(encoded)
	case isAKAChallenge(packet):
		return s.handleRFCChallenge(packet)
	case isAKANotification(packet):
		return s.handleRFCNotification(packet)
	default:
		return fmt.Errorf("swu: unsupported EAP request type=%d subtype=%d", packet.Type, packet.Subtype)
	}
}

func (s *Session) recordAKAIdentityExchange(request, response []byte) {
	for index := 0; index+1 < len(s.eapIdentityTranscript); index += 2 {
		if bytes.Equal(s.eapIdentityTranscript[index], request) &&
			bytes.Equal(s.eapIdentityTranscript[index+1], response) {
			return
		}
	}
	s.eapIdentityTranscript = append(s.eapIdentityTranscript,
		append([]byte(nil), request...), append([]byte(nil), response...))
}

func isAKAIdentityRequest(packet eapaka.Packet) bool {
	return (packet.Type == eapaka.TypeAKA || packet.Type == eapaka.TypeAKAPrime) &&
		packet.Subtype == eapaka.SubtypeIdentity
}

func isAKAChallenge(packet eapaka.Packet) bool {
	return (packet.Type == eapaka.TypeAKA || packet.Type == eapaka.TypeAKAPrime) &&
		packet.Subtype == eapaka.SubtypeChallenge
}

func isAKANotification(packet eapaka.Packet) bool {
	return (packet.Type == eapaka.TypeAKA || packet.Type == eapaka.TypeAKAPrime) &&
		packet.Subtype == eapaka.SubtypeNotification
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
	_, s.eapResultIndicated = eapaka.FindAttribute(packet.Attributes, eapaka.AttributeResultInd)
	s.eapResultConfirmed = false
	s.eapKeys = keys
	return s.sendEAPPacket(response)
}

func (s *Session) handleRFCNotification(packet eapaka.Packet) error {
	if len(s.eapKeys.KAut) == 0 {
		response, handled, err := eapaka.BuildNotificationResponse(packet)
		if err != nil {
			return err
		}
		if !handled {
			return errors.New("swu: unsupported EAP-AKA notification")
		}
		return s.sendEAPPacket(response)
	}
	response, handled, err := eapaka.BuildAuthenticatedNotificationResponse(packet, s.eapKeys.KAut)
	if err != nil {
		return err
	}
	if !handled {
		return errors.New("swu: unsupported authenticated EAP-AKA notification")
	}
	confirmed, err := s.validateResultNotification(packet)
	if err != nil {
		return err
	}
	if err := s.sendEAPPacket(response); err != nil {
		return err
	}
	s.eapResultConfirmed = confirmed
	return nil
}

func (s *Session) validateResultNotification(packet eapaka.Packet) (bool, error) {
	attribute, ok := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeNotification)
	if !ok {
		return false, errors.New("swu: EAP-AKA notification missing result code")
	}
	code, err := attribute.NotificationValue()
	if err != nil {
		return false, err
	}
	if code == eapaka.NotificationSuccess && !s.eapResultIndicated {
		return false, errors.New("swu: unsolicited EAP-AKA success notification")
	}
	return code == eapaka.NotificationSuccess, nil
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
