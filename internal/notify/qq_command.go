package notify

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	qqbot "github.com/iniwex5/qqbot"
	"github.com/yibaiba/hideck/pkg/logger"
)

type qqCommandContext struct {
	conversation qqbot.Conversation
	stateMu      sync.Mutex
	sendMu       sync.Mutex
	released     bool
	pending      []qqCommandReply
}

type qqCommandReply struct {
	text        string
	attachments []CommandAttachment
}

func (c *qqCommandContext) Reply(text string) {
	if c == nil || c.conversation == nil {
		return
	}
	reply := qqCommandReply{text: text}
	if c.queue(reply) {
		return
	}
	go c.respondAndReport(reply)
}

func (c *qqCommandContext) ReplyWithAttachments(text string, attachments []CommandAttachment) {
	if c == nil || c.conversation == nil {
		return
	}
	reply := qqCommandReply{text: text, attachments: append([]CommandAttachment(nil), attachments...)}
	if c.queue(reply) {
		return
	}
	go c.respondAndReport(reply)
}

func (c *qqCommandContext) queue(reply qqCommandReply) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.released {
		return false
	}
	c.pending = append(c.pending, reply)
	return true
}

func (c *qqCommandContext) release() {
	c.stateMu.Lock()
	if c.released {
		c.stateMu.Unlock()
		return
	}
	c.released = true
	pending := append([]qqCommandReply(nil), c.pending...)
	c.pending = nil
	c.stateMu.Unlock()
	for _, reply := range pending {
		if len(reply.attachments) > 0 {
			c.respondAndReport(reply)
			continue
		}
		if err := c.respond(context.Background(), reply); err != nil {
			logger.Warn("回复 QQ 命令消息失败", "err", err)
		}
	}
}

func (c *qqCommandContext) respond(ctx context.Context, reply qqCommandReply) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	for _, attachment := range reply.attachments {
		delivery, err := qqVoiceDelivery(attachment)
		if err != nil {
			return fmt.Errorf("准备 QQ 录音附件失败: %w", err)
		}
		if _, err := c.conversation.Respond(ctx, delivery); err != nil {
			return fmt.Errorf("上传或发送 QQ 录音附件失败: %w", err)
		}
	}
	if strings.TrimSpace(reply.text) == "" {
		return nil
	}
	_, err := c.conversation.RespondText(ctx, reply.text)
	return err
}

func (c *qqCommandContext) respondAndReport(reply qqCommandReply) {
	if err := c.respond(context.Background(), reply); err != nil {
		logger.Warn("回复 QQ 命令录音失败", "err", err)
		failure := "录音发送失败\n原因    " + err.Error()
		if _, sendErr := c.conversation.RespondText(context.Background(), failure); sendErr != nil {
			logger.Warn("发送 QQ 录音失败说明失败", "err", sendErr)
		}
	}
}

func qqVoiceDelivery(attachment CommandAttachment) (qqbot.Delivery, error) {
	path := strings.TrimSpace(attachment.Path)
	if path == "" {
		return qqbot.Delivery{}, errors.New("录音路径为空")
	}
	if !strings.EqualFold(strings.TrimSpace(attachment.Codec), "MP3") ||
		!strings.EqualFold(filepath.Ext(path), ".mp3") {
		return qqbot.Delivery{}, errors.New("QQ 语音附件必须是 MP3")
	}
	fileName := strings.TrimSpace(attachment.Recording)
	if fileName == "" {
		fileName = filepath.Base(path)
	}
	return qqbot.Delivery{Kind: qqbot.Voice, MediaPath: path, FileName: fileName}, nil
}
