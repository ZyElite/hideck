package swu

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

type fragmentPacketSpec struct {
	number       uint16
	total        uint16
	messageID    uint32
	exchangeType ikev2.ExchangeType
	flags        uint8
	firstPayload ikev2.PayloadType
}

type fragmentProtection struct {
	cipher       enginecrypto.PreparedCipher
	integrity    enginecrypto.IntegrityAlgorithm
	integrityKey []byte
	iv           []byte
	padded       []byte
	authSize     int
	cipherSize   int
}

// buildSKFPacket restores the original single-fragment wire constructor.
func (s *Session) buildSKFPacket(
	plaintext []byte,
	fragNum uint16,
	totalFrags uint16,
	msgID uint32,
	exchangeType ikev2.ExchangeType,
	firstPayloadType ikev2.PayloadType,
) ([]byte, error) {
	return s.buildSKFPacketWithFlags(plaintext, fragmentPacketSpec{
		number: fragNum, total: totalFrags, messageID: msgID,
		exchangeType: exchangeType, flags: s.localIKEFlags(false),
		firstPayload: firstPayloadType,
	})
}

func (s *Session) buildSKFPacketWithFlags(
	plaintext []byte,
	spec fragmentPacketSpec,
) ([]byte, error) {
	if spec.number == 0 || spec.total == 0 || spec.number > spec.total || spec.total > maxFragments {
		return nil, fmt.Errorf("invalid IKE fragment %d/%d", spec.number, spec.total)
	}
	nextPayload := ikev2.NoNextPayload
	if spec.number == 1 {
		nextPayload = spec.firstPayload
	}
	protection, err := s.prepareFragmentProtection(plaintext, spec.flags)
	if err != nil {
		return nil, err
	}
	prefix, err := s.buildSKFPrefix(spec, nextPayload, protection)
	if err != nil {
		return nil, err
	}
	return s.sealSKF(prefix, protection)
}

func (s *Session) prepareFragmentProtection(
	plaintext []byte,
	flags uint8,
) (*fragmentProtection, error) {
	cipher, integrityKey, integrity, err := s.fragmentCipherMaterial(flags)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, cipher.IVSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generate SKF IV: %w", err)
	}
	padded := padIKEPlaintext(plaintext, cipher.BlockSize())
	protection := &fragmentProtection{
		cipher: cipher, integrity: integrity, integrityKey: integrityKey,
		iv: iv, padded: padded, authSize: aesGCMTagLength, cipherSize: len(padded),
	}
	if s.aead {
		protection.cipherSize += aesGCMTagLength
	} else {
		protection.authSize = integrity.OutputSize()
	}
	return protection, nil
}

func (s *Session) buildSKFPrefix(
	spec fragmentPacketSpec,
	nextPayload ikev2.PayloadType,
	protection *fragmentProtection,
) ([]byte, error) {
	payloadLength := 4 + 4 + len(protection.iv) + protection.cipherSize
	if !s.aead {
		payloadLength += protection.authSize
	}
	if payloadLength > int(^uint16(0)) {
		return nil, errors.New("SKF payload exceeds uint16 length")
	}
	s.mu.RLock()
	initiatorSPI, responderSPI := s.spiI, s.spiR
	s.mu.RUnlock()
	header := newIKEHeader(
		initiatorSPI, responderSPI, spec.exchangeType, spec.flags, spec.messageID,
	)
	header.NextPayload = ikev2.EncryptedFragment
	header.Length = uint32(ikev2.IKE_HEADER_LEN + payloadLength)
	generic := (&ikev2.PayloadHeader{
		NextPayload: nextPayload, PayloadLength: uint16(payloadLength),
	}).Encode()
	fragmentHeader := make([]byte, 4)
	binary.BigEndian.PutUint16(fragmentHeader[0:2], spec.number)
	binary.BigEndian.PutUint16(fragmentHeader[2:4], spec.total)
	return append(append(header.Encode(), generic...), fragmentHeader...), nil
}

func (s *Session) sealSKF(prefix []byte, protection *fragmentProtection) ([]byte, error) {
	aad := []byte(nil)
	if s.aead {
		aad = prefix
	}
	ciphertext, err := protection.cipher.Seal(nil, protection.padded, protection.iv, aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt SKF: %w", err)
	}
	packet := append(append(append([]byte{}, prefix...), protection.iv...), ciphertext...)
	if !s.aead {
		packet = append(packet, protection.integrity.Compute(protection.integrityKey, packet)...)
	}
	if len(packet) != int(binary.BigEndian.Uint32(prefix[24:28])) {
		return nil, errors.New("SKF encryption output length mismatch")
	}
	return packet, nil
}

func (s *Session) fragmentCipherMaterial(
	flags uint8,
) (enginecrypto.PreparedCipher, []byte, enginecrypto.IntegrityAlgorithm, error) {
	if s.ikeKeys == nil {
		return nil, nil, nil, errors.New("swu: no IKE SA keys")
	}
	fromResponder := flags&ikeInitiatorFlag == 0
	encryptionKey, integrityKey := s.ikeProtectionKeys(fromResponder)
	cipher, err := enginecrypto.PrepareCipher(s.encrAlg, encryptionKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("prepare SKF cipher: %w", err)
	}
	if s.aead {
		return cipher, integrityKey, nil, nil
	}
	integrity := enginecrypto.NewIntegrity(s.integAlg)
	if integrity == nil {
		return nil, nil, nil, errors.New("swu: no SKF integrity algorithm")
	}
	return cipher, integrityKey, integrity, nil
}
