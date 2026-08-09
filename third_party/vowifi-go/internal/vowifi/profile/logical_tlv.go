package profile

import (
	"bytes"
	"strings"
	"unicode"
)

func parseTransparentFileSizeFromFCP(fcp []byte) int {
	for _, tag := range []byte{0x80, 0x81} {
		for _, value := range collectTLVValues(fcp, tag) {
			if size := bigEndianInt(value); size > 0 {
				return size
			}
		}
	}
	return 0
}

func parseLinearFixedMetaFromFCP(fcp []byte) (int, int) {
	for _, value := range collectTLVValues(fcp, 0x82) {
		if len(value) >= 5 {
			return int(value[3]), int(value[4])
		}
	}
	return 0, parseTransparentFileSizeFromFCP(fcp)
}

func collectTLVValues(data []byte, targetTag byte) [][]byte {
	var values [][]byte
	for offset := 0; offset < len(data); {
		firstTag, value, next, ok := readTLV(data, offset)
		if !ok {
			break
		}
		if firstTag == targetTag {
			values = append(values, append([]byte(nil), value...))
		}
		if firstTag&0x20 != 0 {
			values = append(values, collectTLVValues(value, targetTag)...)
		}
		offset = next
	}
	return values
}

func readTLV(data []byte, offset int) (byte, []byte, int, bool) {
	if offset >= len(data) {
		return 0, nil, offset, false
	}
	firstTag := data[offset]
	offset++
	if firstTag&0x1F == 0x1F {
		for offset < len(data) {
			part := data[offset]
			offset++
			if part&0x80 == 0 {
				break
			}
		}
	}
	if offset >= len(data) {
		return 0, nil, offset, false
	}
	length, valueOffset, ok := readTLVLength(data, offset)
	if !ok || length > len(data)-valueOffset {
		return 0, nil, offset, false
	}
	return firstTag, data[valueOffset : valueOffset+length], valueOffset + length, true
}

func readTLVLength(data []byte, offset int) (int, int, bool) {
	first := data[offset]
	offset++
	if first&0x80 == 0 {
		return int(first), offset, true
	}
	count := int(first & 0x7F)
	if count == 0 || count > len(data)-offset {
		return 0, offset, false
	}
	length := 0
	for _, part := range data[offset : offset+count] {
		if length > (int(^uint(0)>>1)-int(part))/256 {
			return 0, offset, false
		}
		length = length<<8 | int(part)
	}
	return length, offset + count, true
}

func bigEndianInt(value []byte) int {
	result := 0
	for _, part := range value {
		result = result<<8 | int(part)
	}
	return result
}

func decodeIdentityValues(data []byte) []string {
	var result []string
	for _, value := range collectTLVValues(data, 0x80) {
		result = appendUnique(result, normalizeIdentityString(value))
	}
	if len(result) == 0 {
		result = appendUnique(result, normalizeIdentityString(data))
	}
	return result
}

func normalizeIdentityString(data []byte) string {
	trimmed := bytes.Trim(data, "\x00\xff")
	var result strings.Builder
	for _, value := range string(trimmed) {
		if value != 0 && value != unicode.ReplacementChar && unicode.IsPrint(value) {
			result.WriteRune(value)
		}
	}
	return strings.TrimSpace(result.String())
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
