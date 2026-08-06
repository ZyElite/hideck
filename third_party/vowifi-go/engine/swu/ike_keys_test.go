package swu

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

// newKeyDerivationSession builds a Session wired for HMAC-SHA1 / AES-128-CBC
// / HMAC-SHA1-96 key derivation.
func newKeyDerivationSession(t *testing.T) *Session {
	t.Helper()
	s := &Session{
		SPIi:        [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		SPIr:        [8]byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
		Ni:          bytes.Repeat([]byte{0xa1}, 16),
		prf:         crypto.NewPRF(2), // PRF_HMAC_SHA1
		prfKey:      bytes.Repeat([]byte{0xaa}, 20),
		integKeyLen: 12, // HMAC-SHA1-96
		encKeyLen:   16, // AES-128-CBC
		dhSharedSecret: bytes.Repeat([]byte{0xdd}, 128),
	}
	return s
}

func TestGenerateIKESAKeysLengths(t *testing.T) {
	s := newKeyDerivationSession(t)
	nr := bytes.Repeat([]byte{0xb2}, 16)
	if err := s.GenerateIKESAKeys(nr); err != nil {
		t.Fatalf("GenerateIKESAKeys: %v", err)
	}
	k := s.ikeKeys
	if k == nil {
		t.Fatal("ikeKeys not set")
	}
	if len(k.SK_d) != 20 || len(k.SK_pi) != 20 || len(k.SK_pr) != 20 {
		t.Errorf("SK_d/SK_pi/SK_pr length = %d/%d/%d, want 20", len(k.SK_d), len(k.SK_pi), len(k.SK_pr))
	}
	if len(k.SK_ai) != 12 || len(k.SK_ar) != 12 {
		t.Errorf("SK_ai/SK_ar length = %d/%d, want 12", len(k.SK_ai), len(k.SK_ar))
	}
	if len(k.SK_ei) != 16 || len(k.SK_er) != 16 {
		t.Errorf("SK_ei/SK_er length = %d/%d, want 16", len(k.SK_ei), len(k.SK_er))
	}
}

func TestGenerateIKESAKeysSKEYSEED(t *testing.T) {
	// SKEYSEED = prf(Ni | Nr, g^ir) = HMAC-SHA1(key=Ni|Nr, data=g^ir).
	s := newKeyDerivationSession(t)
	nr := bytes.Repeat([]byte{0xb2}, 16)
	if err := s.GenerateIKESAKeys(nr); err != nil {
		t.Fatalf("GenerateIKESAKeys: %v", err)
	}
	key := append(append([]byte{}, s.Ni...), nr...)
	mac := hmac.New(sha1.New, key)
	mac.Write(s.dhSharedSecret)
	want := mac.Sum(nil)
	if !bytes.Equal(s.ikeKeys.SKEYSEED, want) {
		t.Errorf("SKEYSEED = %x, want %x", s.ikeKeys.SKEYSEED, want)
	}
}

func TestGenerateIKESAKeysDeterminism(t *testing.T) {
	nr := bytes.Repeat([]byte{0xb2}, 16)
	s1 := newKeyDerivationSession(t)
	s1.GenerateIKESAKeys(nr)
	s2 := newKeyDerivationSession(t)
	s2.GenerateIKESAKeys(nr)
	if !bytes.Equal(s1.ikeKeys.SK_ei, s2.ikeKeys.SK_ei) || !bytes.Equal(s1.ikeKeys.SK_d, s2.ikeKeys.SK_d) {
		t.Error("key derivation not deterministic for identical inputs")
	}

	// Different Nr → different keys.
	s3 := newKeyDerivationSession(t)
	s3.GenerateIKESAKeys(bytes.Repeat([]byte{0xc3}, 16))
	if bytes.Equal(s1.ikeKeys.SK_d, s3.ikeKeys.SK_d) {
		t.Error("different Nr produced identical SK_d")
	}
}

func TestGenerateIKESAKeysKeyIndependence(t *testing.T) {
	s := newKeyDerivationSession(t)
	s.GenerateIKESAKeys(bytes.Repeat([]byte{0xb2}, 16))
	k := s.ikeKeys
	// Keys must be distinct and not alias each other.
	if bytes.Equal(k.SK_ai, k.SK_ar) || bytes.Equal(k.SK_ei, k.SK_er) || bytes.Equal(k.SK_pi, k.SK_pr) {
		t.Error("derived keys are not distinct")
	}
	// Mutating one key must not affect another (independent copies).
	k.SK_ei[0] ^= 0xff
	if bytes.Equal(k.SK_ei, k.SK_er) {
		t.Error("keys alias the same backing array")
	}
}

func TestGenerateIKESAKeysAEAD(t *testing.T) {
	// AEAD mode: no separate integrity (integKeyLen=0), encKeyLen includes
	// the 4-byte GCM salt.
	s := &Session{
		SPIi:           [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		SPIr:           [8]byte{8, 7, 6, 5, 4, 3, 2, 1},
		Ni:             bytes.Repeat([]byte{0xa1}, 16),
		prf:            crypto.NewPRF(2),
		integKeyLen:    0,
		encKeyLen:      20, // 16-byte AES key + 4-byte salt
		aead:           true,
		dhSharedSecret: bytes.Repeat([]byte{0xdd}, 128),
	}
	if err := s.GenerateIKESAKeys(bytes.Repeat([]byte{0xb2}, 16)); err != nil {
		t.Fatalf("GenerateIKESAKeys AEAD: %v", err)
	}
	if len(s.ikeKeys.SK_ai) != 0 || len(s.ikeKeys.SK_ar) != 0 {
		t.Errorf("AEAD SK_ai/SK_ar should be empty, got %d/%d", len(s.ikeKeys.SK_ai), len(s.ikeKeys.SK_ar))
	}
	if len(s.ikeKeys.SK_ei) != 20 || len(s.ikeKeys.SK_er) != 20 {
		t.Errorf("AEAD SK_ei/SK_er length = %d/%d, want 20", len(s.ikeKeys.SK_ei), len(s.ikeKeys.SK_er))
	}
}

func TestGenerateIKESAKeysTruncationFor16BytePRF(t *testing.T) {
	// AES-XCBC-PRF-128 has a 16-byte output: the SKEYSEED key uses only the
	// first 8 bytes of each nonce.
	s := &Session{
		SPIi:           [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		SPIr:           [8]byte{8, 7, 6, 5, 4, 3, 2, 1},
		Ni:             bytes.Repeat([]byte{0xa1}, 32),
		prf:            crypto.NewPRF(4), // PRF_AES128_XCBC
		integKeyLen:    12,
		encKeyLen:      16,
		dhSharedSecret: bytes.Repeat([]byte{0xdd}, 128),
	}
	nr := bytes.Repeat([]byte{0xb2}, 32)
	if err := s.GenerateIKESAKeys(nr); err != nil {
		t.Fatalf("GenerateIKESAKeys: %v", err)
	}
	// Recompute SKEYSEED with truncated nonces (8 bytes each).
	key := append(append([]byte{}, s.Ni[:8]...), nr[:8]...)
	want := s.prf.Compute(key, s.dhSharedSecret)
	if !bytes.Equal(s.ikeKeys.SKEYSEED, want) {
		t.Errorf("SKEYSEED with 16-byte PRF truncation = %x, want %x", s.ikeKeys.SKEYSEED, want)
	}
}

func TestGenerateIKESARekeyKeys(t *testing.T) {
	s := newKeyDerivationSession(t)
	if err := s.GenerateIKESAKeys(bytes.Repeat([]byte{0xb2}, 16)); err != nil {
		t.Fatalf("initial: %v", err)
	}
	oldSKd := append([]byte{}, s.ikeKeys.SK_d...)
	oldSKdCopy := append([]byte{}, s.ikeKeys.SK_d...)

	ni2 := bytes.Repeat([]byte{0xe1}, 16)
	nr2 := bytes.Repeat([]byte{0xe2}, 16)
	if err := s.GenerateIKESARekeyKeys(ni2, nr2); err != nil {
		t.Fatalf("rekey: %v", err)
	}
	// SKEYSEED_rekey = prf(SK_d, Ni|Nr).
	rekeyData := append(append([]byte{}, ni2...), nr2...)
	want := s.prf.Compute(oldSKdCopy, rekeyData)
	if !bytes.Equal(s.ikeKeys.SKEYSEED, want) {
		t.Errorf("rekey SKEYSEED = %x, want %x", s.ikeKeys.SKEYSEED, want)
	}
	// Rekey produced a fresh SK_d.
	if bytes.Equal(s.ikeKeys.SK_d, oldSKd) {
		t.Error("rekey did not produce a new SK_d")
	}
}

func TestGenerateIKESAKeysErrors(t *testing.T) {
	s := newKeyDerivationSession(t)
	s.dhSharedSecret = nil
	if err := s.GenerateIKESAKeys(bytes.Repeat([]byte{0xb2}, 16)); err == nil {
		t.Error("missing DH secret should error")
	}
	s.dhSharedSecret = bytes.Repeat([]byte{0xdd}, 128)
	s.prf = nil
	if err := s.GenerateIKESAKeys(bytes.Repeat([]byte{0xb2}, 16)); err == nil {
		t.Error("missing PRF should error")
	}
}