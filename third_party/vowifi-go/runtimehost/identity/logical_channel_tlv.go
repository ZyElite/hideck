package identity

import "fmt"

type isimTLV struct {
	tag   int
	value []byte
}

func findISIMTLV(data []byte, target int) ([]byte, bool) {
	items, err := parseISIMTLVs(data)
	if err != nil {
		return nil, false
	}
	for _, item := range items {
		if item.tag == target {
			return item.value, true
		}
		if isConstructedISIMTag(item.tag) {
			if value, ok := findISIMTLV(item.value, target); ok {
				return value, true
			}
		}
	}
	return nil, false
}

func parseISIMTLVs(data []byte) ([]isimTLV, error) {
	var result []isimTLV
	for len(data) > 0 {
		data = trimISIMTLVPadding(data)
		if len(data) == 0 {
			break
		}
		tag, rest, err := readISIMTag(data)
		if err != nil {
			return result, err
		}
		length, rest, err := readISIMLength(rest)
		if err != nil {
			return result, err
		}
		if length > len(rest) {
			return result, fmt.Errorf("identity: TLV tag 0x%X length %d exceeds remaining %d", tag, length, len(rest))
		}
		result = append(result, isimTLV{tag: tag, value: append([]byte(nil), rest[:length]...)})
		data = rest[length:]
	}
	return result, nil
}

func readISIMTag(data []byte) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, fmt.Errorf("identity: empty TLV tag")
	}
	tag := int(data[0])
	data = data[1:]
	if tag&0x1F != 0x1F {
		return tag, data, nil
	}
	tag <<= 8
	for {
		if len(data) == 0 {
			return 0, nil, fmt.Errorf("identity: truncated high-tag-number TLV")
		}
		part := data[0]
		data = data[1:]
		tag |= int(part)
		if part&0x80 == 0 {
			return tag, data, nil
		}
		tag <<= 8
	}
}

func readISIMLength(data []byte) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, fmt.Errorf("identity: empty TLV length")
	}
	first := data[0]
	data = data[1:]
	if first&0x80 == 0 {
		return int(first), data, nil
	}
	count := int(first & 0x7F)
	if count == 0 || count > 3 || len(data) < count {
		return 0, nil, fmt.Errorf("identity: invalid TLV length form 0x%02X", first)
	}
	length := 0
	for _, part := range data[:count] {
		length = length<<8 | int(part)
	}
	return length, data[count:], nil
}

func fileSizeFromFCP(fcp []byte) int {
	body := fcp
	if value, ok := findISIMTLV(fcp, 0x62); ok {
		body = value
	}
	if value, ok := findISIMTLV(body, 0x80); ok {
		return bigEndianInt(value)
	}
	if value, ok := findISIMTLV(body, 0x81); ok {
		return bigEndianInt(value)
	}
	return 0
}

func recordInfoFromFCP(fcp []byte) (recordLength, recordCount int) {
	body := fcp
	if value, ok := findISIMTLV(fcp, 0x62); ok {
		body = value
	}
	if value, ok := findISIMTLV(body, 0x82); ok && len(value) >= 5 {
		return int(value[2])<<8 | int(value[3]), int(value[4])
	}
	return 0, 0
}

func trimISIMTLVPadding(data []byte) []byte {
	for len(data) > 0 && (data[0] == 0x00 || data[0] == 0xFF) {
		data = data[1:]
	}
	return data
}

func isConstructedISIMTag(tag int) bool {
	for tag > 0xFF {
		tag >>= 8
	}
	return tag&0x20 != 0
}

func bigEndianInt(value []byte) int {
	result := 0
	for _, part := range value {
		result = result<<8 | int(part)
	}
	return result
}
