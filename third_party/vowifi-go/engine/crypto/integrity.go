package crypto

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"hash"
)

type IntegrityAlgorithm interface {
	Compute(key, data []byte) []byte
	Verify(key, data, expectedMAC []byte) bool
	OutputSize() int
	KeySize() int
}

// Integrity retains the later name used by the current ESP implementation.
type Integrity = IntegrityAlgorithm

type hmacSHA1_96 struct{}
type hmacMD5_96 struct{}
type hmacSHA256_128 struct{}
type hmacSHA384_192 struct{}
type hmacSHA512_256 struct{}
type nullIntegrity struct{}
type aesXCBC96 struct{}

func (*hmacSHA1_96) Compute(key, data []byte) []byte {
	return truncatedHMAC(sha1.New, key, data, 12)
}
func (h *hmacSHA1_96) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}
func (*hmacSHA1_96) OutputSize() int { return 12 }
func (*hmacSHA1_96) KeySize() int    { return 20 }

func (*hmacMD5_96) Compute(key, data []byte) []byte {
	return truncatedHMAC(md5.New, key, data, 12)
}
func (h *hmacMD5_96) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}
func (*hmacMD5_96) OutputSize() int { return 12 }
func (*hmacMD5_96) KeySize() int    { return 16 }

func (*hmacSHA256_128) Compute(key, data []byte) []byte {
	return truncatedHMAC(sha256.New, key, data, 16)
}
func (h *hmacSHA256_128) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}
func (*hmacSHA256_128) OutputSize() int { return 16 }
func (*hmacSHA256_128) KeySize() int    { return 32 }

func (*hmacSHA384_192) Compute(key, data []byte) []byte {
	return truncatedHMAC(sha512.New384, key, data, 24)
}
func (h *hmacSHA384_192) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}
func (*hmacSHA384_192) OutputSize() int { return 24 }
func (*hmacSHA384_192) KeySize() int    { return 48 }

func (*hmacSHA512_256) Compute(key, data []byte) []byte {
	return truncatedHMAC(sha512.New, key, data, 32)
}
func (h *hmacSHA512_256) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}
func (*hmacSHA512_256) OutputSize() int { return 32 }
func (*hmacSHA512_256) KeySize() int    { return 64 }

func (*nullIntegrity) Compute(key, data []byte) []byte   { return nil }
func (*nullIntegrity) Verify(key, data, mac []byte) bool { return true }
func (*nullIntegrity) OutputSize() int                   { return 0 }
func (*nullIntegrity) KeySize() int                      { return 0 }

func (*aesXCBC96) Compute(key, data []byte) []byte {
	return aesXCBCMAC(key, data)[:12]
}
func (h *aesXCBC96) Verify(key, data, expected []byte) bool {
	return hmac.Equal(h.Compute(key, data), expected)
}
func (*aesXCBC96) OutputSize() int { return 12 }
func (*aesXCBC96) KeySize() int    { return 16 }

func truncatedHMAC(newHash func() hash.Hash, key, data []byte, size int) []byte {
	return computeHMAC(newHash, key, data)[:size]
}

func GetIntegrityAlgorithm(id uint16) (IntegrityAlgorithm, error) {
	switch id {
	case 0:
		return &nullIntegrity{}, nil
	case 1:
		return &hmacMD5_96{}, nil
	case 2:
		return &hmacSHA1_96{}, nil
	case 5:
		return &aesXCBC96{}, nil
	case 12:
		return &hmacSHA256_128{}, nil
	case 13:
		return &hmacSHA384_192{}, nil
	case 14:
		return &hmacSHA512_256{}, nil
	default:
		return nil, errors.New("不支持的完整性算法")
	}
}

func NewIntegrity(id uint16) IntegrityAlgorithm {
	algorithm, err := GetIntegrityAlgorithm(id)
	if err != nil {
		return nil
	}
	return algorithm
}

func newHmacSHA1_96() IntegrityAlgorithm    { return &hmacSHA1_96{} }
func newHmacMD5_96() IntegrityAlgorithm     { return &hmacMD5_96{} }
func newHmacSHA256_128() IntegrityAlgorithm { return &hmacSHA256_128{} }
func newHmacSHA384_192() IntegrityAlgorithm { return &hmacSHA384_192{} }
func newHmacSHA512_256() IntegrityAlgorithm { return &hmacSHA512_256{} }
func newNullIntegrity() IntegrityAlgorithm  { return &nullIntegrity{} }
func newAesXCBC96() IntegrityAlgorithm      { return &aesXCBC96{} }
