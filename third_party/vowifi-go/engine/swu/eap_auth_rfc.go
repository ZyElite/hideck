package swu

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/logger"
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

const eapTypeIdentity byte = 1

func (s *Session) handleRFCEAP(data []byte) error {
	payloads, err := s.handleEAP(data)
	if err != nil || len(payloads) == 0 {
		return err
	}
	return s.sendIKEAuthRequest(payloads)
}

// handleEAP restores the original state-auth contract: it parses one EAP
// message and returns the IKE payloads to send. The IKE_AUTH state machine owns
// transport and retransmission.
func (s *Session) handleEAP(data []byte) ([]ikev2.Payload, error) {
	packet, err := eapaka.ParsePacket(data)
	if err != nil {
		return nil, fmt.Errorf("parse EAP: %w", err)
	}
	switch packet.Code {
	case eapaka.CodeRequest:
		return s.handleRFCEAPRequest(packet, data)
	case eapaka.CodeSuccess:
		if len(s.eapKeys.MSK) == 0 {
			return nil, errors.New("swu: EAP success without derived MSK")
		}
		if s.eapResultIndicated && !s.eapResultConfirmed {
			return nil, errors.New("swu: EAP success before authenticated result indication")
		}
		s.stage = stageFinal
		return nil, nil
	case eapaka.CodeFailure:
		return nil, errors.New("swu: EAP authentication failed")
	default:
		return nil, fmt.Errorf("swu: unexpected EAP code %d", packet.Code)
	}
}

func (s *Session) handleRFCEAPRequest(packet eapaka.Packet, raw []byte) ([]ikev2.Payload, error) {
	s.eapID = packet.Identifier
	s.eapType = packet.Type
	switch {
	case packet.Type == eapTypeIdentity:
		identity := s.currentEAPIdentity()
		s.eapIdentity, s.eapIdentitySet = identity, true
		response := eapaka.Packet{
			Code: eapaka.CodeResponse, Identifier: packet.Identifier,
			Type: eapTypeIdentity, Data: []byte(identity),
		}
		return eapResponsePayload(response)
	case isAKAIdentityRequest(packet):
		response, err := s.buildAKAIdentityResponse(packet)
		if err != nil {
			return nil, err
		}
		encoded, err := response.MarshalBinary()
		if err != nil {
			return nil, err
		}
		s.recordAKAIdentityExchange(raw, encoded)
		return eapBytesPayload(encoded), nil
	case isAKAChallenge(packet):
		return s.handleRFCChallenge(packet)
	case isAKAReauthenticationRequest(packet):
		return s.handleLegacyFastReauthentication(packet, raw)
	case isAKANotification(packet):
		return s.handleRFCNotification(packet)
	default:
		return nil, fmt.Errorf("swu: unsupported EAP request type=%d subtype=%d", packet.Type, packet.Subtype)
	}
}

func (s *Session) buildAKAIdentityResponse(request eapaka.Packet) (eapaka.Packet, error) {
	if err := validateParsedSimakaAttributes(request.Type, request.Attributes); err != nil {
		return eapaka.Packet{}, err
	}
	identity, include, err := s.requestedAKAIdentity(request.Attributes)
	if err != nil {
		return eapaka.Packet{}, err
	}
	response := eapaka.Packet{
		Code: eapaka.CodeResponse, Identifier: request.Identifier,
		Type: request.Type, Subtype: eapaka.SubtypeIdentity,
	}
	s.eapIdentity, s.eapIdentitySet = identity, true
	if include {
		response.Attributes = []eapaka.Attribute{eapaka.IdentityAttribute(identity)}
	}
	return response, nil
}

func (s *Session) requestedAKAIdentity(attributes []eapaka.Attribute) (string, bool, error) {
	_, permanent := eapaka.FindAttribute(attributes, eapaka.AttributePermanentIDReq)
	_, full := eapaka.FindAttribute(attributes, eapaka.AttributeFullAuthIDReq)
	_, any := eapaka.FindAttribute(attributes, eapaka.AttributeAnyIDReq)
	if !permanent && !full && !any {
		return "", false, nil
	}
	if any && s.fastReauthCtx != nil && s.fastReauthCtx.CanUseReauth() {
		return s.fastReauthCtx.ReauthID, true, nil
	}
	imsi, err := requiredConfiguredIMSI(s.cfg)
	if err != nil {
		return "", false, err
	}
	return buildNAI(imsi, s.cfg), true, nil
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
	s.eapTranscript = append(s.eapTranscript,
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

func isAKAReauthenticationRequest(packet eapaka.Packet) bool {
	return (packet.Type == eapaka.TypeAKA || packet.Type == eapaka.TypeAKAPrime) &&
		packet.Subtype == eapaka.SubtypeReauthentication
}

func (s *Session) handleRFCChallenge(packet eapaka.Packet) ([]ikev2.Payload, error) {
	if err := validateParsedSimakaAttributes(packet.Type, packet.Attributes); err != nil {
		return nil, err
	}
	if response, negotiated, err := eapaka.BuildAKAPrimeKDFNegotiationResponse(packet); err != nil {
		return nil, err
	} else if negotiated {
		return eapResponsePayload(response)
	}
	provider := s.configuredAKAProvider()
	if provider == nil {
		return nil, errors.New("swu: no AKA provider configured")
	}
	rand16, autn16, err := eapaka.ChallengeRANDAndAUTN(packet)
	if err != nil {
		return nil, err
	}
	aka, err := provider.CalculateAKA(rand16, autn16)
	if err != nil {
		if errors.Is(err, enginesim.ErrSyncFailure) && len(aka.AUTS) > 0 {
			return s.buildEAPSyncFailure(packet.Identifier, aka.AUTS)
		}
		return nil, fmt.Errorf("swu: AKA computation failed: %w", err)
	}
	response, keys, err := s.buildConfiguredChallengeResponse(packet, aka)
	if err != nil {
		return nil, err
	}
	if packet.Type == eapaka.TypeAKA && !s.cfg.DisableEAPMACValidation {
		requestRaw, marshalErr := packet.MarshalBinary()
		if marshalErr != nil {
			return nil, fmt.Errorf("swu: marshal AKA challenge for MAC verification: %w", marshalErr)
		}
		macAttribute, hasMAC := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeMAC)
		if !hasMAC {
			return nil, errors.New("swu: AKA challenge has no verifiable AT_MAC")
		}
		received, valueErr := macAttribute.FixedValue(16)
		if valueErr != nil {
			return nil, valueErr
		}
		if err := verifyEAPAKAMAC(requestRaw, nil, keys.KAut, received); err != nil {
			return nil, err
		}
	}
	response, err = s.applyAKAChallengeMode(response, packet, keys.KAut)
	if err != nil {
		return nil, err
	}
	_, s.eapResultIndicated = eapaka.FindAttribute(response.Attributes, eapaka.AttributeResultInd)
	s.eapResultConfirmed = false
	s.eapKeys = keys
	if err := s.captureFastReauthentication(packet, keys); err != nil {
		return nil, err
	}
	return eapResponsePayload(response)
}

func (s *Session) configuredAKAProvider() AKAProvider {
	if s.cfg == nil {
		return nil
	}
	if s.cfg.AKAProvider != nil {
		return s.cfg.AKAProvider
	}
	return s.cfg.SIM
}

func (s *Session) buildConfiguredChallengeResponse(
	packet eapaka.Packet,
	aka enginesim.AKAResult,
) (eapaka.Packet, eapaka.Keys, error) {
	identity := s.currentEAPIdentityForKeyDerivation()
	if s.cfg.DisableEAPMACValidation {
		logger.Warn("EAP challenge MAC validation is explicitly disabled")
		return eapaka.BuildChallengeResponseWithoutMACValidation(
			identity, packet, aka, s.eapIdentityTranscript,
		)
	}
	return eapaka.BuildChallengeResponseWithCheckcode(
		identity, packet, aka, s.eapIdentityTranscript,
	)
}

func (s *Session) handleRFCNotification(packet eapaka.Packet) ([]ikev2.Payload, error) {
	if err := validateParsedSimakaAttributes(packet.Type, packet.Attributes); err != nil {
		return nil, err
	}
	if len(s.eapKeys.KAut) == 0 {
		response, handled, err := eapaka.BuildNotificationResponse(packet)
		if err != nil {
			return nil, err
		}
		if !handled {
			return nil, errors.New("swu: unsupported EAP-AKA notification")
		}
		return eapResponsePayload(response)
	}
	response, handled, err := eapaka.BuildAuthenticatedNotificationResponse(packet, s.eapKeys.KAut)
	if err != nil {
		return nil, err
	}
	if !handled {
		return nil, errors.New("swu: unsupported authenticated EAP-AKA notification")
	}
	confirmed, err := s.validateResultNotification(packet)
	if err != nil {
		return nil, err
	}
	s.eapResultConfirmed = confirmed
	return eapResponsePayload(response)
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
	payloads, err := eapResponsePayload(packet)
	if err != nil {
		return err
	}
	return s.sendIKEAuthRequest(payloads)
}

func (s *Session) sendEAPBytes(raw []byte) error {
	return s.sendIKEAuthRequest(eapBytesPayload(raw))
}

func eapResponsePayload(packet eapaka.Packet) ([]ikev2.Payload, error) {
	raw, err := packet.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return eapBytesPayload(raw), nil
}

func eapBytesPayload(raw []byte) []ikev2.Payload {
	return []ikev2.Payload{
		&ikev2.EncryptedPayloadEAP{EAPMessage: append([]byte(nil), raw...)},
	}
}
