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

// PRF is the original IKEv2 pseudo-random-function contract.
type PRF interface {
	Compute(key, data []byte) []byte
	KeyLen() int
}

// SizedPRF retains the later output-size API without narrowing PRF.
type SizedPRF interface {
	PRF
	OutputSize() int
}

type hmacPRF struct {
	compute func(key, data []byte) []byte
	keyLen  int
}

func (h *hmacPRF) Compute(key, data []byte) []byte { return h.compute(key, data) }
func (h *hmacPRF) KeyLen() int                     { return h.keyLen }
func (h *hmacPRF) OutputSize() int                 { return len(h.Compute(nil, nil)) }

type xcbcPRF struct{}

func (*xcbcPRF) Compute(key, data []byte) []byte { return aesXCBCPRF128(key, data) }
func (*xcbcPRF) KeyLen() int                     { return 16 }
func (*xcbcPRF) OutputSize() int                 { return 16 }

var (
	PRF_HMAC_MD5      = newHMACPRF(md5.New, 16)
	PRF_HMAC_SHA1     = newHMACPRF(sha1.New, 20)
	PRF_HMAC_SHA2_256 = newHMACPRF(sha256.New, 32)
	PRF_HMAC_SHA2_384 = newHMACPRF(sha512.New384, 48)
	PRF_HMAC_SHA2_512 = newHMACPRF(sha512.New, 64)
	PRF_AES128_XCBC   = &xcbcPRF{}
)

func newHMACPRF(newHash func() hash.Hash, keyLen int) *hmacPRF {
	return &hmacPRF{
		compute: func(key, data []byte) []byte { return computeHMAC(newHash, key, data) },
		keyLen:  keyLen,
	}
}

func PrfPlus(prf PRF, key, seed []byte, totalBytes int) ([]byte, error) {
	var result []byte
	var lastBlock []byte
	for blockIndex := 1; len(result) < totalBytes; blockIndex++ {
		input := make([]byte, 0, len(lastBlock)+len(seed)+1)
		if blockIndex > 1 {
			input = append(input, lastBlock...)
		}
		input = append(input, seed...)
		input = append(input, byte(blockIndex))
		lastBlock = prf.Compute(key, input)
		result = append(result, lastBlock...)
		if blockIndex >= 255 {
			return nil, errors.New("PRF+ 溢出: 块太多")
		}
	}
	return result[:totalBytes], nil
}

func GetPRF(id uint16) (PRF, error) {
	switch id {
	case 1:
		return PRF_HMAC_MD5, nil
	case 2:
		return PRF_HMAC_SHA1, nil
	case 4:
		return PRF_AES128_XCBC, nil
	case 5:
		return PRF_HMAC_SHA2_256, nil
	case 6:
		return PRF_HMAC_SHA2_384, nil
	case 7:
		return PRF_HMAC_SHA2_512, nil
	default:
		return nil, errors.New("不支持的 PRF ID")
	}
}

func NewPRF(id uint16) SizedPRF {
	prf, err := GetPRF(id)
	if err != nil {
		return nil
	}
	return prf.(SizedPRF)
}

func PRFOutputSize(prf PRF) int {
	if sized, ok := prf.(interface{ OutputSize() int }); ok {
		return sized.OutputSize()
	}
	return len(prf.Compute(nil, nil))
}

func computeHMAC(newHash func() hash.Hash, key, data []byte) []byte {
	h := hmac.New(newHash, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func ComputeHMAC(newHash func() hash.Hash, key, data []byte) []byte {
	return computeHMAC(newHash, key, data)
}

func VerifyHMAC(newHash func() hash.Hash, key, data, expected []byte) bool {
	computed := computeHMAC(newHash, key, data)
	return hmac.Equal(computed, expected[:len(computed)])
}
