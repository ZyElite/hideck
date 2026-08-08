package swu

import (
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const (
	ikeResponseFlag        = 0x20
	ikeInitiatorFlag       = 0x08
	ikeHeaderLength        = 28
	ikePayloadHeaderLength = 4
	aesGCMTagLength        = 16
)

func (s *Session) ikeProtectionKeys(fromResponder bool) (encryption, integrity []byte) {
	if fromResponder {
		return s.ikeKeys.SK_er, s.ikeKeys.SK_ar
	}
	return s.ikeKeys.SK_ei, s.ikeKeys.SK_ai
}

func (s *Session) localIKEFlags(response bool) byte {
	s.mu.RLock()
	initiator := s.localIKEInitiator
	s.mu.RUnlock()
	var flags byte
	if initiator {
		flags |= ikeInitiatorFlag
	}
	if response {
		flags |= ikeResponseFlag
	}
	return flags
}

func firstIKEPayloadType(payloads []ikev2.Payload) byte {
	if len(payloads) == 0 {
		return ikev2.PayloadNoNext
	}
	return payloads[0].Type()
}

func protectedIKEPacket(source *ikev2.IKEPacket, messageID uint32, first byte, body []byte) *ikev2.IKEPacket {
	return &ikev2.IKEPacket{
		InitiatorSPI: source.InitiatorSPI, ResponderSPI: source.ResponderSPI,
		Version: source.Version, ExchangeType: source.ExchangeType,
		Flags: source.Flags, MessageID: messageID,
		Payloads: []ikev2.Payload{ikev2.NewEncryptedPayloadSK(first, body)},
	}
}

func padIKEPlaintext(data []byte, blockSize int) []byte {
	if blockSize <= 0 {
		blockSize = 1
	}
	paddingLength := (blockSize - (len(data)+1)%blockSize) % blockSize
	out := append([]byte(nil), data...)
	for i := 0; i < paddingLength; i++ {
		out = append(out, byte(i+1))
	}
	return append(out, byte(paddingLength))
}

func unpadIKEPlaintext(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("swu: empty encrypted IKE plaintext")
	}
	paddingLength := int(data[len(data)-1])
	if paddingLength+1 > len(data) {
		return nil, fmt.Errorf("swu: invalid IKE padding length %d", paddingLength)
	}
	return append([]byte(nil), data[:len(data)-paddingLength-1]...), nil
}

func (s *Session) encryptAEADIKE(source *ikev2.IKEPacket, messageID uint32, first byte, plain, iv []byte, cipher enginecrypto.PreparedCipher) ([]byte, error) {
	bodyLength := len(iv) + len(plain) + aesGCMTagLength
	template := protectedIKEPacket(source, messageID, first, make([]byte, bodyLength)).Encode()
	aad := template[:ikeHeaderLength+ikePayloadHeaderLength]
	encrypted, err := cipher.Seal(nil, plain, iv, aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt IKE AEAD message: %w", err)
	}
	body := append(append([]byte{}, iv...), encrypted...)
	return protectedIKEPacket(source, messageID, first, body).Encode(), nil
}

func (s *Session) decryptAEADIKE(packet *ikev2.IKEPacket, encrypted *ikev2.EncryptedPayloadSK, cipher enginecrypto.PreparedCipher) ([]ikev2.Payload, error) {
	if len(encrypted.Data) < cipher.IVSize()+aesGCMTagLength {
		return nil, errors.New("swu: encrypted AEAD payload too short")
	}
	raw := packet.Encode()
	aad := raw[:ikeHeaderLength+ikePayloadHeaderLength]
	iv := encrypted.Data[:cipher.IVSize()]
	ciphertext := encrypted.Data[cipher.IVSize():]
	padded, err := cipher.Open(nil, ciphertext, iv, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt IKE AEAD message: %w", err)
	}
	plain, err := unpadIKEPlaintext(padded)
	if err != nil {
		return nil, err
	}
	return ikev2.DecodePayloadChainWithFirst(encrypted.NextPayload, plain)
}
