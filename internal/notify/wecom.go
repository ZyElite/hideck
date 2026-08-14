package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/pkg/logger"
)

const (
	maxWeComURLs         = 8
	maxWeComURLLength    = 4096
	maxWeComResponseSize = 64 << 10
	weComRequestTimeout  = 8 * time.Second
)

var weComTemplateVariableNames = []string{
	"event",
	"title",
	"message",
	"timestamp",
	"content",
	"number",
	"device_id",
	"device_name",
	"device_label",
	"time",
}

type WeComChannel struct {
	urls            []string
	payloadTemplate string
	client          *http.Client
}

type SendWeComResult struct {
	FailedCount int `json:"failed_count"`
}

func NewWeComChannel(cfg config.WeComConfig) (*WeComChannel, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	return newWeComChannel(cfg, &http.Client{Timeout: weComRequestTimeout})
}

func newWeComChannel(cfg config.WeComConfig, client *http.Client) (*WeComChannel, error) {
	urls, err := validateWeComConfig(cfg)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("企业微信 HTTP 客户端不能为空")
	}
	channel := &WeComChannel{
		urls:            urls,
		payloadTemplate: cfg.PayloadTemplate,
		client:          client,
	}
	logger.Info("企业微信通知渠道已创建", "urls_count", len(urls))
	return channel, nil
}

func ValidateWeComConfig(cfg config.WeComConfig) error {
	_, err := validateWeComConfig(cfg)
	return err
}

func validateWeComConfig(cfg config.WeComConfig) ([]string, error) {
	if len(cfg.URLs) == 0 {
		return nil, errors.New("企业微信通知至少需要一个 Webhook URL")
	}
	if len(cfg.URLs) > maxWeComURLs {
		return nil, fmt.Errorf("企业微信 Webhook URL 不能超过 %d 个", maxWeComURLs)
	}
	urls := make([]string, 0, len(cfg.URLs))
	for _, rawURL := range cfg.URLs {
		validated, err := validateWeComURL(rawURL)
		if err != nil {
			return nil, err
		}
		urls = append(urls, validated)
	}
	if err := ValidateWeComPayloadTemplate(cfg.PayloadTemplate); err != nil {
		return nil, err
	}
	return urls, nil
}

func validateWeComURL(rawURL string) (string, error) {
	value := strings.TrimSpace(rawURL)
	if value == "" || len(value) > maxWeComURLLength || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("企业微信 Webhook URL 无效")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("企业微信 Webhook URL 无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("企业微信 Webhook URL 必须使用 HTTP 或 HTTPS")
	}
	return parsed.String(), nil
}

func ValidateWeComPayloadTemplate(template string) error {
	_, err := renderWeComPayload(template, NotificationContext{
		Event:     "test",
		Text:      "VoHive notification test",
		Timestamp: time.Unix(0, 0),
	})
	return err
}

func renderWeComPayload(template string, ctx NotificationContext) ([]byte, error) {
	values := weComTemplateValues(ctx)
	for _, name := range weComTemplateVariableNames {
		encoded, err := json.Marshal(values[name])
		if err != nil {
			return nil, fmt.Errorf("编码企业微信模板变量 %q 失败: %w", name, err)
		}
		template = strings.ReplaceAll(template, "{{"+name+"}}", string(encoded))
	}
	if strings.Contains(template, "{{") {
		return nil, errors.New("企业微信 JSON 模板包含不支持的变量")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(template), &payload); err != nil || len(payload) == 0 {
		return nil, errors.New("企业微信 JSON 模板必须渲染为非空对象")
	}
	return []byte(template), nil
}

func weComTemplateValues(ctx NotificationContext) map[string]string {
	timestamp := ctx.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	event := strings.TrimSpace(ctx.Event)
	if event == "" {
		event = "notification"
	}
	return map[string]string{
		"event":        event,
		"title":        weComNotificationTitle(event),
		"message":      strings.TrimSpace(ctx.Text),
		"timestamp":    timestamp.UTC().Format(time.RFC3339),
		"content":      ctx.Content,
		"number":       ctx.Number,
		"device_id":    strings.TrimSpace(ctx.DeviceID),
		"device_name":  strings.TrimSpace(ctx.DeviceName),
		"device_label": ctx.DeviceLabel(),
		"time":         timestamp.Local().Format("2006-01-02 15:04:05"),
	}
}

func weComNotificationTitle(event string) string {
	switch event {
	case "sms_received":
		return "收到新短信"
	case "incoming_call":
		return "来电通知"
	case "call_ended", "call_failed", "call_missed":
		return "通话结果"
	case "ip_rotated":
		return "公网切换"
	default:
		return "VoHive 通知"
	}
}

func (w *WeComChannel) Name() string { return "wecom" }

func (w *WeComChannel) Send(text string) error {
	return w.SendWithContext(NotificationContext{
		Event:     "notification",
		Text:      text,
		Timestamp: time.Now(),
	})
}

func (w *WeComChannel) SendWithContext(ctx NotificationContext) error {
	_, err := w.SendWithContextDetailed(ctx)
	return err
}

func (w *WeComChannel) SendWithContextDetailed(ctx NotificationContext) (SendWeComResult, error) {
	result := SendWeComResult{}
	if w == nil || w.client == nil || len(w.urls) == 0 {
		return result, nil
	}
	if strings.TrimSpace(ctx.Text) == "" {
		return result, nil
	}
	payload, err := renderWeComPayload(w.payloadTemplate, ctx)
	if err != nil {
		return result, err
	}
	return w.sendToAll(payload)
}

func (w *WeComChannel) sendToAll(payload []byte) (SendWeComResult, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var lastErr error
	failedCount := 0
	for index, destination := range w.urls {
		wg.Add(1)
		go func(destinationIndex int, target string) {
			defer wg.Done()
			if err := w.post(target, payload); err != nil {
				mu.Lock()
				failedCount++
				lastErr = err
				mu.Unlock()
				logger.Warn("企业微信通知发送失败", "destination_index", destinationIndex+1, "err", err)
			}
		}(index, destination)
	}
	wg.Wait()
	return SendWeComResult{FailedCount: failedCount}, lastErr
}

func (w *WeComChannel) post(destination string, payload []byte) error {
	request, err := http.NewRequest(http.MethodPost, destination, bytes.NewReader(payload))
	if err != nil {
		return errors.New("创建企业微信通知请求失败")
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("User-Agent", "vohive-wecom-notification/1")
	response, err := w.client.Do(request)
	if err != nil {
		return redactWeComTransportError(err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxWeComResponseSize))
	closeErr := response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("读取企业微信响应失败: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭企业微信响应失败: %w", closeErr)
	}
	return validateWeComResponse(response.StatusCode, body)
}

func redactWeComTransportError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return fmt.Errorf("企业微信请求失败: %w", urlErr.Err)
	}
	return fmt.Errorf("企业微信请求失败: %w", err)
}

func validateWeComResponse(status int, body []byte) error {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("企业微信返回 HTTP %d", status)
	}
	var result struct {
		ErrCode *int `json:"errcode"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.ErrCode == nil {
		return errors.New("企业微信返回了无效响应")
	}
	if *result.ErrCode != 0 {
		return fmt.Errorf("企业微信拒绝请求，错误码 %d", *result.ErrCode)
	}
	return nil
}

func (w *WeComChannel) RegisterCommand(_ string, _ CommandHandler) {}

func (w *WeComChannel) Start() error { return nil }

func (w *WeComChannel) Close() error {
	if w != nil && w.client != nil {
		w.client.CloseIdleConnections()
	}
	return nil
}
