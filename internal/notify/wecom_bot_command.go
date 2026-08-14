package notify

import (
	"context"
	"sync"

	"github.com/yibaiba/hideck/pkg/logger"
)

type weComCommandContext struct {
	channel   *WeComBotChannel
	target    string
	requestID string
	stateMu   sync.Mutex
	sendMu    sync.Mutex
	released  bool
	pending   []string
}

func (c *weComCommandContext) Reply(text string) {
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
			logger.Warn("回复企业微信长连接命令失败", "err", err)
		}
	}()
}

func (c *weComCommandContext) release() {
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
			logger.Warn("回复企业微信长连接命令失败", "err", err)
		}
	}
}

func (c *weComCommandContext) respond(ctx context.Context, text string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.channel.sendText(ctx, c.target, c.requestID, text)
}
