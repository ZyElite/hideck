package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/yibaiba/hideck/pkg/logger"
)

func (w *WeixinChannel) pollLoop(ctx context.Context) error {
	logger.Info("个人微信命令监听已启动")
	failures := 0
	for {
		state := w.snapshotState()
		response, err := w.client.getUpdates(ctx, weixinCredentials(state), state.Weixin.SyncBuffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			failures++
			logger.Warn("个人微信长轮询失败", "attempt", failures, "err", err)
			if !waitContext(ctx, pollRetryDelay(failures)) {
				return nil
			}
			continue
		}
		if delay, err := weixinResponseError(response); err != nil {
			logger.Warn("个人微信长轮询返回错误", "err", err)
			if !waitContext(ctx, delay) {
				return nil
			}
			continue
		}
		failures = 0
		if err := w.processUpdates(ctx, response); err != nil {
			logger.Warn("处理个人微信消息失败，保留旧游标等待重试", "err", err)
			if !waitContext(ctx, weixinRetryDelay) {
				return nil
			}
		}
	}
}

func (w *WeixinChannel) processUpdates(ctx context.Context, response weixinUpdatesResponse) error {
	for _, message := range response.Messages {
		if err := w.processMessage(ctx, message); err != nil {
			return err
		}
	}
	if strings.TrimSpace(response.GetUpdatesBuffer) == "" {
		return nil
	}
	return w.updateState(func(state *RuntimeState) {
		state.Weixin.SyncBuffer = response.GetUpdatesBuffer
	})
}

func (w *WeixinChannel) processMessage(ctx context.Context, message weixinMessage) error {
	sender := strings.TrimSpace(message.FromUserID)
	text := message.text()
	state := w.snapshotState()
	if sender == "" || sender == state.Weixin.AccountID || text == "" {
		if sender != "" && sender != state.Weixin.AccountID && text == "" {
			logger.Debug("个人微信消息没有文本，已忽略", "sender", sender)
		}
		return nil
	}
	logger.Info("收到个人微信消息", "sender", sender, "chars", len([]rune(text)))
	chatKind, chatID := message.chat(state.Weixin.AccountID)
	allowed, bound, err := w.authorizeMessage(weixinAuthorizationRequest{
		Kind: chatKind, ChatID: chatID, Sender: sender, ContextToken: message.ContextToken,
	})
	if err != nil || !allowed {
		return err
	}
	if bound && !isHelpCommand(text) {
		if err := w.executeMessage(ctx, chatID, "/help"); err != nil {
			return fmt.Errorf("发送个人微信注册帮助失败: %w", err)
		}
	}
	return w.executeMessage(ctx, chatID, text)
}
