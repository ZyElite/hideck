//go:build linux && arm

package notify

import (
	"fmt"

	"github.com/yibaiba/hideck/internal/config"
)

// FeishuChannel is unavailable on linux/arm because the upstream Feishu SDK does
// not currently compile on 32-bit Go targets.
type FeishuChannel struct{}

func NewFeishuChannel(cfg config.FeishuConfig) (*FeishuChannel, error) {
	return NewFeishuChannelWithOptions(FeishuChannelOptions{Config: cfg})
}

func NewFeishuChannelWithOptions(options FeishuChannelOptions) (*FeishuChannel, error) {
	if !options.Config.Enabled {
		return nil, nil
	}
	return nil, fmt.Errorf("飞书通知渠道在 linux/arm 构建中不可用")
}

func (f *FeishuChannel) Name() string { return "feishu" }

func (f *FeishuChannel) Send(text string) error {
	return fmt.Errorf("飞书通知渠道在 linux/arm 构建中不可用")
}

func (f *FeishuChannel) RegisterCommand(cmd string, handler CommandHandler) {}

func (f *FeishuChannel) Start() error { return nil }

func (f *FeishuChannel) Close() error { return nil }
