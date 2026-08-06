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

	prfOut := s.prf.OutputSize()
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
	wipe(skeyseedKey)

	// prf+ seed = full Ni | Nr | SPIi | SPIr.
	seed := append(append([]byte{}, s.Ni...), responderNonce...)
	seed = append(seed, s.SPIi[:]...)
	seed = append(seed, s.SPIr[:]...)

	keys, err := s.deriveIKEKeys(skeyseed, seed, prfOut)
	if err != nil {
		return err
	}
	s.ikeKeys = keys
	return nil
}

// GenerateIKESARekeyKeys derives a fresh IKE SA's keys during an IKE SA rekey
// (RFC 7296 §2.14): SKEYSEED = prf(SK_d, Ni | Nr), using the rekey nonces and
// the new SPIs. The previous IKE SA's SK_d must be present.
func (s *Session) GenerateIKESARekeyKeys(initiatorNonce, responderNonce []byte) error {
	if s.ikeKeys == nil || len(s.ikeKeys.SK_d) == 0 {
		return errors.New("no previous IKE SA keys for rekey")
	}
	if s.prf == nil {
		return errors.New("no PRF configured")
	}

	prfOut := s.prf.OutputSize()
	ni, nr := initiatorNonce, responderNonce
	if prfOut == 16 {
		if len(ni) > 8 {
			ni = ni[:8]
		}
		if len(nr) > 8 {
			nr = nr[:8]
		}
	}

	// SKEYSEED_rekey = prf(SK_d, Ni | Nr).
	rekeyData := append(append([]byte{}, ni...), nr...)
	skeyseed := s.prf.Compute(s.ikeKeys.SK_d, rekeyData)
	wipe(rekeyData)

	seed := append(append([]byte{}, initiatorNonce...), responderNonce...)
	seed = append(seed, s.SPIi[:]...)
	seed = append(seed, s.SPIr[:]...)

	keys, err := s.deriveIKEKeys(skeyseed, seed, prfOut)
	if err != nil {
		return err
	}
	s.ikeKeys = keys
	return nil
}

// deriveIKEKeys runs prf+ and slices the output into the seven IKE SA keys.
func (s *Session) deriveIKEKeys(skeyseed, seed []byte, prfOut int) (*IKEKeys, error) {
	total := 3*prfOut + 2*s.integKeyLen + 2*s.encKeyLen
	km := crypto.PrfPlus(s.prf, skeyseed, seed, total)
	if len(km) < total {
		return nil, errors.New("prf+ produced insufficient key material")
	}

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

// wipe zeroes a key buffer.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}