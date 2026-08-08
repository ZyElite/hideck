package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// IKEv2 encryption transform IDs used by the original engine.
const (
	EncrDESCBC   uint16 = 2
	Encr3DESCBC  uint16 = 3
	EncrNull     uint16 = 11
	EncrAESCBC   uint16 = 12
	EncrAESGCM8  uint16 = 18
	EncrAESGCM12 uint16 = 19
	EncrAESGCM16 uint16 = 20
)

// Encrypter is the original stateless encryption transform contract.
type Encrypter interface {
	Encrypt(plaintext, key, iv, aad []byte) ([]byte, error)
	Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error)
	IVSize() int
	BlockSize() int
	KeySize() int
}

// AppendEncrypter avoids an intermediate allocation when appending output.
type AppendEncrypter interface {
	EncryptTo(dst, plaintext, key, iv, aad []byte) ([]byte, error)
	DecryptTo(dst, ciphertext, key, iv, aad []byte) ([]byte, error)
}

// CipherPreparer compiles key material into a reusable cipher.
type CipherPreparer interface {
	Prepare(key []byte) (PreparedCipher, error)
}

// PreparedCipher is safe to reuse with a new packet IV on each operation.
type PreparedCipher interface {
	Seal(dst, plaintext, iv, aad []byte) ([]byte, error)
	Open(dst, ciphertext, iv, aad []byte) ([]byte, error)
	IVSize() int
	BlockSize() int
}

type fallbackPreparedCipher struct {
	enc Encrypter
	key []byte
}

// PrepareCipher accepts both the original Encrypter form and the later
// transform-ID form retained by this tree.
func PrepareCipher(algorithm any, key []byte) (PreparedCipher, error) {
	enc, err := resolveEncrypter(algorithm, key)
	if err != nil {
		return nil, err
	}
	if preparer, ok := enc.(CipherPreparer); ok {
		return preparer.Prepare(key)
	}
	return &fallbackPreparedCipher{enc: enc, key: key}, nil
}

func resolveEncrypter(algorithm any, key []byte) (Encrypter, error) {
	if algorithm == nil {
		return nil, nil
	}
	if enc, ok := algorithm.(Encrypter); ok {
		return enc, nil
	}
	var id uint16
	switch selector := algorithm.(type) {
	case uint16:
		id = selector
	case int:
		if selector < 0 || selector > int(^uint16(0)) {
			return nil, fmt.Errorf("crypto: invalid encryption selector %d", selector)
		}
		id = uint16(selector)
	default:
		return nil, fmt.Errorf("crypto: invalid encryption selector %T", algorithm)
	}
	return encrypterForKey(id, key)
}

func encrypterForKey(id uint16, key []byte) (Encrypter, error) {
	keyBits := len(key) * 8
	if isAESGCM(id) {
		keyBits = (len(key) - 4) * 8
	}
	if id == EncrNull || id == EncrDESCBC || id == Encr3DESCBC {
		keyBits = 0
	}
	return GetEncrypterWithKeyLen(id, keyBits)
}

func isAESGCM(id uint16) bool {
	return id == EncrAESGCM8 || id == EncrAESGCM12 || id == EncrAESGCM16
}

func (f *fallbackPreparedCipher) Seal(dst, plaintext, iv, aad []byte) ([]byte, error) {
	return EncryptTo(f.enc, dst, plaintext, f.key, iv, aad)
}

func (f *fallbackPreparedCipher) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return DecryptTo(f.enc, dst, ciphertext, f.key, iv, aad)
}

func (f *fallbackPreparedCipher) IVSize() int {
	if f == nil || f.enc == nil {
		return 0
	}
	return f.enc.IVSize()
}

func (f *fallbackPreparedCipher) BlockSize() int {
	if f == nil || f.enc == nil {
		return 0
	}
	return f.enc.BlockSize()
}

func EncryptTo(enc Encrypter, dst, plaintext, key, iv, aad []byte) ([]byte, error) {
	if appender, ok := enc.(AppendEncrypter); ok {
		return appender.EncryptTo(dst, plaintext, key, iv, aad)
	}
	result, err := enc.Encrypt(plaintext, key, iv, aad)
	if err != nil {
		return dst, err
	}
	return append(dst, result...), nil
}

func DecryptTo(enc Encrypter, dst, ciphertext, key, iv, aad []byte) ([]byte, error) {
	if appender, ok := enc.(AppendEncrypter); ok {
		return appender.DecryptTo(dst, ciphertext, key, iv, aad)
	}
	result, err := enc.Decrypt(ciphertext, key, iv, aad)
	if err != nil {
		return dst, err
	}
	return append(dst, result...), nil
}

func GetEncrypter(id uint16) (Encrypter, error) {
	return GetEncrypterWithKeyLen(id, 0)
}

func GetEncrypterWithKeyLen(id uint16, keyLenBits int) (Encrypter, error) {
	keySize := 16
	if keyLenBits != 0 {
		if keyLenBits%8 != 0 {
			return nil, errors.New("无效的密钥长度")
		}
		keySize = keyLenBits / 8
	}
	switch id {
	case EncrAESCBC:
		return &aesCBC{keySize: keySize}, nil
	case Encr3DESCBC:
		return &tripleDESCBC{}, nil
	case EncrDESCBC:
		return &desCBC{}, nil
	case EncrNull:
		return &nullEncryption{}, nil
	case EncrAESGCM16, EncrAESGCM12, EncrAESGCM8:
		if keyLenBits == 0 {
			return nil, errors.New("AES-GCM 密钥长度未指定（keyLenBits=0），无法安全初始化加密器")
		}
		return &aesGCM{icvSize: 16, keySize: keySize}, nil
	default:
		return nil, errors.New("不支持的加密算法")
	}
}

func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, b)
	return b, err
}
