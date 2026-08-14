package notify

import (
	"context"
	"sync"

	"github.com/yibaiba/hideck/pkg/logger"
)

type weixinCommandContext struct {
	channel  *WeixinChannel
	target   string
	stateMu  sync.Mutex
	sendMu   sync.Mutex
	released bool
	pending  []string
}

func (c *weixinCommandContext) Reply(text string) {
	if c == nil || c.channel == nil {
		return
	}
	c.stateMu.Lock()
	if !c.released {
		c.pending = append(c.pending, text)
		c.stateMu.Unlock()
		return
	}
	c.stateMu.Unlock()
	go func() {
		if err := c.respond(context.Background(), text); err != nil {
			logger.Warn("回复个人微信命令消息失败", "err", err)
		}
	}()
}

func (c *weixinCommandContext) release() {
	c.stateMu.Lock()
	if c.released {
		c.stateMu.Unlock()
		return
	}
	c.released = true
	pending := append([]string(nil), c.pending...)
	c.pending = nil
	c.stateMu.Unlock()
	for _, text := range pending {
		if err := c.respond(context.Background(), text); err != nil {
			logger.Warn("回复个人微信命令消息失败", "err", err)
		}
	}
}

func (c *weixinCommandContext) respond(ctx context.Context, text string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.channel.sendTo(ctx, c.target, text)
}
