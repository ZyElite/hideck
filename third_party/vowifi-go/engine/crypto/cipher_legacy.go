package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"fmt"
)

// Cipher is the (legacy) IKEv2 encryption transform interface used by the
// original engine/crypto alongside PreparedCipher. Each implementation is
// stateful: Prepare loads the key material, then Encrypt/Decrypt operate on
// the IV passed in (CBC) or stored (GCM).
type Cipher interface {
	// Prepare loads the secret key (16/24/32 bytes for AES, 8 for DES,
	// 24 for 3DES).
	Prepare(key []byte) error
	// Encrypt appends the encryption of plaintext to dst.
	Encrypt(dst, iv, plaintext []byte) []byte
	// Decrypt appends the decryption of ciphertext to dst.
	Decrypt(dst, iv, ciphertext []byte) ([]byte, error)
	// IVSize is the IV length in bytes.
	IVSize() int
	// BlockSize is the cipher block size (0 for stream/GCM).
	BlockSize() int
	// KeySize is the accepted key length in bytes.
	KeySize() int
}

// EncryptTo encrypts plaintext in place using the stateful cipher (CBC).
// DecryptTo is provided for symmetry with the original API surface.
func (c *aesCBC) EncryptTo(dst, iv, plaintext []byte) []byte   { return c.Encrypt(dst, iv, plaintext) }
func (c *aesCBC) DecryptTo(dst, iv, ct []byte) ([]byte, error) { return c.Decrypt(dst, iv, ct) }

// pkcs7Pad/pkcs7Unpad are used by the legacy (stateful) CBC Cipher path only;
// the raw PreparedCipher CBC path leaves padding to the caller.
func pkcs7Pad(b []byte, blockSize int) []byte {
	n := blockSize - len(b)%blockSize
	pad := make([]byte, len(b)+n)
	copy(pad, b)
	for i := len(b); i < len(pad); i++ {
		pad[i] = byte(n)
	}
	return pad
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("crypto: empty padded data")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > len(b) || n > aes.BlockSize {
		return nil, fmt.Errorf("crypto: bad padding")
	}
	for _, c := range b[len(b)-n:] {
		if int(c) != n {
			return nil, fmt.Errorf("crypto: bad padding")
		}
	}
	return b[:len(b)-n], nil
}

// NewCipher returns the legacy encryption transform for an ENCR transform ID,
// with key material loaded.
func NewCipher(transformID uint16, key []byte) (Cipher, error) {
	var c Cipher
	switch transformID {
	case EncrNull:
		c = &nullEncryption{}
	case EncrAESCBC:
		c = &aesCBC{}
	case EncrDESCBC:
		c = &desCBC{}
	case Encr3DESCBC:
		c = &tripleDESCBC{}
	case EncrAESGCM16, EncrAESGCM12, EncrAESGCM8:
		c = &aesGCM{}
	default:
		return nil, fmt.Errorf("crypto: unsupported ENCR transform %d", transformID)
	}
	if err := c.Prepare(key); err != nil {
		return nil, err
	}
	return c, nil
}

// aesCBC is the legacy AES-CBC transform.
type aesCBC struct {
	block cipher.Block
}

func (c *aesCBC) Prepare(key []byte) error {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	c.block = blk
	return nil
}

func (c *aesCBC) Encrypt(dst, iv, plaintext []byte) []byte {
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	out := append(dst, padded...)
	mode := cipher.NewCBCEncrypter(c.block, iv)
	mode.CryptBlocks(out[len(dst):], padded)
	return out
}

func (c *aesCBC) Decrypt(dst, iv, ciphertext []byte) ([]byte, error) {
	if c.block == nil {
		return dst, fmt.Errorf("crypto: aesCBC not prepared")
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return dst, fmt.Errorf("crypto: bad ciphertext length %d", len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(c.block, iv)
	mode.CryptBlocks(out, ciphertext)
	unpadded, err := pkcs7Unpad(out)
	if err != nil {
		return dst, err
	}
	return append(dst, unpadded...), nil
}

func (c *aesCBC) IVSize() int    { return aes.BlockSize }
func (c *aesCBC) BlockSize() int { return aes.BlockSize }
func (c *aesCBC) KeySize() int   { return 16 }

// desCBC is the legacy DES-CBC transform (ENCR_3DES_CBC keyed with the
// 8-byte DES key; present for completeness).
type desCBC struct {
	block cipher.Block
}

func (c *desCBC) Prepare(key []byte) error {
	blk, err := des.NewCipher(key)
	if err != nil {
		return err
	}
	c.block = blk
	return nil
}

func (c *desCBC) Encrypt(dst, iv, plaintext []byte) []byte {
	padded := pkcs7Pad(plaintext, des.BlockSize)
	out := append(dst, padded...)
	mode := cipher.NewCBCEncrypter(c.block, iv)
	mode.CryptBlocks(out[len(dst):], padded)
	return out
}

func (c *desCBC) Decrypt(dst, iv, ciphertext []byte) ([]byte, error) {
	if c.block == nil {
		return dst, fmt.Errorf("crypto: desCBC not prepared")
	}
	if len(ciphertext) == 0 || len(ciphertext)%des.BlockSize != 0 {
		return dst, fmt.Errorf("crypto: bad ciphertext length %d", len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(c.block, iv)
	mode.CryptBlocks(out, ciphertext)
	unpadded, err := pkcs7Unpad(out)
	if err != nil {
		return dst, err
	}
	return append(dst, unpadded...), nil
}

func (c *desCBC) IVSize() int    { return des.BlockSize }
func (c *desCBC) BlockSize() int { return des.BlockSize }
func (c *desCBC) KeySize() int   { return 8 }

func (c *desCBC) EncryptTo(dst, iv, pt []byte) []byte          { return c.Encrypt(dst, iv, pt) }
func (c *desCBC) DecryptTo(dst, iv, ct []byte) ([]byte, error) { return c.Decrypt(dst, iv, ct) }

// tripleDESCBC is the legacy 3DES-CBC transform (ENCR_3DES_CBC).
type tripleDESCBC struct {
	block cipher.Block
}

func (c *tripleDESCBC) Prepare(key []byte) error {
	blk, err := des.NewTripleDESCipher(key)
	if err != nil {
		return err
	}
	c.block = blk
	return nil
}

func (c *tripleDESCBC) Encrypt(dst, iv, plaintext []byte) []byte {
	padded := pkcs7Pad(plaintext, des.BlockSize)
	out := append(dst, padded...)
	mode := cipher.NewCBCEncrypter(c.block, iv)
	mode.CryptBlocks(out[len(dst):], padded)
	return out
}

func (c *tripleDESCBC) Decrypt(dst, iv, ciphertext []byte) ([]byte, error) {
	if c.block == nil {
		return dst, fmt.Errorf("crypto: tripleDESCBC not prepared")
	}
	if len(ciphertext) == 0 || len(ciphertext)%des.BlockSize != 0 {
		return dst, fmt.Errorf("crypto: bad ciphertext length %d", len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(c.block, iv)
	mode.CryptBlocks(out, ciphertext)
	unpadded, err := pkcs7Unpad(out)
	if err != nil {
		return dst, err
	}
	return append(dst, unpadded...), nil
}

func (c *tripleDESCBC) IVSize() int    { return des.BlockSize }
func (c *tripleDESCBC) BlockSize() int { return des.BlockSize }
func (c *tripleDESCBC) KeySize() int   { return 24 }

func (c *tripleDESCBC) EncryptTo(dst, iv, pt []byte) []byte          { return c.Encrypt(dst, iv, pt) }
func (c *tripleDESCBC) DecryptTo(dst, iv, ct []byte) ([]byte, error) { return c.Decrypt(dst, iv, ct) }
