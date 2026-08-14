package notify

import (
	"context"
	"fmt"
	"strings"
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
	pending   []weComCommandReply
}

type weComCommandReply struct {
	text        string
	attachments []CommandAttachment
}

func (c *weComCommandContext) Reply(text string) {
	if c == nil || c.channel == nil {
		return
	}
	c.stateMu.Lock()
	if !c.released {
		c.pending = append(c.pending, weComCommandReply{text: text})
		c.stateMu.Unlock()
		return
	}
	c.stateMu.Unlock()
	go func() {
		if err := c.respond(context.Background(), weComCommandReply{text: text}); err != nil {
			logger.Warn("回复企业微信长连接命令失败", "err", err)
		}
	}()
}

func (c *weComCommandContext) ReplyWithAttachments(text string, attachments []CommandAttachment) {
	if c == nil || c.channel == nil {
		return
	}
	reply := weComCommandReply{text: text, attachments: append([]CommandAttachment(nil), attachments...)}
	c.stateMu.Lock()
	if !c.released {
		c.pending = append(c.pending, reply)
		c.stateMu.Unlock()
		return
	}
	c.stateMu.Unlock()
	go c.respondAndReport(reply)
}

func (c *weComCommandContext) release() {
	c.stateMu.Lock()
	if c.released {
		c.stateMu.Unlock()
		return
	}
	c.released = true
	pending := append([]weComCommandReply(nil), c.pending...)
	c.pending = nil
	c.stateMu.Unlock()
	for _, reply := range pending {
		if len(reply.attachments) > 0 {
			c.respondAndReport(reply)
			continue
		}
		if err := c.respond(context.Background(), reply); err != nil {
			logger.Warn("回复企业微信长连接命令失败", "err", err)
		}
	}
}

func (c *weComCommandContext) respond(ctx context.Context, reply weComCommandReply) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	plans := make([]weComMediaPlan, 0, len(reply.attachments))
	uploaded := make([]weComUploadedMedia, 0, len(reply.attachments))
	for _, attachment := range reply.attachments {
		plan, err := prepareWeComMedia(attachment)
		if err != nil {
			return fmt.Errorf("准备录音附件失败: %w", err)
		}
		media, err := c.channel.uploadMediaPlan(ctx, plan)
		if err != nil {
			return fmt.Errorf("上传录音附件失败: %w", err)
		}
		plans, uploaded = append(plans, plan), append(uploaded, media)
	}
	text := strings.TrimSpace(reply.text)
	for _, plan := range plans {
		if plan.note != "" {
			text += "\n说明    " + plan.note
		}
	}
	if err := c.channel.sendText(ctx, c.target, c.requestID, text); err != nil {
		return err
	}
	for _, media := range uploaded {
		if err := c.channel.sendUploadedMedia(ctx, c.target, c.requestID, media); err != nil {
			return fmt.Errorf("发送录音附件失败: %w", err)
		}
	}
	return nil
}

func (c *weComCommandContext) respondAndReport(reply weComCommandReply) {
	if err := c.respond(context.Background(), reply); err != nil {
		logger.Warn("回复企业微信长连接录音失败", "err", err)
		failure := strings.TrimSpace(reply.text)
		if failure != "" {
			failure += "\n"
		}
		failure += "录音发送失败\n原因    " + err.Error()
		if sendErr := c.channel.sendText(context.Background(), c.target, c.requestID, failure); sendErr != nil {
			logger.Warn("发送企业微信长连接录音失败说明失败", "err", sendErr)
		}
	}
}
