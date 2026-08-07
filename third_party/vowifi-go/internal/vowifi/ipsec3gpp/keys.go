package ipsec3gpp

import "errors"

// SecureChannelKeys contains the legacy 3DES encryption key and integrity key.
// It is retained for callers that still negotiate des-ede3-cbc.
type SecureChannelKeys struct {
	EncKey  []byte
	AuthKey []byte
}

// Derive3DESKeyFromCK expands CK to a 24-byte 3DES key and fixes DES parity.
func Derive3DESKeyFromCK(ck []byte) ([]byte, error) {
	if len(ck) < 16 {
		return nil, errors.New("ipsec3gpp: CK too short")
	}
	key := make([]byte, 24)
	copy(key, ck[:16])
	copy(key[16:], ck[:8])
	for i := range key {
		key[i] = fixParity(key[i])
	}
	return key, nil
}

func fixParity(value byte) byte {
	ones := 0
	for bit := 1; bit < 8; bit++ {
		if value&(1<<bit) != 0 {
			ones++
		}
	}
	if ones%2 == 0 {
		return value | 1
	}
	return value &^ 1
}

// DeriveSecureChannelKeys derives keys for the legacy 3DES/HMAC profile.
func DeriveSecureChannelKeys(ck, ik []byte) (*SecureChannelKeys, error) {
	encKey, err := Derive3DESKeyFromCK(ck)
	if err != nil {
		return nil, err
	}
	if len(ik) < 16 {
		return nil, errors.New("ipsec3gpp: IK too short")
	}
	return &SecureChannelKeys{
		EncKey: encKey, AuthKey: append([]byte(nil), ik[:16]...),
	}, nil
}
