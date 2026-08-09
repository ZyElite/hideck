package ipsec3gpp

import (
	"errors"
	"fmt"
	"strings"
)

type SecureChannelKeys struct {
	EncKey  []byte
	AuthKey []byte
}

func Derive3DESKeyFromCK(ck []byte) ([]byte, error) {
	if len(ck) < 16 {
		return nil, errors.New("ipsec3gpp: CK too short for 3DES")
	}
	key := make([]byte, 24)
	copy(key[:16], ck[:16])
	copy(key[16:], ck[:8])
	for index, value := range key {
		key[index] = withOddParity(value)
	}
	return key, nil
}

func withOddParity(value byte) byte {
	value &^= 1
	parity := byte(1)
	for remaining := value; remaining != 0; remaining >>= 1 {
		parity ^= remaining & 1
	}
	return value | parity
}

func DeriveSecureChannelKeys(ck, ik []byte, authAlg, encAlg string) ([]byte, []byte, error) {
	authKey, err := deriveAuthKey(ik, authAlg)
	if err != nil {
		return nil, nil, err
	}
	encKey, err := deriveEncKey(ck, encAlg)
	if err != nil {
		return nil, nil, err
	}
	return authKey, encKey, nil
}

func deriveAuthKey(ik []byte, algorithm string) ([]byte, error) {
	switch normalizedAlgorithm(algorithm) {
	case "hmac(md5)", "hmac-md5-96":
		if len(ik) < 16 {
			return nil, errors.New("ipsec3gpp: IK too short for HMAC-MD5-96")
		}
		return append([]byte(nil), ik[:16]...), nil
	case "hmac(sha1)", "hmac-sha-1-96":
		if len(ik) < 16 {
			return nil, errors.New("ipsec3gpp: IK too short for HMAC-SHA-1-96")
		}
		key := make([]byte, 20)
		copy(key, ik[:16])
		return key, nil
	default:
		return nil, fmt.Errorf("ipsec3gpp: unsupported authentication algorithm %q", algorithm)
	}
}

func deriveEncKey(ck []byte, algorithm string) ([]byte, error) {
	switch normalizedAlgorithm(algorithm) {
	case "", "aes-cbc", "cbc(aes)":
		if len(ck) < 16 {
			return nil, errors.New("ipsec3gpp: CK too short for AES-CBC")
		}
		return append([]byte(nil), ck[:16]...), nil
	case "des3-cbc", "des-ede3-cbc", "cbc(des3_ede)":
		return Derive3DESKeyFromCK(ck)
	case "null", "cipher_null", "ecb(cipher_null)":
		return nil, nil
	default:
		return nil, fmt.Errorf("ipsec3gpp: unsupported encryption algorithm %q", algorithm)
	}
}

func normalizedAlgorithm(algorithm string) string {
	return strings.ToLower(strings.TrimSpace(algorithm))
}

func DeriveLegacySecureChannelKeys(ck, ik []byte) (*SecureChannelKeys, error) {
	authKey, encKey, err := DeriveSecureChannelKeys(ck, ik, AuthHMACSHA196, Encryption3DES)
	if err != nil {
		return nil, err
	}
	return &SecureChannelKeys{EncKey: encKey, AuthKey: authKey}, nil
}
