package ipsec3gpp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
)

func (t *Transport) seal(spi, sequence uint32, nextHeader byte, payload []byte) ([]byte, error) {
	block, err := t.encryptionBlock()
	if err != nil {
		return nil, err
	}
	blockSize := 4
	if block != nil {
		blockSize = block.BlockSize()
	}
	plain := addESPTrailer(payload, nextHeader, blockSize)
	esp := make([]byte, 8)
	binary.BigEndian.PutUint32(esp[:4], spi)
	binary.BigEndian.PutUint32(esp[4:8], sequence)
	if block == nil {
		esp = append(esp, plain...)
	} else {
		iv := make([]byte, block.BlockSize())
		if _, err := rand.Read(iv); err != nil {
			return nil, fmt.Errorf("ipsec3gpp: generate ESP IV: %w", err)
		}
		ciphertext := make([]byte, len(plain))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plain)
		esp = append(esp, iv...)
		esp = append(esp, ciphertext...)
	}
	return append(esp, t.icv(esp)...), nil
}

func (t *Transport) open(espp []byte) ([]byte, byte, error) {
	content := espp[:len(espp)-espICVLength]
	want := espp[len(espp)-espICVLength:]
	if !hmac.Equal(t.icv(content), want) {
		return nil, 0, errors.New("ipsec3gpp: ESP integrity check failed")
	}
	block, err := t.encryptionBlock()
	if err != nil {
		return nil, 0, err
	}
	encoded := content[8:]
	if block != nil {
		if len(encoded) < block.BlockSize()*2 || (len(encoded)-block.BlockSize())%block.BlockSize() != 0 {
			return nil, 0, errors.New("ipsec3gpp: invalid CBC payload length")
		}
		iv, ciphertext := encoded[:block.BlockSize()], encoded[block.BlockSize():]
		plain := make([]byte, len(ciphertext))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
		encoded = plain
	}
	return removeESPTrailer(encoded)
}

func (t *Transport) encryptionBlock() (cipher.Block, error) {
	switch t.policy.Encryption {
	case EncryptionNull:
		return nil, nil
	case EncryptionAES:
		return aes.NewCipher(t.policy.CK[:16])
	case Encryption3DES:
		key, err := Derive3DESKeyFromCK(t.policy.CK)
		if err != nil {
			return nil, err
		}
		return des.NewTripleDESCipher(key)
	default:
		return nil, fmt.Errorf("ipsec3gpp: unsupported encryption %q", t.policy.Encryption)
	}
}

func (t *Transport) icv(packet []byte) []byte {
	// TS 33.203 Annex I expands IKIM to 160 bits with four zero octets.
	key := make([]byte, sha1.Size)
	copy(key, t.policy.IK[:16])
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(packet)
	return mac.Sum(nil)[:espICVLength]
}

func addESPTrailer(payload []byte, nextHeader byte, alignment int) []byte {
	paddingLength := (alignment - (len(payload)+2)%alignment) % alignment
	out := make([]byte, len(payload)+paddingLength+2)
	copy(out, payload)
	for index := 0; index < paddingLength; index++ {
		out[len(payload)+index] = byte(index + 1)
	}
	out[len(out)-2] = byte(paddingLength)
	out[len(out)-1] = nextHeader
	return out
}

func removeESPTrailer(plaintext []byte) ([]byte, byte, error) {
	if len(plaintext) < 2 {
		return nil, 0, errors.New("ipsec3gpp: ESP plaintext too short")
	}
	paddingLength := int(plaintext[len(plaintext)-2])
	if paddingLength+2 > len(plaintext) {
		return nil, 0, errors.New("ipsec3gpp: invalid ESP padding length")
	}
	padding := plaintext[len(plaintext)-2-paddingLength : len(plaintext)-2]
	for index, value := range padding {
		if value != byte(index+1) {
			return nil, 0, errors.New("ipsec3gpp: invalid ESP padding")
		}
	}
	return append([]byte(nil), plaintext[:len(plaintext)-2-paddingLength]...), plaintext[len(plaintext)-1], nil
}

func validatePorts(protocol byte, payload []byte, source, destination uint16) error {
	actualSource, actualDestination, ok := transportPorts(protocol, payload)
	if !ok || actualSource != source || actualDestination != destination {
		return errors.New("ipsec3gpp: decrypted packet missed protected selector")
	}
	return nil
}
