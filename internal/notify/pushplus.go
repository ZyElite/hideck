package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

const pushplusRequestTimeout = 10 * time.Second
const pushplusSendURL = "https://www.pushplus.plus/send"

type PushplusChannel struct {
	cfg    config.PushplusConfig
	client *http.Client
}

func NewPushplusChannel(cfg config.PushplusConfig) (*PushplusChannel, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("pushplus token is required")
	}
	return &PushplusChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: pushplusRequestTimeout},
	}, nil
}

func (c *PushplusChannel) Name() string {
	return "pushplus"
}

func (c *PushplusChannel) Send(text string) error {
	return c.SendWithContext(NotificationContext{Event: "通知", Text: text})
}

func (c *PushplusChannel) SendWithContext(ctx NotificationContext) error {
	title := fmt.Sprintf("[HiDeck] %s", ctx.Event)
	if label := ctx.DeviceLabel(); label != "未知设备" {
		title = fmt.Sprintf("[HiDeck] %s - %s", ctx.Event, label)
	}

	payload := map[string]interface{}{
		"token":    c.cfg.Token,
		"title":    title,
		"content":  ctx.Text,
		"template": "markdown",
	}

	if c.cfg.Topic != "" {
		payload["topic"] = c.cfg.Topic
	}

	channel := c.cfg.Channel
	if channel == "" {
		channel = "wechat"
	}
	payload["channel"] = channel

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, pushplusSendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: pushplusRequestTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("Pushplus 发送失败", "err", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("http status code %d", resp.StatusCode)
		logger.Warn("Pushplus 发送失败", "err", err)
		return err
	}

	return nil
}

func (c *PushplusChannel) RegisterCommand(cmd string, handler CommandHandler) {
	// Pushplus 不支持接收指令
}

func (c *PushplusChannel) Start() error {
	return nil
}

func (c *PushplusChannel) Close() error {
	return nil
}
