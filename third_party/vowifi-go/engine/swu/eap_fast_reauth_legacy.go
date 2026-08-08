package swu

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	engineeap "github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func (s *Session) captureFastReauthentication(request eapaka.Packet, keys eapaka.Keys) error {
	attributes, present, err := eapaka.DecryptChallengeEncryptedAttributes(request, keys)
	if err != nil || !present {
		return err
	}
	identity, err := eapaka.IdentityStateFromAttributes(attributes)
	if err != nil || identity.NextReauthID == "" {
		return err
	}
	if s.fastReauthCtx == nil {
		s.fastReauthCtx = engineeap.NewFastReauthContext()
	}
	s.fastReauthCtx.SaveReauthData(identity.NextReauthID, keys.MK, keys.KEncr, keys.KAut)
	if s.cfg != nil && s.cfg.OnFastReauthUpdate != nil {
		s.cfg.OnFastReauthUpdate(identity.NextReauthID, keys.MK, keys.KAut, keys.KEncr)
	}
	return nil
}

func (s *Session) handleLegacyFastReauthentication(packet eapaka.Packet, raw []byte) error {
	if s.fastReauthCtx == nil || !s.fastReauthCtx.CanUseReauth() {
		return errors.New("swu: fast reauthentication context not available")
	}
	framed, err := engineeap.Parse(raw)
	if err != nil {
		return fmt.Errorf("swu: parse reauthentication packet: %w", err)
	}
	attributes, err := engineeap.ParseAttributes(framed.Data)
	if err != nil {
		return fmt.Errorf("swu: parse reauthentication attributes: %w", err)
	}
	nonce, counter, err := reauthenticationInputs(attributes)
	if err != nil {
		return err
	}
	if err := verifyLegacyReauthenticationMAC(packet.Type, s.fastReauthCtx.KAut, raw); err != nil {
		return err
	}
	counterTooSmall := counter < s.fastReauthCtx.Counter
	responseData, err := s.fastReauthCtx.BuildReauthResponse(nonce, counter, counterTooSmall)
	if err != nil {
		return err
	}
	response, err := signLegacyReauthentication(packet, responseData, s.fastReauthCtx.KAut)
	if err != nil {
		return err
	}
	s.installLegacyReauthenticationKeys(packet.Type, counterTooSmall)
	return s.sendEAPBytes(response)
}

func reauthenticationInputs(attributes map[uint8]*engineeap.Attribute) ([]byte, uint16, error) {
	nonce, hasNonce := attributes[engineeap.AT_NONCE_S]
	counter, hasCounter := attributes[engineeap.AT_COUNTER]
	mac, hasMAC := attributes[engineeap.AT_MAC]
	if !hasNonce || !hasCounter || !hasMAC {
		return nil, 0, errors.New("swu: reauthentication request missing NONCE_S, COUNTER, or MAC")
	}
	if len(counter.Value) < 2 || len(mac.Value) < 18 {
		return nil, 0, errors.New("swu: malformed reauthentication attributes")
	}
	value := uint16(counter.Value[0])<<8 | uint16(counter.Value[1])
	return nonce.Value, value, nil
}

func verifyLegacyReauthenticationMAC(eapType uint8, key, raw []byte) error {
	framed, err := engineeap.Parse(raw)
	if err != nil {
		return err
	}
	attributes, err := engineeap.ParseAttributes(framed.Data)
	if err != nil {
		return err
	}
	macAttribute, ok := attributes[engineeap.AT_MAC]
	if !ok || len(macAttribute.Value) < 16 {
		return errors.New("swu: reauthentication request missing MAC value")
	}
	received := macAttribute.Value[len(macAttribute.Value)-16:]
	var calculated []byte
	if eapType == eapaka.TypeAKAPrime {
		calculated, err = eapaka.CalculateAKAPrimeMAC(key, raw, nil)
	} else {
		calculated, err = eapaka.CalculateMAC(key, raw, nil)
	}
	if err != nil {
		return fmt.Errorf("swu: verify reauthentication MAC: %w", err)
	}
	if !hmac.Equal(calculated, received) {
		return errors.New("swu: reauthentication MAC mismatch")
	}
	return nil
}

func signLegacyReauthentication(request eapaka.Packet, data, key []byte) ([]byte, error) {
	response := (&engineeap.EAPPacket{
		Code: engineeap.CodeResponse, Identifier: request.Identifier,
		Type: request.Type, Subtype: engineeap.SubtypeReauthentication, Data: data,
	}).Encode()
	var mac []byte
	var err error
	if request.Type == eapaka.TypeAKAPrime {
		mac, err = eapaka.CalculateAKAPrimeMAC(key, response, nil)
	} else {
		mac, err = eapaka.CalculateMAC(key, response, nil)
	}
	if err != nil {
		return nil, err
	}
	macOffset, ok := attributeOffset(data, engineeap.AT_MAC)
	if !ok {
		return nil, errors.New("swu: reauthentication response missing MAC")
	}
	copy(response[8+macOffset+4:], mac)
	return response, nil
}

func attributeOffset(data []byte, attributeType uint8) (int, bool) {
	for offset := 0; offset+2 <= len(data); {
		length := int(data[offset+1]) * 4
		if length == 0 || offset+length > len(data) {
			return 0, false
		}
		if data[offset] == attributeType {
			return offset, true
		}
		offset += length
	}
	return 0, false
}

func (s *Session) installLegacyReauthenticationKeys(eapType uint8, counterTooSmall bool) {
	if counterTooSmall {
		s.eapKeys = eapaka.Keys{}
		return
	}
	if eapType == eapaka.TypeAKAPrime {
		seed := append([]byte("EAP-AKA'"), []byte(s.fastReauthCtx.ReauthID)...)
		material := hmacSHA256Plus(s.fastReauthCtx.MK, seed, 144)
		s.eapKeys = eapaka.Keys{MSK: append([]byte(nil), material[80:144]...)}
		return
	}
	material := enginecrypto.NewFIPS1862PRFSHA1(s.fastReauthCtx.MK).Bytes(nil, 96)
	s.eapKeys = eapaka.Keys{MSK: append([]byte(nil), material[32:96]...)}
}

func hmacSHA256Plus(key, seed []byte, length int) []byte {
	result := make([]byte, 0, length)
	var previous []byte
	for counter := byte(1); len(result) < length; counter++ {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write(previous)
		_, _ = mac.Write(seed)
		_, _ = mac.Write([]byte{counter})
		previous = mac.Sum(nil)
		result = append(result, previous...)
	}
	return result[:length]
}
