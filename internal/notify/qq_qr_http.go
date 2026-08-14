package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	qqQRCreatePath    = "/lite/create_bind_task"
	qqQRPollPath      = "/lite/poll_bind_result"
	qqQRResponseLimit = 1 << 20
)

type qqQRHTTPClient struct {
	baseURL string
	client  *http.Client
}

type qqQRPollResult struct {
	Status          int
	AppID           string
	EncryptedSecret string
	UserOpenID      string
}

type qqQRAPIResponse struct {
	RetCode int    `json:"retcode"`
	Message string `json:"msg"`
	Data    struct {
		TaskID          string `json:"task_id"`
		Status          int    `json:"status"`
		AppID           string `json:"bot_appid"`
		EncryptedSecret string `json:"bot_encrypt_secret"`
		UserOpenID      string `json:"user_openid"`
	} `json:"data"`
}

func newQQQRHTTPClient(baseURL string, client *http.Client) *qqQRHTTPClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &qqQRHTTPClient{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: client}
}

func (c *qqQRHTTPClient) create(ctx context.Context, key string) (string, error) {
	var response qqQRAPIResponse
	if err := c.post(ctx, qqQRCreatePath, map[string]string{"key": key}, &response); err != nil {
		return "", fmt.Errorf("创建 QQ 扫码任务失败: %w", err)
	}
	if response.RetCode != 0 {
		return "", fmt.Errorf("创建 QQ 扫码任务失败: retcode=%d msg=%s", response.RetCode, response.Message)
	}
	taskID := strings.TrimSpace(response.Data.TaskID)
	if taskID == "" {
		return "", errors.New("创建 QQ 扫码任务响应缺少 task_id")
	}
	return taskID, nil
}

func (c *qqQRHTTPClient) poll(ctx context.Context, taskID string) (qqQRPollResult, error) {
	var response qqQRAPIResponse
	if err := c.post(ctx, qqQRPollPath, map[string]string{"task_id": taskID}, &response); err != nil {
		return qqQRPollResult{}, fmt.Errorf("查询 QQ 扫码任务失败: %w", err)
	}
	if response.RetCode != 0 {
		return qqQRPollResult{}, fmt.Errorf("查询 QQ 扫码任务失败: retcode=%d msg=%s", response.RetCode, response.Message)
	}
	return qqQRPollResult{
		Status: response.Data.Status, AppID: response.Data.AppID,
		EncryptedSecret: response.Data.EncryptedSecret, UserOpenID: response.Data.UserOpenID,
	}, nil
}

func (c *qqQRHTTPClient) post(ctx context.Context, path string, payload any, target any) error {
	endpoint, err := qqQREndpoint(c.baseURL, path)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "QQBotAdapter/1.1.0 (Go; HiDeck)")
	response, err := c.client.Do(request)
	if err != nil {
		return redactQQQRTransportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("QQ 扫码服务返回 HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, qqQRResponseLimit))
	if err := decoder.Decode(target); err != nil {
		return errors.New("QQ 扫码服务返回无效 JSON")
	}
	return nil
}

func qqQREndpoint(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("QQ 扫码服务地址无效")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHTTP(parsed)) {
		return "", errors.New("QQ 扫码服务地址必须使用 HTTPS")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	return parsed.String(), nil
}

func redactQQQRTransportError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		return fmt.Errorf("QQ 扫码请求失败: %w", urlError.Err)
	}
	return fmt.Errorf("QQ 扫码请求失败: %w", err)
}
