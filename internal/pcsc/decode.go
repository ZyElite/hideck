package pcsc

import (
	"context"
	"errors"
	"strings"
)

func decodeSwappedBCD(value []byte, dropFirstNibble bool) string {
	var result strings.Builder
	for _, octet := range value {
		for _, nibble := range []byte{octet & 0x0F, octet >> 4} {
			if dropFirstNibble {
				dropFirstNibble = false
				continue
			}
			if nibble == 0x0F {
				return result.String()
			}
			if nibble > 9 {
				return ""
			}
			result.WriteByte('0' + nibble)
		}
	}
	return result.String()
}

func decodeIMSI(data []byte) (string, error) {
	if len(data) < 2 {
		return "", errors.New("pcsc: EF_IMSI is too short")
	}
	length := int(data[0])
	if length <= 0 || length > len(data)-1 {
		return "", errors.New("pcsc: EF_IMSI has an invalid length")
	}
	value := decodeSwappedBCD(data[1:1+length], true)
	if len(value) < 10 || len(value) > 18 || !decimalDigits(value) {
		return "", errors.New("pcsc: card returned an invalid IMSI")
	}
	return value, nil
}

func decimalDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func findTLV(data []byte, wanted byte) []byte {
	for len(data) >= 2 {
		tag := data[0]
		data = data[1:]
		length, consumed, ok := decodeTLVLength(data)
		if !ok || consumed+length > len(data) {
			return nil
		}
		value := data[consumed : consumed+length]
		if tag == wanted {
			return append([]byte(nil), value...)
		}
		if tag&0x20 != 0 {
			if nested := findTLV(value, wanted); len(nested) > 0 {
				return nested
			}
		}
		data = data[consumed+length:]
	}
	return nil
}

func decodeTLVLength(data []byte) (length, consumed int, ok bool) {
	if len(data) == 0 {
		return 0, 0, false
	}
	if data[0]&0x80 == 0 {
		return int(data[0]), 1, true
	}
	count := int(data[0] & 0x7F)
	if count < 1 || count > 2 || len(data) < 1+count {
		return 0, 0, false
	}
	for _, octet := range data[1 : 1+count] {
		length = length<<8 | int(octet)
	}
	return length, 1 + count, true
}

func parseAKAResponse(data []byte) (AKAResult, error) {
	if len(data) < 2 {
		return AKAResult{}, errors.New("pcsc: USIM returned a short AKA response")
	}
	if data[0] == 0xDC {
		auts, tail, ok := takeLV(data[1:])
		if !ok || len(auts) != 14 || len(tail) != 0 {
			return AKAResult{}, errors.New("pcsc: USIM returned invalid AKA synchronization evidence")
		}
		return AKAResult{AUTS: append([]byte(nil), auts...), SynchronizationFailure: true}, nil
	}
	if data[0] != 0xDB {
		return AKAResult{}, errors.New("pcsc: USIM returned an unsupported AKA response")
	}
	return parseAKASuccess(data[1:])
}

func parseAKASuccess(data []byte) (AKAResult, error) {
	res, rest, ok := takeLV(data)
	if !ok || len(res) < 4 || len(res) > 16 {
		return AKAResult{}, errors.New("pcsc: USIM returned an invalid AKA RES")
	}
	ck, rest, ok := takeLV(rest)
	if !ok || len(ck) != 16 {
		return AKAResult{}, errors.New("pcsc: USIM returned an invalid AKA CK")
	}
	ik, rest, ok := takeLV(rest)
	if !ok || len(ik) != 16 {
		return AKAResult{}, errors.New("pcsc: USIM returned an invalid AKA IK")
	}
	if len(rest) > 0 {
		kc, tail, valid := takeLV(rest)
		if !valid || len(kc) != 8 || len(tail) != 0 {
			return AKAResult{}, errors.New("pcsc: USIM returned invalid trailing AKA material")
		}
	}
	return AKAResult{RES: append([]byte(nil), res...), CK: append([]byte(nil), ck...), IK: append([]byte(nil), ik...)}, nil
}

func takeLV(data []byte) (value, rest []byte, ok bool) {
	if len(data) == 0 || int(data[0]) > len(data)-1 {
		return nil, data, false
	}
	length := int(data[0])
	return data[1 : 1+length], data[1+length:], true
}

func readSPN(ctx context.Context, card Card) string {
	if err := selectFile(ctx, card, []byte{0x6F, 0x46}); err != nil {
		return ""
	}
	data, err := readBinary(ctx, card, 17)
	if err != nil || len(data) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(data[1:]), "\x00\xFF"))
}

func readSMSC(ctx context.Context, card Card) string {
	if err := selectFile(ctx, card, []byte{0x6F, 0x42}); err != nil {
		return ""
	}
	data, sw, err := card.Transmit(ctx, []byte{0x00, 0xB2, 0x01, 0x04, 0x00})
	if err != nil || sw != 0x9000 || len(data) < 15 {
		return ""
	}
	sca := data[len(data)-15 : len(data)-3]
	if len(sca) < 2 || sca[0] < 2 || int(sca[0]) > len(sca)-1 {
		return ""
	}
	digits := decodeSwappedBCD(sca[2:1+int(sca[0])], false)
	if !decimalDigits(digits) {
		return ""
	}
	if sca[1]&0x70 == 0x10 {
		return "+" + digits
	}
	return digits
}
