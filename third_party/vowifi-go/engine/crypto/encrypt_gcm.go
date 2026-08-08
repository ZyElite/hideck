package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
)

const (
	gcmSaltSize  = 4
	gcmIVSize    = 8
	gcmNonceSize = gcmSaltSize + gcmIVSize
)

var errGCMKeyTooShort = errors.New("GCM 盐的密钥太短")

type aesGCM struct {
	icvSize int
	keySize int
}

func (*aesGCM) IVSize() int    { return gcmIVSize }
func (*aesGCM) BlockSize() int { return aes.BlockSize }
func (e *aesGCM) KeySize() int { return e.keySize }

func (e *aesGCM) Prepare(key []byte) (PreparedCipher, error) {
	aead, salt, err := prepareGCMKey(key)
	if err != nil {
		return nil, err
	}
	return &preparedGCM{aead: aead, salt: salt}, nil
}

func (e *aesGCM) Encrypt(plaintext, key, iv, aad []byte) ([]byte, error) {
	return e.EncryptTo(nil, plaintext, key, iv, aad)
}

func (e *aesGCM) Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error) {
	return e.DecryptTo(nil, ciphertext, key, iv, aad)
}

func (e *aesGCM) EncryptTo(dst, plaintext, key, iv, aad []byte) ([]byte, error) {
	aead, salt, err := prepareGCMKey(key)
	if err != nil {
		return dst, err
	}
	return aead.Seal(dst, gcmNonce(salt, iv), plaintext, aad), nil
}

func (e *aesGCM) DecryptTo(dst, ciphertext, key, iv, aad []byte) ([]byte, error) {
	aead, salt, err := prepareGCMKey(key)
	if err != nil {
		return dst, err
	}
	return aead.Open(dst, gcmNonce(salt, iv), ciphertext, aad)
}

type preparedGCM struct {
	aead cipher.AEAD
	salt []byte
}

func (g *preparedGCM) Seal(dst, plaintext, iv, aad []byte) ([]byte, error) {
	return g.aead.Seal(dst, gcmNonce(g.salt, iv), plaintext, aad), nil
}

func (g *preparedGCM) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return g.aead.Open(dst, gcmNonce(g.salt, iv), ciphertext, aad)
}

func (*preparedGCM) IVSize() int    { return gcmIVSize }
func (*preparedGCM) BlockSize() int { return aes.BlockSize }

func prepareGCMKey(key []byte) (cipher.AEAD, []byte, error) {
	if len(key) < gcmSaltSize {
		return nil, nil, errGCMKeyTooShort
	}
	realKey := key[:len(key)-gcmSaltSize]
	block, err := aes.NewCipher(realKey)
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, gcmNonceSize)
	if err != nil {
		return nil, nil, err
	}
	salt := append([]byte(nil), key[len(key)-gcmSaltSize:]...)
	return aead, salt, nil
}

func gcmNonce(salt, iv []byte) []byte {
	nonce := make([]byte, gcmNonceSize)
	copy(nonce, salt)
	copy(nonce[gcmSaltSize:], iv)
	return nonce
}

type nullEncryption struct{}

func (*nullEncryption) IVSize() int    { return 0 }
func (*nullEncryption) BlockSize() int { return 4 }
func (*nullEncryption) KeySize() int   { return 0 }

func (e *nullEncryption) Prepare(key []byte) (PreparedCipher, error) { return e, nil }

func (*nullEncryption) Encrypt(plaintext, key, iv, aad []byte) ([]byte, error) {
	return append([]byte(nil), plaintext...), nil
}

func (*nullEncryption) Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error) {
	return append([]byte(nil), ciphertext...), nil
}

func (*nullEncryption) EncryptTo(dst, plaintext, key, iv, aad []byte) ([]byte, error) {
	return append(dst, plaintext...), nil
}

func (*nullEncryption) DecryptTo(dst, ciphertext, key, iv, aad []byte) ([]byte, error) {
	return append(dst, ciphertext...), nil
}

func (*nullEncryption) Seal(dst, plaintext, iv, aad []byte) ([]byte, error) {
	return append(dst, plaintext...), nil
}

func (*nullEncryption) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return append(dst, ciphertext...), nil
}
