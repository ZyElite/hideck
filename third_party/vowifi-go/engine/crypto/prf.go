package crypto

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"hash"
)

// PRF is an IKEv2 pseudo-random function (PRF transform).
type PRF interface {
	// Compute returns PRF(key, data).
	Compute(key, data []byte) []byte
	// KeyLen is the PRF key length in bytes.
	KeyLen() int
	// OutputSize is the PRF output length in bytes.
	OutputSize() int
}

// hmacPRF is the HMAC-based PRF (RFC 2104).
type hmacPRF struct {
	newHash func() hash.Hash
	keyLen  int
}

func (p *hmacPRF) Compute(key, data []byte) []byte {
	mac := hmac.New(p.newHash, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func (p *hmacPRF) KeyLen() int      { return p.keyLen }
func (p *hmacPRF) OutputSize() int  { return p.newHash().Size() }

// xcbcPRF is the AES-XCBC-PRF-128 (RFC 4434).
type xcbcPRF struct{}

func (p *xcbcPRF) Compute(key, data []byte) []byte {
	return aesXCBCPRF128(normalizeXCBCKey(key), data)
}

func (p *xcbcPRF) KeyLen() int     { return 16 }
func (p *xcbcPRF) OutputSize() int { return 16 }

// NewPRF returns the PRF for an IKEv2 PRF transform ID.
func NewPRF(transformID uint16) PRF {
	switch transformID {
	case 1: // PRF_HMAC_MD5 (unsupported — retained for completeness)
		return nil
	case 2: // PRF_HMAC_SHA1
		return &hmacPRF{newHash: sha1.New, keyLen: 20}
	case 3: // PRF_HMAC_TIGER (unsupported)
		return nil
	case 4: // PRF_AES128_XCBC
		return &xcbcPRF{}
	case 5: // PRF_HMAC_SHA2_256
		return &hmacPRF{newHash: sha256.New, keyLen: 32}
	case 6: // PRF_HMAC_SHA2_384
		return &hmacPRF{newHash: sha512.New384, keyLen: 48}
	case 7: // PRF_HMAC_SHA2_512
		return &hmacPRF{newHash: sha512.New, keyLen: 64}
	}
	return nil
}

// PrfPlus is the IKEv2 PRF+ expansion (RFC 7296 §2.13):
//
//	PRF+(K, S) = T1 || T2 || ... || Tn
//	where T1 = PRF(K, S || 0x01)
//	      T2 = PRF(K, T1 || S || 0x02)
//	      ...
func PrfPlus(prf PRF, key, seed []byte, outputLen int) []byte {
	out := make([]byte, 0, outputLen)
	var prev []byte
	for counter := byte(1); len(out) < outputLen; counter++ {
		input := make([]byte, 0, len(prev)+len(seed)+1)
		input = append(input, prev...)
		input = append(input, seed...)
		input = append(input, counter)
		prev = prf.Compute(key, input)
		out = append(out, prev...)
	}
	return out[:outputLen]
}

// aesXCBCPRF128 is the AES-XCBC-PRF-128 (RFC 4434): PRF(K, M) = AES-XCBC-MAC(K, M).
func aesXCBCPRF128(key, data []byte) []byte {
	return aesXCBCMAC(key, data)
}

// normalizeXCBCPRFKey returns the XCBC-PRF key material (16 bytes).
func normalizeXCBCPRFKey(key []byte) []byte {
	return normalizeXCBCKey(key)
}

// computeHMAC is a small helper for raw HMAC (used by key derivation).
func computeHMAC(newHash func() hash.Hash, key, data []byte) []byte {
	mac := hmac.New(newHash, key)
	mac.Write(data)
	return mac.Sum(nil)
}

var _ = aes.BlockSize // reserved
