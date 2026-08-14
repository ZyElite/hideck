package notify

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const qqQRGCMNonceSize = 12

func decryptQQQRSecret(encryptedBase64 string, key []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("QQ 扫码 AES-256 密钥长度无效")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encryptedBase64))
	if err != nil {
		return "", errors.New("QQ 扫码加密 Secret 不是有效 Base64")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("创建 QQ 扫码 AES 解密器失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建 QQ 扫码 GCM 解密器失败: %w", err)
	}
	if len(raw) < qqQRGCMNonceSize+gcm.Overhead() {
		return "", errors.New("QQ 扫码加密 Secret 长度无效")
	}
	plaintext, err := gcm.Open(nil, raw[:qqQRGCMNonceSize], raw[qqQRGCMNonceSize:], nil)
	if err != nil {
		return "", errors.New("QQ 扫码 Secret 解密认证失败")
	}
	if len(plaintext) == 0 || !utf8.Valid(plaintext) {
		return "", errors.New("QQ 扫码 Secret 明文无效")
	}
	return string(plaintext), nil
}
