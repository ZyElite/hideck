package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/rand"
	"fmt"
)

// IKEv2 ENCR transform IDs (RFC 7296 §3.3.2).
const (
	EncrNull     uint16 = 1
	EncrDESCBC   uint16 = 2
	Encr3DESCBC  uint16 = 3
	EncrAESCBC   uint16 = 12
	EncrAESGCM16 uint16 = 18
	EncrAESGCM12 uint16 = 19
	EncrAESGCM8  uint16 = 20
)

// PreparedCipher is a ready-to-use IKEv2/ESP encryption transform.
type PreparedCipher interface {
	// Seal encrypts plaintext with the given IV and AAD, appending to dst.
	Seal(dst, plaintext, iv, aad []byte) []byte
	// Open decrypts ciphertext with the given IV and AAD, appending to dst.
	Open(dst, ciphertext, iv, aad []byte) ([]byte, error)
	// IVSize is the IV length carried in the packet.
	IVSize() int
	// BlockSize is the block size used for padding.
	BlockSize() int
}

// PrepareCipher builds an encryption transform for the given ENCR transform
// ID. Returns an error for unsupported transforms.
//
// AES-GCM follows RFC 4106/5282: the key material is K|salt (the 4-byte salt
// is the last 4 bytes), the packet carries an 8-byte IV and the GCM nonce is
// salt||IV (12 bytes).
func PrepareCipher(transformID uint16, key []byte) (PreparedCipher, error) {
	switch transformID {
	case EncrNull:
		return &nullEncryption{}, nil
	case EncrAESCBC:
		return newAESCBC(key)
	case Encr3DESCBC:
		return newPrepared3DESCBC(key)
	case EncrAESGCM16, EncrAESGCM12, EncrAESGCM8:
		return newAESGCM(key)
	default:
		return nil, fmt.Errorf("crypto: unsupported ENCR transform %d", transformID)
	}
}

type prepared3DESCBC struct {
	block cipher.Block
}

func newPrepared3DESCBC(key []byte) (PreparedCipher, error) {
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, err
	}
	return &prepared3DESCBC{block: block}, nil
}

func (c *prepared3DESCBC) Seal(dst, plaintext, iv, aad []byte) []byte {
	if len(plaintext) == 0 {
		return append(dst, plaintext...)
	}
	if len(plaintext)%des.BlockSize != 0 {
		return dst
	}
	out := append(dst, plaintext...)
	cipher.NewCBCEncrypter(c.block, iv).CryptBlocks(out[len(dst):], out[len(dst):])
	return out
}

func (c *prepared3DESCBC) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return append(dst, ciphertext...), nil
	}
	if len(ciphertext)%des.BlockSize != 0 {
		return dst, fmt.Errorf("crypto: bad 3DES ciphertext length %d", len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(c.block, iv).CryptBlocks(out, ciphertext)
	return append(dst, out...), nil
}

func (*prepared3DESCBC) IVSize() int    { return des.BlockSize }
func (*prepared3DESCBC) BlockSize() int { return des.BlockSize }

// GetEncrypterWithKeyLen returns a prepared AES-CBC or AES-GCM transform with
// the given key.
func GetEncrypterWithKeyLen(transformID uint16, key []byte) (PreparedCipher, error) {
	return PrepareCipher(transformID, key)
}

// RandomBytes returns n cryptographically random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// nullEncryption passes data through unmodified (ENCR_NULL).
type nullEncryption struct{}

func (*nullEncryption) Seal(dst, plaintext, iv, aad []byte) []byte {
	return append(dst, plaintext...)
}
func (*nullEncryption) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return append(dst, ciphertext...), nil
}
func (*nullEncryption) IVSize() int    { return 0 }
func (*nullEncryption) BlockSize() int { return 0 }

// aesGCM is ENCR_AES_GCM_* (RFC 4106 ESP / RFC 5282 IKEv2).
//
// The key material is K|salt: the AES key is key[:len-4] and the last 4 bytes
// form the salt. The packet IV is 8 bytes and the GCM nonce is salt||IV.
type aesGCM struct {
	aead   cipher.AEAD // 12-byte nonce
	salt   [4]byte
	keyLen int // AES key length (16/24/32)
}

func newAESGCM(key []byte) (PreparedCipher, error) {
	if len(key) < 4 {
		return nil, fmt.Errorf("crypto: AES-GCM key too short (%d bytes)", len(key))
	}
	blk, err := aes.NewCipher(key[:len(key)-4])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	g := &aesGCM{aead: aead, keyLen: len(key) - 4}
	copy(g.salt[:], key[len(key)-4:])
	return g, nil
}

// nonce builds the 12-byte GCM nonce as salt||IV.
func (g *aesGCM) nonce(iv []byte) []byte {
	nonce := make([]byte, 12)
	copy(nonce, g.salt[:])
	copy(nonce[4:], iv)
	return nonce
}

func (g *aesGCM) Seal(dst, plaintext, iv, aad []byte) []byte {
	return g.aead.Seal(dst, g.nonce(iv), plaintext, aad)
}
func (g *aesGCM) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return g.aead.Open(dst, g.nonce(iv), ciphertext, aad)
}
func (g *aesGCM) IVSize() int    { return 8 }
func (g *aesGCM) BlockSize() int { return aes.BlockSize }

// preparedCBC is ENCR_AES_CBC (RFC 3602).
type preparedCBC struct {
	key []byte
	iv  []byte
}

func newAESCBC(key []byte) (PreparedCipher, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("crypto: bad AES-CBC key length %d", len(key))
	}
	return &preparedCBC{key: key}, nil
}

// preparedCBC is raw AES-CBC: the caller is responsible for padding the
// plaintext to a block multiple (IKEv2/ESP apply their own padding schemes).
func (c *preparedCBC) Seal(dst, plaintext, iv, aad []byte) []byte {
	if len(plaintext) == 0 {
		return append(dst, plaintext...)
	}
	if len(plaintext)%aes.BlockSize != 0 {
		return dst
	}
	blk, err := aes.NewCipher(c.key)
	if err != nil {
		return dst
	}
	// Copy the plaintext into dst first so the source is not clobbered when
	// plaintext aliases dst's tail (ESP encrypts in place).
	out := append(dst, plaintext...)
	mode := cipher.NewCBCEncrypter(blk, iv)
	mode.CryptBlocks(out[len(dst):], out[len(dst):])
	return out
}

func (c *preparedCBC) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return append(dst, ciphertext...), nil
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return dst, fmt.Errorf("crypto: bad ciphertext length %d", len(ciphertext))
	}
	blk, err := aes.NewCipher(c.key)
	if err != nil {
		return dst, err
	}
	out := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(blk, iv)
	mode.CryptBlocks(out, ciphertext)
	return append(dst, out...), nil
}

func (c *preparedCBC) IVSize() int    { return aes.BlockSize }
func (c *preparedCBC) BlockSize() int { return aes.BlockSize }

// ---------------------------------------------------------------------------
// Legacy Cipher interface methods on the shared types
// ---------------------------------------------------------------------------

// Prepare loads the secret key (required by the legacy Cipher interface).
func (n *nullEncryption) Prepare(key []byte) error { return nil }
func (n *nullEncryption) Encrypt(dst, iv, plaintext []byte) []byte {
	return append(dst, plaintext...)
}
func (n *nullEncryption) Decrypt(dst, iv, ciphertext []byte) ([]byte, error) {
	return append(dst, ciphertext...), nil
}
func (n *nullEncryption) EncryptTo(dst, iv, plaintext []byte) []byte {
	return append(dst, plaintext...)
}
func (n *nullEncryption) DecryptTo(dst, iv, ciphertext []byte) ([]byte, error) {
	return append(dst, ciphertext...), nil
}
func (n *nullEncryption) KeySize() int { return 0 }

// Prepare loads the secret key for AES-GCM (RFC 4106: K|salt).
func (g *aesGCM) Prepare(key []byte) error {
	prepared, err := newAESGCM(key)
	if err != nil {
		return err
	}
	*g = *(prepared.(*aesGCM))
	return nil
}
func (g *aesGCM) Encrypt(dst, iv, plaintext []byte) []byte {
	return g.Seal(dst, plaintext, iv, nil)
}
func (g *aesGCM) Decrypt(dst, iv, ciphertext []byte) ([]byte, error) {
	return g.Open(dst, ciphertext, iv, nil)
}
func (g *aesGCM) EncryptTo(dst, iv, plaintext []byte) []byte {
	return g.Seal(dst, plaintext, iv, nil)
}
func (g *aesGCM) DecryptTo(dst, iv, ciphertext []byte) ([]byte, error) {
	return g.Open(dst, ciphertext, iv, nil)
}
func (g *aesGCM) KeySize() int { return g.keyLen }

// EncryptTo encrypts plaintext with the given IV/AAD using a prepared cipher
// (alias of Seal with the recovered EncryptTo name).
func EncryptTo(c PreparedCipher, dst, plaintext, iv, aad []byte) []byte {
	if c == nil {
		return nil
	}
	return c.Seal(dst, plaintext, iv, aad)
}

// DecryptTo decrypts ciphertext with the given IV/AAD (alias of Open).
func DecryptTo(c PreparedCipher, dst, ciphertext, iv, aad []byte) ([]byte, error) {
	if c == nil {
		return nil, nil
	}
	return c.Open(dst, ciphertext, iv, aad)
}

// preparedGCM is the prepared (Seal/Open) AES-GCM transform recovered from the
// binary as a distinct type from the legacy aesGCM.
type preparedGCM struct {
	inner *aesGCM
}

// newPreparedGCM builds a prepared AES-GCM transform.
func newPreparedGCM(key []byte) (*preparedGCM, error) {
	g, err := newAESGCM(key)
	if err != nil {
		return nil, err
	}
	inner, ok := g.(*aesGCM)
	if !ok {
		return nil, fmt.Errorf("crypto: unexpected GCM type")
	}
	return &preparedGCM{inner: inner}, nil
}

// Seal encrypts plaintext, appending to dst.
func (p *preparedGCM) Seal(dst, plaintext, iv, aad []byte) []byte {
	return p.inner.Seal(dst, plaintext, iv, aad)
}

// Open decrypts ciphertext, appending to dst.
func (p *preparedGCM) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return p.inner.Open(dst, ciphertext, iv, aad)
}

// IVSize returns the IV length carried in the packet.
func (p *preparedGCM) IVSize() int { return p.inner.IVSize() }

// BlockSize returns the block size used for padding.
func (p *preparedGCM) BlockSize() int { return p.inner.BlockSize() }

// fallbackPreparedCipher wraps a legacy Cipher to satisfy PreparedCipher.
type fallbackPreparedCipher struct {
	cipher Cipher
}

// newFallbackPreparedCipher wraps a legacy cipher.
func newFallbackPreparedCipher(c Cipher) *fallbackPreparedCipher {
	return &fallbackPreparedCipher{cipher: c}
}

// Seal encrypts via the legacy cipher (IV is the explicit IV).
func (f *fallbackPreparedCipher) Seal(dst, plaintext, iv, aad []byte) []byte {
	return f.cipher.Encrypt(dst, iv, plaintext)
}

// Open decrypts via the legacy cipher.
func (f *fallbackPreparedCipher) Open(dst, ciphertext, iv, aad []byte) ([]byte, error) {
	return f.cipher.Decrypt(dst, iv, ciphertext)
}

// IVSize returns the IV length of the legacy cipher.
func (f *fallbackPreparedCipher) IVSize() int { return f.cipher.IVSize() }

// BlockSize returns the block size of the legacy cipher.
func (f *fallbackPreparedCipher) BlockSize() int { return f.cipher.BlockSize() }
