package smscodec

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// RPDUKind is the classification of an RP PDU (3GPP TS 24.011).
type RPDUKind string

// RP PDU kinds.
const (
	RPDUKindUnknown RPDUKind = "UNKNOWN"
	RPDUKindData    RPDUKind = "RP-DATA"
	RPDUKindAck     RPDUKind = "RP-ACK"
	RPDUKindError   RPDUKind = "RP-ERROR"
)

// RPDUInfo describes a classified RP PDU.
type RPDUInfo struct {
	Kind    RPDUKind
	RawType byte
	MR      byte
	Cause   int
}

// ClassifyRPDU classifies an RP PDU by its message type indicator.
func ClassifyRPDU(body []byte) RPDUInfo {
	if len(body) == 0 {
		return RPDUInfo{Kind: RPDUKindUnknown}
	}
	info := RPDUInfo{RawType: body[0], Kind: RPDUKindUnknown}
	if len(body) > 1 {
		info.MR = body[1]
	}
	switch body[0] {
	case 0x00, 0x01:
		info.Kind = RPDUKindData
	case 0x02, 0x03:
		info.Kind = RPDUKindAck
	case 0x04, 0x05:
		info.Kind = RPDUKindError
		if cause, err := ParseRPErrorCause(body); err == nil {
			info.Cause = int(cause)
		}
	}
	return info
}

// ParseRPErrorCause parses the cause from an RP-ERROR PDU (TS 24.011 §8.2.5.4).
func ParseRPErrorCause(body []byte) (byte, error) {
	if len(body) < 4 {
		return 0, errors.New("smscodec: RP-ERROR too short")
	}
	if body[0] != 0x04 && body[0] != 0x05 {
		return 0, fmt.Errorf("smscodec: not RP-ERROR mti=0x%02x", body[0])
	}
	causeIELen := int(body[2])
	if causeIELen <= 0 {
		return 0, errors.New("smscodec: empty cause IE")
	}
	if 3+causeIELen > len(body) {
		return 0, errors.New("smscodec: cause IE out of bounds")
	}
	return body[3] & 0x7F, nil
}

// ParseRPDataWithAddresses parses an RP-DATA PDU into (MR, OA, DA, TPUD, err).
func ParseRPDataWithAddresses(body []byte) (byte, string, string, []byte, error) {
	if len(body) < 5 {
		return 0, "", "", nil, errors.New("smscodec: RP-DATA too short")
	}
	i := 0
	i++ // MTI
	if i >= len(body) {
		return 0, "", "", nil, errors.New("smscodec: missing MR")
	}
	rpMr := body[i]
	i++

	if i >= len(body) {
		return 0, "", "", nil, errors.New("smscodec: missing RP-OA")
	}
	oaLen := int(body[i])
	i++
	if i+oaLen > len(body) {
		return 0, "", "", nil, errors.New("smscodec: RP-OA out of bounds")
	}
	oa := decodeAddressValue(body[i : i+oaLen])
	i += oaLen

	if i >= len(body) {
		return 0, "", "", nil, errors.New("smscodec: missing RP-DA")
	}
	daLen := int(body[i])
	i++
	if i+daLen > len(body) {
		return 0, "", "", nil, errors.New("smscodec: RP-DA out of bounds")
	}
	da := decodeAddressValue(body[i : i+daLen])
	i += daLen

	if i >= len(body) {
		return 0, "", "", nil, errors.New("smscodec: missing RP-UD")
	}
	udLen := int(body[i])
	i++
	if i+udLen > len(body) {
		return 0, "", "", nil, errors.New("smscodec: RP-UD out of bounds")
	}
	return rpMr, oa, da, body[i : i+udLen], nil
}

// BuildRPData builds an RP-DATA PDU (TS 24.011 §8.2.2).
func BuildRPData(mr byte, oa, da string, tpdu []byte) []byte {
	oaEnc, _ := EncodeAddress(oa)
	daEnc, _ := EncodeAddress(da)
	out := make([]byte, 0, 3+len(oaEnc)+len(daEnc)+len(tpdu))
	out = append(out, 0x00) // RP-DATA MTI, direction mobile-originated
	out = append(out, mr)
	out = append(out, oaEnc...)
	out = append(out, daEnc...)
	out = append(out, byte(len(tpdu)))
	out = append(out, tpdu...)
	return out
}

// BuildRPAck builds an RP-ACK in the mobile-to-network direction.
func BuildRPAck(mr byte) []byte {
	return []byte{0x02, mr}
}

// BuildRPError builds an RP-ERROR in the mobile-to-network direction.
func BuildRPError(mr, cause byte) []byte {
	return []byte{0x04, mr, 0x01, cause, 0x00}
}

// EncodeAddress encodes a phone number as a TS 24.011 address field.
func EncodeAddress(number string) ([]byte, error) {
	digits := onlyDigits(number)
	if len(digits) == 0 {
		return []byte{0x00}, nil
	}
	ton := byte(0x81)
	if strings.HasPrefix(strings.TrimSpace(number), "+") {
		ton = 0x91
	}
	// Address: [Value length][TON/NPI][semi-octet digits].
	addr := make([]byte, 0, 2+(len(digits)+1)/2)
	addr = append(addr, byte((len(digits)+1)/2+1))
	addr = append(addr, ton)
	for i := 0; i+1 < len(digits); i += 2 {
		addr = append(addr, (digits[i+1]-'0')<<4|(digits[i]-'0'))
	}
	if len(digits)%2 == 1 {
		addr = append(addr, 0xF0|(digits[len(digits)-1]-'0'))
	}
	return addr, nil
}

// DecodeAddressValue decodes a TS 24.011 address value (without the length
// prefix) into a phone number.
func DecodeAddressValue(addr []byte) string {
	return decodeAddressValue(addr)
}

// decodeAddressValue decodes a TS 24.011 address value.
func decodeAddressValue(addr []byte) string {
	if len(addr) == 0 {
		return ""
	}
	// First byte is TON/NPI.
	var b strings.Builder
	if addr[0]&0x70 == 0x10 {
		b.WriteByte('+')
	}
	for _, octet := range addr[1:] {
		lo := octet & 0x0F
		if lo <= 9 {
			b.WriteByte('0' + lo)
		}
		hi := octet >> 4
		if hi <= 9 {
			b.WriteByte('0' + hi)
		}
	}
	return b.String()
}

// DecodeBodyMaybeHex decodes a body that may be hex-encoded.
func DecodeBodyMaybeHex(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	decoded, err := hex.DecodeString(string(body))
	if err == nil && len(decoded) > 0 {
		return decoded
	}
	return body
}

// onlyDigits keeps only the digit characters.
func onlyDigits(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
