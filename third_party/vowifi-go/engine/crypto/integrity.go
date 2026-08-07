// Package crypto implements the IKEv2 cryptographic transforms used by the
// vowifi SWu (UE <-> ePDG) client.
//
// Reconstructed from the decompiled engine/crypto. This file covers the
// integrity transforms:
//
//	HMAC-SHA1-96     (RFC 2404, 20-byte key, 12-byte output)
//	HMAC-MD5-96      (RFC 2403, 16-byte key, 12-byte output)
//	HMAC-SHA256-128  (RFC 4868, 32-byte key, 16-byte output)
//	HMAC-SHA384-192  (RFC 4868, 48-byte key, 24-byte output)
//	HMAC-SHA512-256  (RFC 4868, 64-byte key, 32-byte output)
//	AES-XCBC-96      (RFC 3566, 16-byte key, 12-byte output)
//	NULL             (RFC 7296, 0-byte key, 0-byte output)
package crypto

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"hash"
)

// Integrity is an IKEv2 integrity algorithm (INTEG transform).
type Integrity interface {
	// Compute returns the authenticator of data with key.
	Compute(key, data []byte) []byte
	// Verify checks data against the expected authenticator.
	Verify(key, data, expected []byte) bool
	// OutputSize is the truncated output length in bytes.
	OutputSize() int
	// KeySize is the key length in bytes.
	KeySize() int
}

// hmacIntegrity is the common HMAC-based implementation, truncated to
// outputSize bytes.
type hmacIntegrity struct {
	newHash func() hash.Hash
	keyLen  int
	outLen  int
}

func (h *hmacIntegrity) Compute(key, data []byte) []byte {
	mac := hmac.New(h.newHash, key)
	mac.Write(data)
	return mac.Sum(nil)[:h.outLen]
}

func (h *hmacIntegrity) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}

func (h *hmacIntegrity) OutputSize() int { return h.outLen }
func (h *hmacIntegrity) KeySize() int    { return h.keyLen }

// hmacSHA1_96 is INTEG_HMAC_SHA1_96 (RFC 2404).
type hmacSHA1_96 struct{ hmacIntegrity }

func newHmacSHA1_96() Integrity {
	return &hmacSHA1_96{hmacIntegrity{newHash: sha1.New, keyLen: 20, outLen: 12}}
}

// hmacMD5_96 is INTEG_HMAC_MD5_96 (RFC 2403).
type hmacMD5_96 struct{ hmacIntegrity }

func newHmacMD5_96() Integrity {
	return &hmacMD5_96{hmacIntegrity{newHash: md5.New, keyLen: 16, outLen: 12}}
}

// hmacSHA256_128 is INTEG_HMAC_SHA2_256_128 (RFC 4868).
type hmacSHA256_128 struct{ hmacIntegrity }

func newHmacSHA256_128() Integrity {
	return &hmacSHA256_128{hmacIntegrity{newHash: sha256.New, keyLen: 32, outLen: 16}}
}

// hmacSHA384_192 is INTEG_HMAC_SHA2_384_192 (RFC 4868).
type hmacSHA384_192 struct{ hmacIntegrity }

func newHmacSHA384_192() Integrity {
	return &hmacSHA384_192{hmacIntegrity{newHash: sha512.New384, keyLen: 48, outLen: 24}}
}

// hmacSHA512_256 is INTEG_HMAC_SHA2_512_256 (RFC 4868).
type hmacSHA512_256 struct{ hmacIntegrity }

func newHmacSHA512_256() Integrity {
	return &hmacSHA512_256{hmacIntegrity{newHash: sha512.New, keyLen: 64, outLen: 32}}
}

// aesXCBC96 is INTEG_AES_XCBC_MAC_96 (RFC 3566).
type aesXCBC96 struct{}

func newAesXCBC96() Integrity { return &aesXCBC96{} }

func (*aesXCBC96) Compute(key, data []byte) []byte {
	return aesXCBCMAC(normalizeXCBCKey(key), data)[:12]
}

func (*aesXCBC96) Verify(key, data, expected []byte) bool {
	return hmac.Equal(aesXCBCMAC(normalizeXCBCKey(key), data)[:12], expected)
}

func (*aesXCBC96) OutputSize() int { return 12 }
func (*aesXCBC96) KeySize() int    { return 16 }

// nullIntegrity is the NULL integrity algorithm (RFC 7296 §3.3.2).
type nullIntegrity struct{}

func newNullIntegrity() Integrity { return &nullIntegrity{} }

func (*nullIntegrity) Compute(key, data []byte) []byte { return nil }
func (*nullIntegrity) Verify(key, data, expected []byte) bool {
	return len(expected) == 0
}
func (*nullIntegrity) OutputSize() int { return 0 }
func (*nullIntegrity) KeySize() int    { return 0 }

// NewIntegrity returns the integrity transform for an IKEv2 INTEG transform
// ID, or nil when unsupported.
func NewIntegrity(transformID uint16) Integrity {
	switch transformID {
	case 1: // INTEG_HMAC_MD5_96
		return newHmacMD5_96()
	case 2: // INTEG_HMAC_SHA1_96
		return newHmacSHA1_96()
	case 3: // INTEG_DES_MAC (unsupported)
		return nil
	case 4: // INTEG_KPDK_MD5 (unsupported)
		return nil
	case 5: // INTEG_AES_XCBC_96
		return newAesXCBC96()
	case 12: // INTEG_HMAC_SHA2_256_128
		return newHmacSHA256_128()
	case 13: // INTEG_HMAC_SHA2_384_192
		return newHmacSHA384_192()
	case 14: // INTEG_HMAC_SHA2_512_256
		return newHmacSHA512_256()
	case 0: // INTEG_NONE (NULL)
		return newNullIntegrity()
	}
	return nil
}

// normalizeXCBCKey pads/truncates the XCBC key to 16 bytes per RFC 3566 §2.
func normalizeXCBCKey(key []byte) []byte {
	if len(key) > 16 {
		return key[:16]
	}
	k := make([]byte, 16)
	copy(k, key)
	return k
}

// aesXCBCMAC computes AES-XCBC-MAC (RFC 3566) of data with a 16-byte key.
//
//	K1 = E(K, 0x0101..01), K2 = E(K, 0x0202..02), K3 = E(K, 0x0303..03)
//	E[0] = 0^128
//	for i < n-1:   E[i] = E_K1(E[i-1] XOR M[i])
//	last block:    E[n] = E_K1(E[n-1] XOR M[n] XOR K2)  if M[n] is full
//	               E[n] = E_K1(E[n-1] XOR pad(M[n]) XOR K3)  otherwise
func aesXCBCMAC(key, data []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil
	}
	var k1, k2, k3 [16]byte
	for i := range k1 {
		k1[i] = 0x01
		k2[i] = 0x02
		k3[i] = 0x03
	}
	block.Encrypt(k1[:], k1[:])
	block.Encrypt(k2[:], k2[:])
	block.Encrypt(k3[:], k3[:])
	b1, _ := aes.NewCipher(k1[:])

	n := len(data)
	full := n / 16 // complete blocks
	state := make([]byte, 16)

	// intermediate complete blocks: all complete blocks when the last block
	// is partial, all but the last when the last block is full.
	intermediate := full
	if n%16 == 0 && n > 0 {
		intermediate = full - 1
	}
	for i := 0; i < intermediate; i++ {
		for j := 0; j < 16; j++ {
			state[j] ^= data[i*16+j]
		}
		b1.Encrypt(state, state)
	}

	// last block
	var last [16]byte
	useK3 := false
	switch {
	case n == 0:
		// empty message: single padded block
		last[0] = 0x80
		useK3 = true
	case n%16 == 0:
		// last block is full: XOR K2
		copy(last[:], data[(full-1)*16:])
	case n%16 != 0:
		// partial last block: pad with 0x80 00..., XOR K3
		copy(last[:], data[full*16:])
		last[n%16] = 0x80
		useK3 = true
	}
	for j := 0; j < 16; j++ {
		state[j] ^= last[j]
		if useK3 {
			state[j] ^= k3[j]
		} else {
			state[j] ^= k2[j]
		}
	}
	b1.Encrypt(state, state)
	return state
}

// Explicit forwarders matching the binary's named integrity types. The
// embedded hmacIntegrity already provides these; the forwarders reproduce the
// binary's exact symbol set.

func (h *hmacMD5_96) Compute(key, data []byte) []byte    { return h.hmacIntegrity.Compute(key, data) }
func (h *hmacMD5_96) Verify(key, data, exp []byte) bool  { return h.hmacIntegrity.Verify(key, data, exp) }
func (h *hmacMD5_96) OutputSize() int                    { return h.hmacIntegrity.OutputSize() }
func (h *hmacMD5_96) KeySize() int                       { return h.hmacIntegrity.KeySize() }

func (h *hmacSHA1_96) Compute(key, data []byte) []byte   { return h.hmacIntegrity.Compute(key, data) }
func (h *hmacSHA1_96) Verify(key, data, exp []byte) bool { return h.hmacIntegrity.Verify(key, data, exp) }
func (h *hmacSHA1_96) OutputSize() int                   { return h.hmacIntegrity.OutputSize() }
func (h *hmacSHA1_96) KeySize() int                      { return h.hmacIntegrity.KeySize() }

func (h *hmacSHA256_128) Compute(key, data []byte) []byte   { return h.hmacIntegrity.Compute(key, data) }
func (h *hmacSHA256_128) Verify(key, data, exp []byte) bool { return h.hmacIntegrity.Verify(key, data, exp) }
func (h *hmacSHA256_128) OutputSize() int                   { return h.hmacIntegrity.OutputSize() }
func (h *hmacSHA256_128) KeySize() int                      { return h.hmacIntegrity.KeySize() }

func (h *hmacSHA384_192) Compute(key, data []byte) []byte   { return h.hmacIntegrity.Compute(key, data) }
func (h *hmacSHA384_192) Verify(key, data, exp []byte) bool { return h.hmacIntegrity.Verify(key, data, exp) }
func (h *hmacSHA384_192) OutputSize() int                   { return h.hmacIntegrity.OutputSize() }
func (h *hmacSHA384_192) KeySize() int                      { return h.hmacIntegrity.KeySize() }

func (h *hmacSHA512_256) Compute(key, data []byte) []byte   { return h.hmacIntegrity.Compute(key, data) }
func (h *hmacSHA512_256) Verify(key, data, exp []byte) bool { return h.hmacIntegrity.Verify(key, data, exp) }
func (h *hmacSHA512_256) OutputSize() int                   { return h.hmacIntegrity.OutputSize() }
func (h *hmacSHA512_256) KeySize() int                      { return h.hmacIntegrity.KeySize() }
