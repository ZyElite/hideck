package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/yibaiba/hideck/pkg/logger"
)

type weixinCommandContext struct {
	channel  *WeixinChannel
	target   string
	stateMu  sync.Mutex
	sendMu   sync.Mutex
	released bool
	pending  []weixinCommandReply
}

type weixinCommandReply struct {
	text        string
	attachments []CommandAttachment
}

func (c *weixinCommandContext) Reply(text string) {
	if c == nil || c.channel == nil {
		return
	}
	c.stateMu.Lock()
	if !c.released {
		c.pending = append(c.pending, weixinCommandReply{text: text})
		c.stateMu.Unlock()
		return
	}
	c.stateMu.Unlock()
	go func() {
		if err := c.respond(context.Background(), weixinCommandReply{text: text}); err != nil {
			logger.Warn("回复个人微信命令消息失败", "err", err)
		}
	}()
}

func (c *weixinCommandContext) ReplyWithAttachments(text string, attachments []CommandAttachment) {
	if c == nil || c.channel == nil {
		return
	}
	reply := weixinCommandReply{text: text, attachments: append([]CommandAttachment(nil), attachments...)}
	c.stateMu.Lock()
	if !c.released {
		c.pending = append(c.pending, reply)
		c.stateMu.Unlock()
		return
	}
	c.stateMu.Unlock()
	go c.respondAndReport(reply)
}

func (c *weixinCommandContext) release() {
	c.stateMu.Lock()
	if c.released {
		c.stateMu.Unlock()
		return
	}
	c.released = true
	pending := append([]weixinCommandReply(nil), c.pending...)
	c.pending = nil
	c.stateMu.Unlock()
	for _, reply := range pending {
		if len(reply.attachments) > 0 {
			c.respondAndReport(reply)
			continue
		}
		if err := c.respond(context.Background(), reply); err != nil {
			logger.Warn("回复个人微信命令消息失败", "err", err)
		}
	}
}

func (c *weixinCommandContext) respond(ctx context.Context, reply weixinCommandReply) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	for _, attachment := range reply.attachments {
		if err := c.channel.sendAttachment(ctx, c.target, attachment); err != nil {
			return fmt.Errorf("发送录音附件失败: %w", err)
		}
	}
	if strings.TrimSpace(reply.text) != "" {
		if err := c.channel.sendTo(ctx, c.target, reply.text); err != nil {
			return err
		}
	}
	return nil
}

func (c *weixinCommandContext) respondAndReport(reply weixinCommandReply) {
	if err := c.respond(context.Background(), reply); err != nil {
		logger.Warn("回复个人微信命令附件失败", "err", err)
		failure := "录音发送失败\n原因    " + err.Error()
		if sendErr := c.channel.sendTo(context.Background(), c.target, failure); sendErr != nil {
			logger.Warn("发送个人微信录音失败说明失败", "err", sendErr)
		}
	}
}
