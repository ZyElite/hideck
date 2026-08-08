package e911

import (
	crand "crypto/rand"
	"encoding/base64"
	"io"
	"strings"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func buildEAPRelayReauthenticationAnswer(req Request, result entitlementResult, state swu.EAPReauthenticationState) (string, *eapaka.Keys, swu.EAPReauthenticationState, bool, error) {
	if result.EAPPacket == nil || result.EAPPacket.Subtype != eapaka.SubtypeReauthentication {
		return "", nil, state, false, nil
	}
	state = cloneEAPReauthenticationState(state)
	if !state.Usable() {
		return "", nil, state, false, nil
	}
	parsed, err := eapaka.ParseReauthenticationRequest(*result.EAPPacket, state.Keys)
	if err != nil {
		return "", nil, swu.EAPReauthenticationState{}, true, err
	}
	iv, err := entitlementRandomBytes(req.Random, 16)
	if err != nil {
		return "", nil, swu.EAPReauthenticationState{}, true, err
	}
	packet, keys, next, err := answerEAPReauthentication(*result.EAPPacket, parsed, state, iv)
	if err != nil {
		return "", nil, swu.EAPReauthenticationState{}, true, err
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		return "", nil, swu.EAPReauthenticationState{}, true, err
	}
	return base64.StdEncoding.EncodeToString(raw), eapKeysPtr(keys), cloneEAPReauthenticationState(next), true, nil
}

func answerEAPReauthentication(request eapaka.Packet, parsed eapaka.ReauthenticationRequest, state swu.EAPReauthenticationState, iv []byte) (eapaka.Packet, eapaka.Keys, swu.EAPReauthenticationState, error) {
	if state.CounterOK && parsed.Counter <= state.Counter {
		packet, err := eapaka.BuildReauthenticationCounterTooSmallResponse(request, state.Keys, iv)
		state.CounterTooSmall = true
		state.Reauthenticated = false
		state.LastRejectedCounter = parsed.Counter
		return packet, state.Keys, state, err
	}
	identity := strings.TrimSpace(state.Identity)
	if identity == "" {
		return eapaka.Packet{}, eapaka.Keys{}, swu.EAPReauthenticationState{}, ErrChallengeNotImplemented
	}
	packet, keys, err := eapaka.BuildReauthenticationResponse(identity, request, state.Keys, iv)
	if err != nil {
		return eapaka.Packet{}, eapaka.Keys{}, swu.EAPReauthenticationState{}, err
	}
	state.Keys = cloneEAPAKAKeys(keys)
	state.Counter = parsed.Counter
	state.CounterOK = true
	state.CounterTooSmall = false
	state.Reauthenticated = true
	state.LastAcceptedCounter = parsed.Counter
	if parsed.IdentityState.NextReauthID != "" {
		state.Identity = strings.TrimSpace(parsed.IdentityState.NextReauthID)
	}
	if parsed.IdentityState.NextPseudonym != "" {
		state.NextPseudonym = strings.TrimSpace(parsed.IdentityState.NextPseudonym)
	}
	return packet, keys, state, nil
}

func eapReauthenticationStateFromFullAuth(current swu.EAPReauthenticationState, keys eapaka.Keys, identity eapaka.EncryptedIdentityState) (swu.EAPReauthenticationState, bool) {
	next := cloneEAPReauthenticationState(current)
	if value := strings.TrimSpace(identity.NextReauthID); value != "" {
		next.Identity = value
	}
	if value := strings.TrimSpace(identity.NextPseudonym); value != "" {
		next.NextPseudonym = value
	}
	if strings.TrimSpace(next.Identity) == "" {
		return swu.EAPReauthenticationState{}, false
	}
	next.Keys = cloneEAPAKAKeys(keys)
	next.Counter = 0
	next.CounterOK = true
	next.Reauthenticated = false
	next.CounterTooSmall = false
	next.LastAcceptedCounter = 0
	next.LastRejectedCounter = 0
	return cloneEAPReauthenticationState(next), true
}

func entitlementRandomBytes(reader io.Reader, length int) ([]byte, error) {
	if reader == nil {
		reader = crand.Reader
	}
	out := make([]byte, length)
	if _, err := io.ReadFull(reader, out); err != nil {
		return nil, err
	}
	return out, nil
}

func eapKeysPtr(keys eapaka.Keys) *eapaka.Keys {
	cloned := cloneEAPAKAKeys(keys)
	return &cloned
}

func cloneEAPReauthenticationState(state swu.EAPReauthenticationState) swu.EAPReauthenticationState {
	state.Identity = strings.TrimSpace(state.Identity)
	state.NextPseudonym = strings.TrimSpace(state.NextPseudonym)
	state.Keys = cloneEAPAKAKeys(state.Keys)
	return state
}

func cloneEAPAKAKeys(keys eapaka.Keys) eapaka.Keys {
	return eapaka.Keys{
		MK:      append([]byte(nil), keys.MK...),
		KEncr:   append([]byte(nil), keys.KEncr...),
		KAut:    append([]byte(nil), keys.KAut...),
		KRe:     append([]byte(nil), keys.KRe...),
		MSK:     append([]byte(nil), keys.MSK...),
		EMSK:    append([]byte(nil), keys.EMSK...),
		CKPrime: append([]byte(nil), keys.CKPrime...),
		IKPrime: append([]byte(nil), keys.IKPrime...),
	}
}
