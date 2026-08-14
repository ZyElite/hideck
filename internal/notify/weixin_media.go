package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	weixinMediaFile         = 3
	weixinItemFile          = 4
	defaultWeixinCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
)

type weixinUploadRequest struct {
	Credentials WeixinQRCredentials
	Target      string
	FileKey     string
	RawSize     int
	RawMD5      string
	CipherSize  int
	AESKeyHex   string
}

type weixinUploadResponse struct {
	Ret           int    `json:"ret"`
	ErrCode       int    `json:"errcode"`
	ErrMsg        string `json:"errmsg"`
	UploadParam   string `json:"upload_param"`
	UploadFullURL string `json:"upload_full_url"`
}

type weixinFileItemInput struct {
	Filename       string
	RawSize        int
	Key            []byte
	EncryptedParam string
}

func (w *WeixinChannel) sendAttachment(ctx context.Context, target string, attachment CommandAttachment) error {
	path := strings.TrimSpace(attachment.Path)
	if path == "" {
		return errors.New("录音附件缺少服务端文件路径")
	}
	plaintext, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取录音文件失败: %w", err)
	}
	key := make([]byte, aes.BlockSize)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("生成微信媒体密钥失败: %w", err)
	}
	ciphertext, err := encryptWeixinMedia(plaintext, key)
	if err != nil {
		return err
	}
	fileKey, err := randomHex(16)
	if err != nil {
		return fmt.Errorf("生成微信媒体文件标识失败: %w", err)
	}
	state := w.snapshotState()
	credentials := weixinCredentials(state)
	upload, err := w.requestUpload(ctx, weixinUploadRequest{
		Credentials: credentials, Target: target, FileKey: fileKey, RawSize: len(plaintext),
		RawMD5: fmt.Sprintf("%x", md5.Sum(plaintext)), CipherSize: len(ciphertext), AESKeyHex: hex.EncodeToString(key),
	})
	if err != nil {
		return err
	}
	uploadURL, err := w.resolveUploadURL(upload, fileKey)
	if err != nil {
		return err
	}
	encryptedParam, err := w.uploadCiphertext(ctx, uploadURL, ciphertext)
	if err != nil {
		return err
	}
	filename := strings.TrimSpace(attachment.Recording)
	if filename == "" {
		filename = filepath.Base(path)
	}
	item := buildWeixinFileItem(weixinFileItemInput{
		Filename: filename, RawSize: len(plaintext), Key: key, EncryptedParam: encryptedParam,
	})
	return w.client.sendItem(ctx, weixinSendItemRequest{
		Credentials: credentials, Target: target, Item: item,
		ContextToken: state.Weixin.ContextTokens[target],
	})
}

func (w *WeixinChannel) requestUpload(ctx context.Context, input weixinUploadRequest) (weixinUploadResponse, error) {
	var response weixinUploadResponse
	err := w.client.post(ctx, weixinPostRequest{
		Credentials: input.Credentials, Endpoint: "/ilink/bot/getuploadurl",
		Payload: map[string]any{
			"filekey": input.FileKey, "media_type": weixinMediaFile, "to_user_id": input.Target,
			"rawsize": input.RawSize, "rawfilemd5": input.RawMD5, "filesize": input.CipherSize,
			"no_need_thumb": true, "aeskey": input.AESKeyHex,
		}, Target: &response,
	})
	if err != nil {
		return response, err
	}
	if response.Ret != 0 || response.ErrCode != 0 {
		return response, fmt.Errorf("iLink getuploadurl 失败: ret=%d errcode=%d errmsg=%s", response.Ret, response.ErrCode, response.ErrMsg)
	}
	return response, nil
}

func (w *WeixinChannel) resolveUploadURL(response weixinUploadResponse, fileKey string) (string, error) {
	value := strings.TrimSpace(response.UploadFullURL)
	if value == "" && strings.TrimSpace(response.UploadParam) != "" {
		baseURL := strings.TrimSpace(w.config.CDNBaseURL)
		if baseURL == "" {
			baseURL = defaultWeixinCDNBaseURL
		}
		value = strings.TrimRight(baseURL, "/") + "/upload?encrypted_query_param=" +
			url.QueryEscape(response.UploadParam) + "&filekey=" + url.QueryEscape(fileKey)
	}
	if value == "" {
		return "", errors.New("iLink getuploadurl 未返回上传地址")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !isLoopbackHTTP(parsed)) {
		return "", errors.New("微信媒体上传地址无效")
	}
	if parsed.Scheme == "https" && !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".qq.com") {
		return "", errors.New("微信媒体上传地址不是腾讯域名")
	}
	return parsed.String(), nil
}

func (w *WeixinChannel) uploadCiphertext(ctx context.Context, uploadURL string, ciphertext []byte) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(ciphertext))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := w.client.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("上传微信媒体失败: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("上传微信媒体失败: HTTP %d", response.StatusCode)
	}
	value := strings.TrimSpace(response.Header.Get("x-encrypted-param"))
	if value == "" {
		return "", errors.New("上传微信媒体成功但缺少 x-encrypted-param")
	}
	return value, nil
}

func encryptWeixinMedia(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建微信媒体加密器失败: %w", err)
	}
	padding := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	ciphertext := make([]byte, len(padded))
	for offset := 0; offset < len(padded); offset += aes.BlockSize {
		block.Encrypt(ciphertext[offset:offset+aes.BlockSize], padded[offset:offset+aes.BlockSize])
	}
	return ciphertext, nil
}

func buildWeixinFileItem(input weixinFileItemInput) map[string]any {
	encodedKey := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(input.Key)))
	return map[string]any{
		"type": weixinItemFile,
		"file_item": map[string]any{
			"media": map[string]any{
				"encrypt_query_param": input.EncryptedParam, "aes_key": encodedKey, "encrypt_type": 1,
			},
			"file_name": input.Filename, "len": strconv.Itoa(input.RawSize),
		},
	}
}
