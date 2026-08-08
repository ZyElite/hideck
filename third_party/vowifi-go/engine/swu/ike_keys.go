package swu

import (
	"errors"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

// IKEKeys holds the IKE SA key material derived per RFC 7296 §2.14-2.21:
//
//	SKEYSEED = prf(Ni | Nr, g^ir)
//	{SK_d, SK_ai, SK_ar, SK_ei, SK_er, SK_pi, SK_pr} = prf+(SKEYSEED, Ni | Nr | SPIi | SPIr)
//
// SK_d derives child SA keys; SK_ai/SK_ar and SK_ei/SK_er protect IKE
// integrity/encryption; SK_pi/SK_pr feed the AUTH computation.
type IKEKeys struct {
	SKEYSEED []byte
	SK_d     []byte
	SK_ai    []byte
	SK_ar    []byte
	SK_ei    []byte
	SK_er    []byte
	SK_pi    []byte
	SK_pr    []byte
}

func wipeIKEKeys(keys *IKEKeys) {
	if keys == nil {
		return
	}
	crypto.Wipe(keys.SKEYSEED)
	crypto.Wipe(keys.SK_d)
	crypto.Wipe(keys.SK_ai)
	crypto.Wipe(keys.SK_ar)
	crypto.Wipe(keys.SK_ei)
	crypto.Wipe(keys.SK_er)
	crypto.Wipe(keys.SK_pi)
	crypto.Wipe(keys.SK_pr)
}

func (s *Session) clearIKEKeyMaterial() {
	s.mu.Lock()
	active, retired := s.ikeKeys, s.retiredIKESA
	dh, dhSecret := s.dh, s.dhSharedSecret
	prfKey := s.prfKey
	s.ikeKeys, s.retiredIKESA, s.retiredIKEDelete = nil, nil, nil
	s.dh, s.dhSharedSecret, s.prfKey = nil, nil, nil
	s.mu.Unlock()
	wipeIKEKeys(active)
	if retired != nil && retired.keys != active {
		wipeIKEKeys(retired.keys)
	}
	crypto.Wipe(dhSecret)
	crypto.Wipe(prfKey)
	if dh != nil {
		crypto.Wipe(dh.SharedKey)
	}
}

// GenerateIKESAKeys derives the IKE SA keys from the IKE_SA_INIT exchange.
// responderNonce is Nr (the responder's nonce); the DH shared secret (g^ir)
// must already be stored on the session.
//
// For a PRF with a 16-byte output (e.g. AES-XCBC-PRF-128), the nonces used as
// the SKEYSEED key are truncated to 8 bytes each, matching the decompiled
// implementation.
func (s *Session) GenerateIKESAKeys(responderNonce []byte) error {
	if s.dhSharedSecret == nil {
		return errors.New("no DH shared secret")
	}
	if s.prf == nil {
		return errors.New("no PRF configured")
	}

	prfOut := crypto.PRFOutputSize(s.prf)
	ni, nr := s.Ni, responderNonce
	if prfOut == 16 {
		if len(ni) > 8 {
			ni = ni[:8]
		}
		if len(nr) > 8 {
			nr = nr[:8]
		}
	}

	// SKEYSEED = prf(Ni | Nr, g^ir).
	skeyseedKey := append(append([]byte{}, ni...), nr...)
	skeyseed := s.prf.Compute(skeyseedKey, s.dhSharedSecret)
	crypto.Wipe(skeyseedKey)
	defer crypto.Wipe(skeyseed)

	// prf+ seed = full Ni | Nr | SPIi | SPIr.
	seed := append(append([]byte{}, s.Ni...), responderNonce...)
	seed = append(seed, s.SPIi[:]...)
	seed = append(seed, s.SPIr[:]...)
	defer crypto.Wipe(seed)

	keys, err := s.deriveIKEKeys(skeyseed, seed, prfOut)
	if err != nil {
		return err
	}
	s.ikeKeys = keys
	return nil
}

// GenerateIKESARekeyKeys restores the original explicit IKE rekey derivation
// API. It returns new key material without mutating the active IKE SA.
func (s *Session) GenerateIKESARekeyKeys(
	oldSKd []byte,
	newDHSecret []byte,
	initiatorNonce []byte,
	responderNonce []byte,
	newSPIi uint64,
	newSPIr uint64,
) (*IKEKeys, error) {
	return s.deriveIKESARekeyKeys(
		oldSKd, newDHSecret, initiatorNonce, responderNonce,
		ikeSPIBytes(newSPIi), ikeSPIBytes(newSPIr),
	)
}

// RegenerateIKESARekeyKeys retains the additive in-place helper used by the
// current session API.
func (s *Session) RegenerateIKESARekeyKeys(initiatorNonce, responderNonce []byte) error {
	if s.ikeKeys == nil || len(s.ikeKeys.SK_d) == 0 {
		return errors.New("no previous IKE SA keys for rekey")
	}
	if s.prf == nil {
		return errors.New("no PRF configured")
	}

	keys, err := s.deriveIKESARekeyKeys(
		s.ikeKeys.SK_d, s.dhSharedSecret,
		initiatorNonce, responderNonce, s.SPIi, s.SPIr,
	)
	if err != nil {
		return err
	}
	s.ikeKeys = keys
	return nil
}

func (s *Session) deriveIKESARekeyKeys(
	oldSKd, sharedSecret, initiatorNonce, responderNonce []byte,
	initiatorSPI, responderSPI [8]byte,
) (*IKEKeys, error) {
	if len(oldSKd) == 0 || len(sharedSecret) == 0 {
		return nil, errors.New("incomplete IKE SA rekey key material")
	}
	if len(initiatorNonce) == 0 || len(responderNonce) == 0 {
		return nil, errors.New("incomplete IKE SA rekey nonces")
	}
	if s.prf == nil {
		return nil, errors.New("no PRF configured")
	}
	prfOut := crypto.PRFOutputSize(s.prf)

	// SKEYSEED_rekey = prf(SK_d(old), g^ir(new) | Ni | Nr).
	rekeyData := append([]byte{}, sharedSecret...)
	rekeyData = append(rekeyData, initiatorNonce...)
	rekeyData = append(rekeyData, responderNonce...)
	skeyseed := s.prf.Compute(oldSKd, rekeyData)
	crypto.Wipe(rekeyData)
	defer crypto.Wipe(skeyseed)

	seed := append(append([]byte{}, initiatorNonce...), responderNonce...)
	seed = append(seed, initiatorSPI[:]...)
	seed = append(seed, responderSPI[:]...)
	defer crypto.Wipe(seed)

	return s.deriveIKEKeys(skeyseed, seed, prfOut)
}

// deriveIKEKeys runs prf+ and slices the output into the seven IKE SA keys.
func (s *Session) deriveIKEKeys(skeyseed, seed []byte, prfOut int) (*IKEKeys, error) {
	total := 3*prfOut + 2*s.integKeyLen + 2*s.encKeyLen
	km, err := crypto.PrfPlus(s.prf, skeyseed, seed, total)
	if err != nil {
		return nil, err
	}
	if len(km) < total {
		crypto.Wipe(km)
		return nil, errors.New("prf+ produced insufficient key material")
	}
	defer crypto.Wipe(km)

	keys := &IKEKeys{SKEYSEED: append([]byte{}, skeyseed...)}
	off := 0
	keys.SK_d = sliceCopy(km, off, prfOut)
	off += prfOut
	keys.SK_ai = sliceCopy(km, off, s.integKeyLen)
	off += s.integKeyLen
	keys.SK_ar = sliceCopy(km, off, s.integKeyLen)
	off += s.integKeyLen
	keys.SK_ei = sliceCopy(km, off, s.encKeyLen)
	off += s.encKeyLen
	keys.SK_er = sliceCopy(km, off, s.encKeyLen)
	off += s.encKeyLen
	keys.SK_pi = sliceCopy(km, off, prfOut)
	off += prfOut
	keys.SK_pr = sliceCopy(km, off, prfOut)
	return keys, nil
}

// sliceCopy returns an independent copy of km[off:off+n].
func sliceCopy(km []byte, off, n int) []byte {
	if n <= 0 {
		return nil
	}
	out := make([]byte, n)
	copy(out, km[off:off+n])
	return out
}
