package crypto

import "errors"

// Cipher retains the later stateful compatibility API. The original engine
// uses Encrypter and PreparedCipher internally.
type Cipher interface {
	Prepare(key []byte) error
	Encrypt(dst, iv, plaintext []byte) []byte
	Decrypt(dst, iv, ciphertext []byte) ([]byte, error)
	IVSize() int
	BlockSize() int
	KeySize() int
}

type compatibilityCipher struct {
	enc      Encrypter
	prepared PreparedCipher
	padding  bool
}

func NewCipher(transformID uint16, key []byte) (Cipher, error) {
	enc, err := encrypterForKey(transformID, key)
	if err != nil {
		return nil, err
	}
	c := &compatibilityCipher{
		enc:     enc,
		padding: transformID == EncrAESCBC || transformID == EncrDESCBC || transformID == Encr3DESCBC,
	}
	if err := c.Prepare(key); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *compatibilityCipher) Prepare(key []byte) error {
	prepared, err := PrepareCipher(c.enc, key)
	if err != nil {
		return err
	}
	c.prepared = prepared
	return nil
}

func (c *compatibilityCipher) Encrypt(dst, iv, plaintext []byte) []byte {
	input := plaintext
	if c.padding {
		input = pkcs7Pad(plaintext, c.prepared.BlockSize())
	}
	result, err := c.prepared.Seal(dst, input, iv, nil)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *compatibilityCipher) Decrypt(dst, iv, ciphertext []byte) ([]byte, error) {
	plaintext, err := c.prepared.Open(nil, ciphertext, iv, nil)
	if err != nil {
		return dst, err
	}
	if c.padding {
		plaintext, err = pkcs7Unpad(plaintext, c.prepared.BlockSize())
		if err != nil {
			return dst, err
		}
	}
	return append(dst, plaintext...), nil
}

func (c *compatibilityCipher) IVSize() int    { return c.prepared.IVSize() }
func (c *compatibilityCipher) BlockSize() int { return c.prepared.BlockSize() }
func (c *compatibilityCipher) KeySize() int   { return c.enc.KeySize() }

func pkcs7Pad(plaintext []byte, blockSize int) []byte {
	padding := blockSize - len(plaintext)%blockSize
	result := make([]byte, len(plaintext)+padding)
	copy(result, plaintext)
	for i := len(plaintext); i < len(result); i++ {
		result[i] = byte(padding)
	}
	return result
}

func pkcs7Unpad(plaintext []byte, blockSize int) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("crypto: empty padded data")
	}
	padding := int(plaintext[len(plaintext)-1])
	if padding == 0 || padding > len(plaintext) || padding > blockSize {
		return nil, errors.New("crypto: bad padding")
	}
	for _, value := range plaintext[len(plaintext)-padding:] {
		if int(value) != padding {
			return nil, errors.New("crypto: bad padding")
		}
	}
	return plaintext[:len(plaintext)-padding], nil
}
