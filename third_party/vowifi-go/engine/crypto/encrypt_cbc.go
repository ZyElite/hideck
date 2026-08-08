package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"errors"
)

var (
	errPlaintextAlignment  = errors.New("明文未对齐块")
	errCiphertextAlignment = errors.New("密文未对齐块")
	errDESKeyLength        = errors.New("DES 密钥长度错误")
	errTripleDESKeyLength  = errors.New("3DES 密钥长度错误")
)

type aesCBC struct {
	blockSize int
	keySize   int
}

func (*aesCBC) IVSize() int    { return aes.BlockSize }
func (*aesCBC) BlockSize() int { return aes.BlockSize }
func (e *aesCBC) KeySize() int { return e.keySize }

func (e *aesCBC) Prepare(key []byte) (PreparedCipher, error) {
	return prepareCBC(aes.NewCipher, key, aes.BlockSize)
}

func (e *aesCBC) Encrypt(plaintext, key, iv, aad []byte) ([]byte, error) {
	return e.EncryptTo(nil, plaintext, key, iv, aad)
}

func (e *aesCBC) Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error) {
	return e.DecryptTo(nil, ciphertext, key, iv, aad)
}

func (e *aesCBC) EncryptTo(dst, plaintext, key, iv, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return dst, err
	}
	return encryptCBC(dst, plaintext, iv, block, aes.BlockSize)
}

func (e *aesCBC) DecryptTo(dst, ciphertext, key, iv, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return dst, err
	}
	return decryptCBC(dst, ciphertext, iv, block, aes.BlockSize)
}

type desCBC struct{}

func (*desCBC) IVSize() int    { return des.BlockSize }
func (*desCBC) BlockSize() int { return des.BlockSize }
func (*desCBC) KeySize() int   { return 8 }

func (e *desCBC) Prepare(key []byte) (PreparedCipher, error) {
	if len(key) != e.KeySize() {
		return nil, errDESKeyLength
	}
	return prepareCBC(des.NewCipher, key, des.BlockSize)
}

func (e *desCBC) Encrypt(plaintext, key, iv, aad []byte) ([]byte, error) {
	return e.EncryptTo(nil, plaintext, key, iv, aad)
}

func (e *desCBC) Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error) {
	return e.DecryptTo(nil, ciphertext, key, iv, aad)
}

func (e *desCBC) EncryptTo(dst, plaintext, key, iv, aad []byte) ([]byte, error) {
	if len(key) != e.KeySize() {
		return dst, errDESKeyLength
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return dst, err
	}
	return encryptCBC(dst, plaintext, iv, block, des.BlockSize)
}

func (e *desCBC) DecryptTo(dst, ciphertext, key, iv, aad []byte) ([]byte, error) {
	if len(key) != e.KeySize() {
		return dst, errDESKeyLength
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return dst, err
	}
	return decryptCBC(dst, ciphertext, iv, block, des.BlockSize)
}

type tripleDESCBC struct{}

func (*tripleDESCBC) IVSize() int    { return des.BlockSize }
func (*tripleDESCBC) BlockSize() int { return des.BlockSize }
func (*tripleDESCBC) KeySize() int   { return 24 }

func (e *tripleDESCBC) Prepare(key []byte) (PreparedCipher, error) {
	if len(key) != e.KeySize() {
		return nil, errTripleDESKeyLength
	}
	return prepareCBC(des.NewTripleDESCipher, key, des.BlockSize)
}

func (e *tripleDESCBC) Encrypt(plaintext, key, iv, aad []byte) ([]byte, error) {
	return e.EncryptTo(nil, plaintext, key, iv, aad)
}

func (e *tripleDESCBC) Decrypt(ciphertext, key, iv, aad []byte) ([]byte, error) {
	return e.DecryptTo(nil, ciphertext, key, iv, aad)
}

func (e *tripleDESCBC) EncryptTo(dst, plaintext, key, iv, aad []byte) ([]byte, error) {
	if len(key) != e.KeySize() {
		return dst, errTripleDESKeyLength
	}
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return dst, err
	}
	return encryptCBC(dst, plaintext, iv, block, des.BlockSize)
}

func (e *tripleDESCBC) DecryptTo(dst, ciphertext, key, iv, aad []byte) ([]byte, error) {
	if len(key) != e.KeySize() {
		return dst, errTripleDESKeyLength
	}
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return dst, err
	}
	return decryptCBC(dst, ciphertext, iv, block, des.BlockSize)
}

type preparedCBC struct {
	block     cipher.Block
	blockSize int
}

type blockFactory func([]byte) (cipher.Block, error)

func prepareCBC(factory blockFactory, key []byte, blockSize int) (PreparedCipher, error) {
	block, err := factory(key)
	if err != nil {
		return nil, err
	}
	return &preparedCBC{block: block, blockSize: blockSize}, nil
}

func (c *preparedCBC) Seal(dst, plaintext, iv, aad []byte) ([]byte, error) {
	return encryptCBC(dst, plaintext, iv, c.block, c.blockSize)
}

func (c *preparedCBC) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return decryptCBC(dst, ciphertext, iv, c.block, c.blockSize)
}

func (c *preparedCBC) IVSize() int    { return c.blockSize }
func (c *preparedCBC) BlockSize() int { return c.blockSize }

func encryptCBC(dst, plaintext, iv []byte, block cipher.Block, blockSize int) ([]byte, error) {
	if len(plaintext)%blockSize != 0 {
		return dst, errPlaintextAlignment
	}
	start := len(dst)
	dst = append(dst, plaintext...)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(dst[start:], plaintext)
	return dst, nil
}

func decryptCBC(dst, ciphertext, iv []byte, block cipher.Block, blockSize int) ([]byte, error) {
	if len(ciphertext)%blockSize != 0 {
		return dst, errCiphertextAlignment
	}
	start := len(dst)
	dst = append(dst, ciphertext...)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(dst[start:], ciphertext)
	return dst, nil
}
