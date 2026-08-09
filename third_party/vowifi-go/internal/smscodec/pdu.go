package smscodec

import (
	"encoding/hex"
	"errors"
	"strings"
)

// DecodeBodyMaybeHex 尝试把 HTTP/SIP 载荷按十六进制字符串解码，否则原样返回。
func DecodeBodyMaybeHex(body []byte) ([]byte, error) {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return nil, errors.New("body 为空")
	}
	if IsHexString(s) {
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return body, nil
}

// IsHexString 判断字符串是否为偶数长度的十六进制编码。
func IsHexString(s string) bool {
	if len(s) < 2 || len(s)%2 != 0 {
		return false
	}
	for _, c := range s {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}
