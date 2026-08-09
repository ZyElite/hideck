package ts43

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	enginecrypto "github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/eap"
)

func BuildSubscriberID(imsi, mcc, mnc string) string {
	identity := BuildPermanentNAIIdentity(imsi, mcc, mnc)
	if identity == "" {
		return ""
	}
	payload := make([]byte, 5, len(identity)+5)
	payload[0] = 2
	binary.BigEndian.PutUint16(payload[2:4], uint16(len(identity)+5))
	payload[4] = 1
	payload = append(payload, identity...)
	return base64.StdEncoding.EncodeToString(payload)
}

func BuildPermanentNAIIdentity(imsi, mcc, mnc string) string {
	imsi = strings.TrimSpace(imsi)
	mcc = strings.TrimSpace(mcc)
	mnc = strings.TrimSpace(mnc)
	if imsi == "" || mcc == "" || mnc == "" {
		return ""
	}
	return fmt.Sprintf("0%s@nai.epc.mnc%s.mcc%s.3gppnetwork.org", imsi, mnc, mcc)
}

func BuildChallengePayload(
	challenge string,
	provider AKAProvider,
	imsi, _, _, mcc, mnc string,
) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("AKA provider is required")
	}
	packet, attributes, err := parseChallenge(challenge)
	if err != nil {
		return "", err
	}
	rand16, err := last16(attributes[eap.AT_RAND])
	if err != nil {
		return "", err
	}
	autn16, err := last16(attributes[eap.AT_AUTN])
	if err != nil {
		return "", err
	}
	aka, err := provider.CalculateAKAResult(rand16, autn16)
	if err != nil {
		return "", fmt.Errorf("calculate AKA result: %w", err)
	}
	response, err := buildChallengeResponse(packet, attributes, aka, imsi, mcc, mnc)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(response), nil
}

func parseChallenge(value string) (*eap.EAPPacket, map[uint8]*eap.Attribute, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, nil, fmt.Errorf("decode challenge: %w", err)
	}
	packet, err := eap.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("parse challenge: %w", err)
	}
	if packet.Type != eap.TypeAKA || packet.Subtype != eap.SubtypeChallenge {
		return nil, nil, fmt.Errorf("unsupported challenge type/subtype: %d/%d", packet.Type, packet.Subtype)
	}
	attributes, err := eap.ParseAttributes(packet.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse challenge attrs: %w", err)
	}
	if attributes[eap.AT_RAND] == nil {
		return nil, nil, fmt.Errorf("challenge missing AT_RAND")
	}
	if attributes[eap.AT_AUTN] == nil {
		return nil, nil, fmt.Errorf("challenge missing AT_AUTN")
	}
	return packet, attributes, nil
}

func last16(attribute *eap.Attribute) ([]byte, error) {
	if attribute == nil || len(attribute.Value) < 16 {
		return nil, fmt.Errorf("AKA attribute shorter than 16 bytes")
	}
	return append([]byte(nil), attribute.Value[len(attribute.Value)-16:]...), nil
}

func buildChallengeResponse(
	request *eap.EAPPacket,
	attributes map[uint8]*eap.Attribute,
	aka AKAResult,
	imsi, mcc, mnc string,
) ([]byte, error) {
	if len(aka.AUTS) != 0 {
		return encodeAKAResponse(request.Identifier, eap.SubtypeSyncFailure, encodeAttribute(eap.AT_AUTS, aka.AUTS)), nil
	}
	data := encodeRESAttribute(aka.RES)
	if checkcode := attributes[eap.AT_CHECKCODE]; checkcode != nil && len(checkcode.Value) > 2 {
		data = append(data, encodeAttribute(eap.AT_CHECKCODE, checkcode.Value)...)
	}
	data = append(data, encodeAttribute(eap.AT_MAC, make([]byte, 18))...)
	identity := BuildPermanentNAIIdentity(imsi, mcc, mnc)
	kAut := deriveKAut(identity, aka.IK, aka.CK)
	return buildSignedEAPResponse(request.Identifier, data, kAut)
}

func encodeRESAttribute(res []byte) []byte {
	value := make([]byte, 2, len(res)+2)
	binary.BigEndian.PutUint16(value, uint16(len(res)*8))
	value = append(value, res...)
	return encodeAttribute(eap.AT_RES, value)
}

func encodeAttribute(attributeType uint8, value []byte) []byte {
	total := len(value) + 2
	if remainder := total % 4; remainder != 0 {
		total += 4 - remainder
	}
	encoded := make([]byte, total)
	encoded[0] = attributeType
	encoded[1] = byte(total / 4)
	copy(encoded[2:], value)
	return encoded
}

func encodeAKAResponse(identifier, subtype uint8, data []byte) []byte {
	return (&eap.EAPPacket{
		Code: eap.CodeResponse, Identifier: identifier,
		Type: eap.TypeAKA, Subtype: subtype, Data: data,
	}).Encode()
}

func deriveKAut(identity string, ik, ck []byte) []byte {
	digest := sha1.New()
	_, _ = digest.Write([]byte(identity))
	_, _ = digest.Write(ik)
	_, _ = digest.Write(ck)
	prf := enginecrypto.NewFIPS1862PRFSHA1(digest.Sum(nil)).Bytes(nil, 96)
	return append([]byte(nil), prf[16:32]...)
}

func buildSignedEAPResponse(identifier uint8, attributes, kAut []byte) ([]byte, error) {
	macOffset := findMACAttribute(attributes)
	if macOffset < 0 {
		return nil, fmt.Errorf("EAP response missing AT_MAC")
	}
	response := encodeAKAResponse(identifier, eap.SubtypeChallenge, attributes)
	valueOffset := 8 + macOffset + 4
	if valueOffset+16 > len(response) {
		return nil, fmt.Errorf("EAP response AT_MAC offset out of range")
	}
	unsigned := append([]byte(nil), response...)
	for i := 0; i < 16; i++ {
		unsigned[valueOffset+i] = 0
	}
	mac := hmac.New(sha1.New, kAut)
	_, _ = mac.Write(unsigned)
	copy(response[valueOffset:valueOffset+16], mac.Sum(nil)[:16])
	return response, nil
}

func findMACAttribute(attributes []byte) int {
	for offset := 0; offset+2 <= len(attributes); {
		length := int(attributes[offset+1]) * 4
		if length == 0 || offset+length > len(attributes) {
			return -1
		}
		if attributes[offset] == eap.AT_MAC {
			return offset
		}
		offset += length
	}
	return -1
}
