package swu

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// decryptSKF restores the original raw-fragment parser and authenticator.
func (s *Session) decryptSKF(
	data []byte,
) (plaintext []byte, fragNum uint16, totalFrags uint16, msgID uint32, err error) {
	header, generic, err := decodeSKFHeaders(data)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	msgID = header.MessageID
	fragNum = binary.BigEndian.Uint16(data[32:34])
	totalFrags = binary.BigEndian.Uint16(data[34:36])
	if fragNum == 0 || totalFrags == 0 || fragNum > totalFrags || totalFrags > maxFragments {
		return nil, 0, 0, msgID, fmt.Errorf("invalid IKE fragment %d/%d", fragNum, totalFrags)
	}
	if fragNum > 1 && generic.NextPayload != ikev2.NoNextPayload {
		return nil, 0, 0, msgID, errors.New("non-initial IKE fragment declares a next payload")
	}
	plaintext, err = s.decryptSKFBody(data, header)
	return plaintext, fragNum, totalFrags, msgID, err
}

func (s *Session) decryptSKFBody(data []byte, header *ikev2.IKEHeader) ([]byte, error) {
	cipher, integrityKey, integrity, err := s.fragmentCipherMaterial(header.Flags)
	if err != nil {
		return nil, err
	}
	offset := ikev2.IKE_HEADER_LEN + 8
	if len(data) < offset+cipher.IVSize() {
		return nil, errors.New("SKF is too short for IV")
	}
	iv := data[offset : offset+cipher.IVSize()]
	ciphertext := data[offset+cipher.IVSize():]
	aad := []byte(nil)
	if s.aead {
		aad = data[:offset]
	} else {
		integritySize := integrity.OutputSize()
		if len(ciphertext) < integritySize {
			return nil, errors.New("SKF is too short for integrity checksum")
		}
		checksum := ciphertext[len(ciphertext)-integritySize:]
		ciphertext = ciphertext[:len(ciphertext)-integritySize]
		if !integrity.Verify(integrityKey, data[:len(data)-integritySize], checksum) {
			return nil, errors.New("SKF integrity check failed")
		}
	}
	padded, err := cipher.Open(nil, ciphertext, iv, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt SKF: %w", err)
	}
	return unpadIKEPlaintext(padded)
}

func decodeSKFHeaders(data []byte) (*ikev2.IKEHeader, *ikev2.PayloadHeader, error) {
	header, err := ikev2.DecodeHeader(data)
	if err != nil {
		return nil, nil, err
	}
	if header.NextPayload != ikev2.EncryptedFragment || header.Length != uint32(len(data)) {
		return nil, nil, errors.New("invalid SKF IKE header")
	}
	if len(data) < ikev2.IKE_HEADER_LEN+8 {
		return nil, nil, errors.New("SKF is too short for fragment headers")
	}
	generic, err := ikev2.DecodePayloadHeader(data[ikev2.IKE_HEADER_LEN:])
	if err != nil {
		return nil, nil, err
	}
	if generic.Critical || generic.Reserved != 0 || int(generic.PayloadLength) != len(data)-ikev2.IKE_HEADER_LEN {
		return nil, nil, errors.New("invalid SKF generic header")
	}
	return header, generic, nil
}

func (s *Session) normalizeInboundIKE(data []byte) ([]byte, bool, error) {
	header, err := ikev2.DecodeHeader(data)
	if err != nil {
		return nil, false, err
	}
	if header.NextPayload != ikev2.EncryptedFragment {
		return data, true, nil
	}
	plaintext, number, total, messageID, err := s.decryptSKF(data)
	if err != nil {
		s.fragmentBuf.drop(header.MessageID)
		return nil, false, err
	}
	firstPayload := ikev2.PayloadType(data[ikev2.IKE_HEADER_LEN])
	envelope := fragmentEnvelope{
		initiatorSPI: header.SPIi, responderSPI: header.SPIr,
		exchangeType: header.ExchangeType, flags: header.Flags, version: header.Version,
	}
	complete, err := s.fragmentBuf.addReceivedFragment(receivedFragment{
		messageID: messageID, number: number, total: total,
		firstPayload: firstPayload, plaintext: plaintext, envelope: &envelope,
	})
	if err != nil {
		s.fragmentBuf.drop(messageID)
		return nil, false, err
	}
	if !complete {
		return nil, false, nil
	}
	return s.normalizeReassembledIKE(header, messageID)
}

func (s *Session) normalizeReassembledIKE(
	header *ikev2.IKEHeader,
	messageID uint32,
) ([]byte, bool, error) {
	reassembled, firstPayload, err := s.fragmentBuf.reassembleWithFirst(messageID)
	if err != nil {
		return nil, false, err
	}
	payloads, err := ikev2.DecodePayloadChainWithFirst(firstPayload, reassembled)
	if err != nil {
		return nil, false, fmt.Errorf("decode reassembled IKE payloads: %w", err)
	}
	packet := &ikev2.IKEPacket{Header: &ikev2.IKEHeader{
		SPIi: header.SPIi, SPIr: header.SPIr, Version: header.Version,
		ExchangeType: header.ExchangeType, Flags: header.Flags, MessageID: header.MessageID,
	}, Payloads: payloads}
	normalized, err := s.encryptAndWrapWithMsgID(packet, header.MessageID)
	if err != nil {
		return nil, false, fmt.Errorf("normalize reassembled IKE message: %w", err)
	}
	return normalized, true, nil
}
