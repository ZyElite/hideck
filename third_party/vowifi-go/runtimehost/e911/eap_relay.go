package e911

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

type entitlementChallengeState struct {
	eapKeys            *eapaka.Keys
	identityTranscript [][]byte
	identityState      eapaka.EncryptedIdentityState
	reauthentication   swu.EAPReauthenticationState
	reauthUpdated      bool
}

func newEntitlementChallengeState(req Request) entitlementChallengeState {
	state := entitlementChallengeState{
		reauthentication: cloneEAPReauthenticationState(req.EAPReauthentication),
	}
	if state.reauthentication.Usable() {
		state.eapKeys = eapKeysPtr(state.reauthentication.Keys)
	}
	return state
}

func (s entitlementChallengeState) applyToResponse(response Response) Response {
	response.EAPNextPseudonym = s.identityState.NextPseudonym
	response.EAPNextReauthID = s.identityState.NextReauthID
	if !s.reauthUpdated {
		return response
	}
	response.EAPReauthentication = cloneEAPReauthenticationState(s.reauthentication)
	if response.EAPNextPseudonym == "" {
		response.EAPNextPseudonym = s.reauthentication.NextPseudonym
	}
	if response.EAPNextReauthID == "" {
		response.EAPNextReauthID = s.reauthentication.Identity
	}
	return response
}

func challengeAnswer(req Request, result entitlementResult, state *entitlementChallengeState) ([]byte, error) {
	body := entitlementChallengeAnswerBody(req, result)
	handled, err := answerEAPControl(req, result, state, body)
	if err != nil {
		return nil, err
	}
	if !handled {
		if err := answerAKAChallenge(req, result, state, body); err != nil {
			return nil, err
		}
	}
	return json.Marshal([]map[string]interface{}{body})
}

func entitlementChallengeAnswerBody(req Request, result entitlementResult) map[string]interface{} {
	return map[string]interface{}{
		"message-id":    2,
		"operation":     "emergency-address-update",
		"response-id":   result.ResponseID,
		"sip-username":  req.Identity.SIPUsername,
		"terminal-imei": req.Identity.IMEI,
	}
}

func answerEAPControl(req Request, result entitlementResult, state *entitlementChallengeState, body map[string]interface{}) (bool, error) {
	identity := firstNonEmpty(req.Identity.SIPUsername, req.Identity.IMSI)
	if relay, raw, handled, err := buildEAPRelayIdentityAnswer(result, identity); handled || err != nil {
		if err == nil {
			body["eap-relay-packet"] = relay
			err = state.recordIdentityExchange(result, raw)
		}
		return handled, err
	}
	if relay, handled, err := buildEAPRelayKDFNegotiationAnswer(result); handled || err != nil {
		if err == nil {
			body["eap-relay-packet"] = relay
		}
		return handled, err
	}
	if relay, handled, err := buildEAPRelayNotificationAnswer(result, state.eapKeys); handled || err != nil {
		if err == nil {
			body["eap-relay-packet"] = relay
		}
		return handled, err
	}
	return answerEAPReauthenticationOrUnsupported(req, result, state, body)
}

func answerEAPReauthenticationOrUnsupported(req Request, result entitlementResult, state *entitlementChallengeState, body map[string]interface{}) (bool, error) {
	relay, keys, next, handled, err := buildEAPRelayReauthenticationAnswer(req, result, state.reauthentication)
	if handled || err != nil {
		if err == nil {
			body["eap-relay-packet"] = relay
			state.eapKeys = keys
			state.reauthentication = next
			state.reauthUpdated = true
		}
		return handled, err
	}
	if result.EAPPacket == nil || result.EAPPacket.Subtype == eapaka.SubtypeChallenge {
		return false, nil
	}
	relay, err = buildEAPRelayClientErrorAnswer(result, eapaka.ClientErrorUnableToProcessPacket)
	if err == nil {
		body["eap-relay-packet"] = relay
	}
	return true, err
}

func answerAKAChallenge(req Request, result entitlementResult, state *entitlementChallengeState, body map[string]interface{}) error {
	provider, ok := req.AKAProvider.(enginesim.AKAProvider)
	if !ok || provider == nil {
		return ErrChallengeNotImplemented
	}
	aka, err := provider.CalculateAKA(result.RAND, result.AUTN)
	syncFailure := errors.Is(err, enginesim.ErrSyncFailure)
	authFailure := errors.Is(err, enginesim.ErrAuthFailure)
	if err != nil && !syncFailure && !authFailure {
		return fmt.Errorf("e911: calculate AKA: %w", err)
	}
	if syncFailure && len(aka.AUTS) == 0 {
		return fmt.Errorf("e911: calculate AKA: %w without AUTS", err)
	}
	if authFailure {
		return answerAKAAuthenticationReject(result, body, err)
	}
	populateAKAAnswer(body, aka)
	return answerEAPAKAChallenge(req, result, state, body, aka, syncFailure)
}

func answerAKAAuthenticationReject(result entitlementResult, body map[string]interface{}, authErr error) error {
	if result.EAPPacket == nil {
		return fmt.Errorf("e911: calculate AKA: %w", authErr)
	}
	relay, err := buildEAPRelayAuthenticationRejectAnswer(result)
	if err == nil {
		body["eap-relay-packet"] = relay
	}
	return err
}

func populateAKAAnswer(body map[string]interface{}, aka enginesim.AKAResult) {
	body["aka-res"] = strings.ToUpper(hex.EncodeToString(aka.RES))
	body["aka-ck"] = strings.ToUpper(hex.EncodeToString(aka.CK))
	body["aka-ik"] = strings.ToUpper(hex.EncodeToString(aka.IK))
	body["aka-auts"] = strings.ToUpper(hex.EncodeToString(aka.AUTS))
}

func answerEAPAKAChallenge(req Request, result entitlementResult, state *entitlementChallengeState, body map[string]interface{}, aka enginesim.AKAResult, syncFailure bool) error {
	if result.EAPPacket == nil {
		return nil
	}
	identity := firstNonEmpty(req.Identity.SIPUsername, req.Identity.IMSI)
	relay, keys, identityState, err := buildEAPRelayAnswer(result, aka, identity, syncFailure, state.identityTranscript)
	if err != nil {
		return err
	}
	body["eap-relay-packet"] = relay
	if keys != nil {
		state.eapKeys = keys
	}
	state.identityState = mergeEAPIdentityState(state.identityState, identityState)
	state.acceptFullAuthentication(keys)
	return nil
}

func (s *entitlementChallengeState) recordIdentityExchange(result entitlementResult, response []byte) error {
	request := append([]byte(nil), result.EAPPacketRaw...)
	if len(request) == 0 && result.EAPPacket != nil {
		var err error
		request, err = result.EAPPacket.MarshalBinary()
		if err != nil {
			return err
		}
	}
	s.identityTranscript = append(s.identityTranscript, request, append([]byte(nil), response...))
	return nil
}

func (s *entitlementChallengeState) acceptFullAuthentication(keys *eapaka.Keys) {
	if keys == nil {
		return
	}
	next, ok := eapReauthenticationStateFromFullAuth(s.reauthentication, *keys, s.identityState)
	if !ok {
		return
	}
	s.reauthentication = next
	s.reauthUpdated = true
}

func buildEAPRelayIdentityAnswer(result entitlementResult, identity string) (string, []byte, bool, error) {
	if result.EAPPacket == nil || result.EAPPacket.Code != eapaka.CodeRequest || result.EAPPacket.Subtype != eapaka.SubtypeIdentity {
		return "", nil, false, nil
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return "", nil, true, ErrChallengeNotImplemented
	}
	packet := eapaka.Packet{
		Code: eapaka.CodeResponse, Identifier: result.EAPPacket.Identifier,
		Type: result.EAPPacket.Type, Subtype: eapaka.SubtypeIdentity,
		Attributes: []eapaka.Attribute{eapaka.IdentityAttribute(identity)},
	}
	raw, err := packet.MarshalBinary()
	return base64.StdEncoding.EncodeToString(raw), raw, true, err
}

func buildEAPRelayKDFNegotiationAnswer(result entitlementResult) (string, bool, error) {
	if result.EAPPacket == nil {
		return "", false, nil
	}
	packet, negotiated, err := eapaka.BuildAKAPrimeKDFNegotiationResponse(*result.EAPPacket)
	if err != nil || !negotiated {
		return "", negotiated, err
	}
	return marshalEAPRelayPacket(packet, true)
}

func buildEAPRelayNotificationAnswer(result entitlementResult, keys *eapaka.Keys) (string, bool, error) {
	if result.EAPPacket == nil {
		return "", false, nil
	}
	packet, handled, err := eapaka.BuildNotificationResponse(*result.EAPPacket)
	if errors.Is(err, eapaka.ErrInvalidKeyMaterial) && keys != nil {
		packet, handled, err = eapaka.BuildAuthenticatedNotificationResponse(*result.EAPPacket, keys.KAut)
	}
	if err != nil || !handled {
		return "", handled, err
	}
	return marshalEAPRelayPacket(packet, true)
}

func buildEAPRelayAuthenticationRejectAnswer(result entitlementResult) (string, error) {
	packet, err := eapaka.BuildAuthenticationRejectResponse(*result.EAPPacket)
	if err != nil {
		return "", err
	}
	relay, _, err := marshalEAPRelayPacket(packet, true)
	return relay, err
}

func buildEAPRelayClientErrorAnswer(result entitlementResult, code uint16) (string, error) {
	packet, err := eapaka.BuildClientErrorResponse(*result.EAPPacket, code)
	if err != nil {
		return "", err
	}
	relay, _, err := marshalEAPRelayPacket(packet, true)
	return relay, err
}

func marshalEAPRelayPacket(packet eapaka.Packet, handled bool) (string, bool, error) {
	raw, err := packet.MarshalBinary()
	if err != nil {
		return "", handled, err
	}
	return base64.StdEncoding.EncodeToString(raw), handled, nil
}

func buildEAPRelayAnswer(result entitlementResult, aka enginesim.AKAResult, identity string, syncFailure bool, transcript [][]byte) (string, *eapaka.Keys, eapaka.EncryptedIdentityState, error) {
	if syncFailure {
		packet, err := eapaka.BuildSynchronizationFailureResponse(*result.EAPPacket, aka.AUTS)
		if err != nil {
			return "", nil, eapaka.EncryptedIdentityState{}, err
		}
		relay, _, err := marshalEAPRelayPacket(packet, true)
		return relay, nil, eapaka.EncryptedIdentityState{}, err
	}
	packet, keys, err := eapaka.BuildChallengeResponseWithCheckcode(strings.TrimSpace(identity), *result.EAPPacket, aka, transcript)
	if err != nil {
		return "", nil, eapaka.EncryptedIdentityState{}, err
	}
	identityState, err := decryptEAPIdentityState(*result.EAPPacket, keys)
	if err != nil {
		return "", nil, eapaka.EncryptedIdentityState{}, err
	}
	relay, _, err := marshalEAPRelayPacket(packet, true)
	return relay, eapKeysPtr(keys), identityState, err
}

func decryptEAPIdentityState(request eapaka.Packet, keys eapaka.Keys) (eapaka.EncryptedIdentityState, error) {
	attrs, _, err := eapaka.DecryptChallengeEncryptedAttributes(request, keys)
	if err != nil || len(attrs) == 0 {
		return eapaka.EncryptedIdentityState{}, err
	}
	return eapaka.IdentityStateFromAttributes(attrs)
}

func mergeEAPIdentityState(current, next eapaka.EncryptedIdentityState) eapaka.EncryptedIdentityState {
	if next.NextPseudonym != "" {
		current.NextPseudonym = next.NextPseudonym
	}
	if next.NextReauthID != "" {
		current.NextReauthID = next.NextReauthID
	}
	return current
}
