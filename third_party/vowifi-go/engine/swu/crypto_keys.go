package swu

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// encryptAndWrap encrypts and integrity-protects an IKE message (RFC 7296
// §3.2) and returns the full wire packet. The payload chain is encoded as the
// plaintext, encrypted with SK_ei (initiator) and integrity-protected with
// SK_ai, then wrapped in an Encrypted payload.
func (s *Session) encryptAndWrap(pkt *ikev2.IKEPacket) ([]byte, error) {
	return s.encryptAndWrapWithMsgID(pkt, pkt.MessageID)
}

// encryptAndWrapWithMsgID encrypts the payload chain with an explicit message
// ID (used for retransmissions and rekey exchanges).
func (s *Session) encryptAndWrapWithMsgID(pkt *ikev2.IKEPacket, msgID uint32) ([]byte, error) {
	if s.ikeKeys == nil {
		return nil, errors.New("swu: no IKE SA keys")
	}
	plain := ikev2.EncodePayloadChain(pkt.Payloads)

	encKey := s.ikeKeys.SK_ei
	integKey := s.ikeKeys.SK_ai
	if pkt.Flags&0x20 != 0 { // responder
		encKey = s.ikeKeys.SK_er
		integKey = s.ikeKeys.SK_ar
	}

	cipher, err := crypto.PrepareCipher(s.encrAlg, encKey)
	if err != nil {
		return nil, fmt.Errorf("prepare cipher: %w", err)
	}

	// Encrypt: for CBC a random IV is prepended; for AEAD the salt is part of
	// the key and the IV is carried in the packet.
	iv := make([]byte, cipher.IVSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("generate IV: %w", err)
	}
	encrypted := cipher.Seal(nil, plain, iv, nil)

	// Integrity over the encrypted payload (RFC 7296 §3.2: the checksum covers
	// the Encrypted payload header + encrypted data).
	integ := crypto.NewIntegrity(s.integAlg)
	if integ == nil {
		return nil, errors.New("swu: no integrity algorithm")
	}
	checksum := integ.Compute(integKey, encrypted)

	// The Encrypted payload body is: IV | encrypted | checksum.
	body := append(append([]byte{}, iv...), encrypted...)
	body = append(body, checksum...)

	// Rebuild the packet with the Encrypted payload and encode.
	out := &ikev2.IKEPacket{
		InitiatorSPI: pkt.InitiatorSPI,
		ResponderSPI: pkt.ResponderSPI,
		Version:      pkt.Version,
		ExchangeType: pkt.ExchangeType,
		Flags:        pkt.Flags,
		MessageID:    msgID,
		Payloads: []ikev2.Payload{
			ikev2.NewRawPayload(ikev2.PayloadEncrypted, body),
		},
	}
	return out.Encode(), nil
}

// decryptAndParse decrypts and parses an incoming IKE message. The packet must
// carry a single Encrypted payload; the decrypted payload chain is returned.
func (s *Session) decryptAndParse(pkt *ikev2.IKEPacket) ([]ikev2.Payload, error) {
	if s.ikeKeys == nil {
		return nil, errors.New("swu: no IKE SA keys")
	}
	if len(pkt.Payloads) != 1 {
		return nil, errors.New("swu: expected a single Encrypted payload")
	}
	enc, ok := pkt.Payloads[0].(*ikev2.RawPayload)
	if !ok || enc.Type() != ikev2.PayloadEncrypted {
		return nil, errors.New("swu: payload is not Encrypted")
	}

	encKey := s.ikeKeys.SK_er
	integKey := s.ikeKeys.SK_ar
	if pkt.Flags&0x20 != 0 { // responder
		encKey = s.ikeKeys.SK_ei
		integKey = s.ikeKeys.SK_ai
	}

	cipher, err := crypto.PrepareCipher(s.encrAlg, encKey)
	if err != nil {
		return nil, fmt.Errorf("prepare cipher: %w", err)
	}
	integ := crypto.NewIntegrity(s.integAlg)
	if integ == nil {
		return nil, errors.New("swu: no integrity algorithm")
	}

	ivSize := cipher.IVSize()
	integSize := integ.OutputSize()
	if len(enc.Data) < ivSize+integSize {
		return nil, errors.New("swu: encrypted payload too short")
	}
	iv := enc.Data[:ivSize]
	ct := enc.Data[ivSize : len(enc.Data)-integSize]
	checksum := enc.Data[len(enc.Data)-integSize:]

	// Verify integrity.
	if !integ.Verify(integKey, enc.Data[:len(enc.Data)-integSize], checksum) {
		return nil, errors.New("swu: IKE message integrity check failed")
	}

	plain, err := cipher.Open(nil, ct, iv, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt IKE message: %w", err)
	}
	return ikev2.DecodePayloadChain(plain)
}

// decryptSKF decrypts a stored SKF (encrypted IKE_AUTH response) body.
func (s *Session) decryptSKF(body []byte) ([]ikev2.Payload, error) {
	if s.ikeKeys == nil {
		return nil, errors.New("swu: no IKE SA keys")
	}
	cipher, err := crypto.PrepareCipher(s.encrAlg, s.ikeKeys.SK_er)
	if err != nil {
		return nil, fmt.Errorf("prepare cipher: %w", err)
	}
	integ := crypto.NewIntegrity(s.integAlg)
	if integ == nil {
		return nil, errors.New("swu: no integrity algorithm")
	}
	ivSize := cipher.IVSize()
	integSize := integ.OutputSize()
	if len(body) < ivSize+integSize {
		return nil, errors.New("swu: SKF too short")
	}
	iv := body[:ivSize]
	ct := body[ivSize : len(body)-integSize]
	checksum := body[len(body)-integSize:]
	if !integ.Verify(integKeyFor(s, false), body[:len(body)-integSize], checksum) {
		return nil, errors.New("swu: SKF integrity check failed")
	}
	plain, err := cipher.Open(nil, ct, iv, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt SKF: %w", err)
	}
	return ikev2.DecodePayloadChain(plain)
}

// buildSKFPacket wraps an encrypted payload chain into an IKE packet.
func (s *Session) buildSKFPacket(pkt *ikev2.IKEPacket, body []byte) *ikev2.IKEPacket {
	return &ikev2.IKEPacket{
		InitiatorSPI: pkt.InitiatorSPI,
		ResponderSPI: pkt.ResponderSPI,
		Version:      pkt.Version,
		ExchangeType: pkt.ExchangeType,
		Flags:        pkt.Flags,
		MessageID:    pkt.MessageID,
		Payloads: []ikev2.Payload{
			ikev2.NewRawPayload(ikev2.PayloadEncrypted, body),
		},
	}
}

// integKeyFor returns the responder/initiator integrity key.
func integKeyFor(s *Session, responder bool) []byte {
	if responder {
		return s.ikeKeys.SK_ar
	}
	return s.ikeKeys.SK_ai
}
