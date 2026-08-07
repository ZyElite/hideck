package identity

import (
	"errors"
	"fmt"
	"strings"
)

const (
	efIMPI   uint16 = 0x6F02
	efDomain uint16 = 0x6F03
	efIMPU   uint16 = 0x6F04
)

func readISIMIdentityFiles(access LogicalChannelAccess, channel int) (Identity, error) {
	var result Identity
	var readErrors []error

	if raw, err := readTransparentISIMEF(access, channel, efIMPI); err == nil {
		result.IMPI = decodeISIMString(raw)
	} else {
		readErrors = append(readErrors, fmt.Errorf("read EF_IMPI: %w", err))
	}
	if raw, err := readTransparentISIMEF(access, channel, efDomain); err == nil {
		result.Domain = decodeISIMString(raw)
	} else {
		readErrors = append(readErrors, fmt.Errorf("read EF_DOMAIN: %w", err))
	}
	if records, err := readLinearFixedISIMEF(access, channel, efIMPU, 16); err == nil {
		for _, record := range records {
			if value := decodeISIMString(record); value != "" && !containsIdentity(result.IMPU, value) {
				result.IMPU = append(result.IMPU, value)
			}
		}
	} else {
		readErrors = append(readErrors, fmt.Errorf("read EF_IMPU: %w", err))
	}

	result.IMPI = strings.TrimSpace(result.IMPI)
	result.Domain = strings.TrimSpace(result.Domain)
	result.IMPU = trimIdentityValues(result.IMPU)
	if result.IMPI != "" || result.Domain != "" || len(result.IMPU) > 0 {
		return result, nil
	}
	if len(readErrors) == 0 {
		return Identity{}, errors.New("identity: ISIM identity files are empty")
	}
	return Identity{}, errors.Join(readErrors...)
}

func decodeISIMString(raw []byte) string {
	data := trimISIMPadding(raw)
	if len(data) == 0 {
		return ""
	}
	if data[0] == 0x80 {
		if value, ok := decodeISIMDataObject(data[1:]); ok {
			return decodeISIMStringValue(value)
		}
	}
	if value, ok := findISIMTLV(data, 0x80); ok {
		if decoded := decodeISIMStringValue(value); decoded != "" {
			return decoded
		}
	}
	return decodeISIMStringValue(data)
}

func decodeISIMDataObject(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return nil, false
	}
	length := int(data[0])
	data = data[1:]
	if length&0x80 != 0 {
		count := length & 0x7F
		if count == 0 || count > 3 || len(data) < count {
			return nil, false
		}
		length = 0
		for _, part := range data[:count] {
			length = length<<8 | int(part)
		}
		data = data[count:]
	}
	if length < 0 || len(data) < length {
		return nil, false
	}
	return data[:length], true
}

func decodeISIMStringValue(data []byte) string {
	data = trimISIMPadding(data)
	if len(data) == 0 {
		return ""
	}
	if length := int(data[0]); length > 0 && len(data) >= 1+length {
		return strings.TrimSpace(string(trimISIMPadding(data[1 : 1+length])))
	}
	return strings.TrimSpace(string(data))
}

func trimISIMPadding(data []byte) []byte {
	start := 0
	for start < len(data) && (data[start] == 0x00 || data[start] == 0xFF) {
		start++
	}
	end := len(data)
	for end > start && (data[end-1] == 0x00 || data[end-1] == 0xFF) {
		end--
	}
	return data[start:end]
}

func containsIdentity(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
