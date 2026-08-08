package swu

import (
	"crypto/hmac"
	"errors"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
)

func validateParsedSimakaAttributes(eapType uint8, attrs []eapaka.Attribute) error {
	data, err := eapaka.MarshalAttributes(attrs)
	if err != nil {
		return err
	}
	return validateKnownSimakaAttributes(eapType, data)
}

func validateKnownSimakaAttributes(eapType uint8, attrsData []byte) error {
	for offset := 0; offset < len(attrsData); {
		if offset+2 > len(attrsData) {
			return errors.New("swu: incomplete EAP attribute header")
		}
		attributeType := attrsData[offset]
		length := int(attrsData[offset+1]) * 4
		if length == 0 || offset+length > len(attrsData) {
			return fmt.Errorf("swu: invalid EAP attribute type=%d length=%d", attributeType, length)
		}
		if attributeType <= 127 && !knownSimakaAttribute(eapType, attributeType) {
			return fmt.Errorf("swu: unknown non-skippable EAP attribute %d", attributeType)
		}
		offset += length
	}
	return nil
}

func knownSimakaAttribute(eapType, attributeType uint8) bool {
	switch attributeType {
	case 1, 2, 3, 4, 6, 7, 10, 11, 12, 13, 14, 15, 16, 17, 19, 20, 21, 22:
		return true
	case 23, 24:
		return eapType == eapaka.TypeAKAPrime
	default:
		return false
	}
}

func verifyEAPAKAMAC(eapRaw, _ []byte, kAut, recvMac []byte) error {
	calculated, err := calculateEAPMAC(eapaka.TypeAKA, kAut, eapRaw)
	if err != nil {
		return err
	}
	if !hmac.Equal(calculated, recvMac) {
		return errors.New("swu: EAP-AKA AT_MAC verification failed")
	}
	return nil
}

func verifyEAPReauthMAC(eapRaw, _ []byte, kAut, recvMac []byte, useSHA256 bool) error {
	eapType := uint8(eapaka.TypeAKA)
	if useSHA256 {
		eapType = eapaka.TypeAKAPrime
	}
	calculated, err := calculateEAPMAC(eapType, kAut, eapRaw)
	if err != nil {
		return err
	}
	if !hmac.Equal(calculated, recvMac) {
		return errors.New("swu: EAP reauthentication MAC verification failed")
	}
	return nil
}

func (s *Session) buildEAPSyncFailure(id uint8, auts []byte) ([]ikev2.Payload, error) {
	eapType := s.eapType
	if eapType != eapaka.TypeAKA && eapType != eapaka.TypeAKAPrime {
		eapType = eapaka.TypeAKA
	}
	request := eapaka.Packet{
		Code: eapaka.CodeRequest, Identifier: id, Type: eapType,
		Subtype: eapaka.SubtypeChallenge,
	}
	response, err := eapaka.BuildSynchronizationFailureResponse(request, auts)
	if err != nil {
		return nil, err
	}
	return eapResponsePayload(response)
}
