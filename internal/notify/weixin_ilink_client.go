package notify

import (
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
	DefaultWeixinBaseURL = "https://ilinkai.weixin.qq.com"
	weixinQRBotType      = "3"
	weixinQRResponseMax  = 1 << 20
)

type weixinQRPlatformStatus struct {
	Status       string `json:"status"`
	RedirectHost string `json:"redirect_host"`
	AccountID    string `json:"ilink_bot_id"`
	Token        string `json:"bot_token"`
	BaseURL      string `json:"baseurl"`
	UserID       string `json:"ilink_user_id"`
}

type weixinILinkClient struct {
	httpClient *http.Client
}

func newWeixinILinkClient(client *http.Client) *weixinILinkClient {
	if client == nil {
		client = &http.Client{Timeout: 35 * time.Second}
	}
	return &weixinILinkClient{httpClient: client}
}

func (c *weixinILinkClient) fetchQRCode(ctx context.Context, baseURL string) (string, string, error) {
	var response struct {
		QRCode  string `json:"qrcode"`
		Content string `json:"qrcode_img_content"`
	}
	endpoint := "/ilink/bot/get_bot_qrcode?bot_type=" + weixinQRBotType
	if err := c.getJSON(ctx, baseURL, endpoint, &response); err != nil {
		return "", "", fmt.Errorf("获取微信二维码失败: %w", err)
	}
	if strings.TrimSpace(response.QRCode) == "" {
		return "", "", errors.New("微信二维码响应缺少 qrcode")
	}
	qrURL := strings.TrimSpace(response.Content)
	if qrURL == "" {
		qrURL = strings.TrimSpace(response.QRCode)
	}
	return strings.TrimSpace(response.QRCode), qrURL, nil
}

func (c *weixinILinkClient) pollQR(ctx context.Context, baseURL, qrToken string) (weixinQRPlatformStatus, error) {
	var response weixinQRPlatformStatus
	endpoint := "/ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(qrToken)
	err := c.getJSON(ctx, baseURL, endpoint, &response)
	return response, err
}

func (c *weixinILinkClient) getJSON(ctx context.Context, baseURL, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("iLink-App-Id", "bot")
	request.Header.Set("iLink-App-ClientVersion", "131584")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("iLink 请求失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("iLink 请求失败: HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, weixinQRResponseMax))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析 iLink 响应失败: %w", err)
	}
	return nil
}

func normalizeWeixinBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = DefaultWeixinBaseURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !isLoopbackHTTP(parsed)) {
		return "", errors.New("微信 base_url 必须是 HTTPS 地址或本机 HTTP 地址")
	}
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = "", "", "", ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHTTP(parsed *url.URL) bool {
	return parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1")
}

func redirectWeixinBaseURL(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/?#@") {
		return "", errors.New("微信二维码重定向地址无效")
	}
	return normalizeWeixinBaseURL("https://" + host)
}
