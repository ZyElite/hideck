//go:build !(linux && arm)

package notify

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/yibaiba/hideck/pkg/logger"
)

var feishuMentionPattern = regexp.MustCompile(`(?is)<at\b[^>]*>.*?</at>|@_user_\d+`)

func (f *FeishuChannel) handleMessageEvent(event *larkim.P2MessageReceiveV1) {
	if f == nil || event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	msg := event.Event.Message
	chatID := feishuMessageChatID(msg)
	text, msgType, err := feishuInboundText(msg)
	if err != nil {
		logger.Debug("忽略飞书消息", "chat_id", chatID, "type", msgType, "err", err)
		return
	}
	logger.Info("收到飞书消息", "chat_id", chatID, "type", msgType, "chars", len([]rune(text)))
	bound, bindErr := f.bindChatID(chatID)
	if bindErr != nil {
		logger.Warn("保存飞书会话失败", "chat_id", chatID, "err", bindErr)
	}
	ctx := &feishuCommandContext{channel: f, msg: msg}
	if bound && !isHelpCommand(text) && f.handlers != nil {
		if help := f.handlers["help"]; help != nil {
			if response := help(ctx, nil); response != "" {
				ctx.Reply(response)
			}
		}
	}
	name, args, err := parseCommand(text)
	if errors.Is(err, ErrInvalidCommand) {
		if !bound {
			ctx.Reply("请发送 /help 查看可用命令")
		}
		return
	}
	if err != nil {
		return
	}
	if f.handlers == nil {
		return
	}
	handler, ok := f.handlers[name]
	if !ok {
		ctx.Reply(unknownCommandReply(name))
		return
	}
	logger.Info("收到飞书命令", "command", name, "args", args)
	if response := handler(ctx, args); response != "" {
		ctx.Reply(response)
	}
}

func feishuInboundText(msg *larkim.EventMessage) (string, string, error) {
	if msg == nil {
		return "", "", errors.New("消息为空")
	}
	msgType := strings.TrimSpace(ptrText(msg.MessageType))
	if msgType != "" && !strings.EqualFold(msgType, "text") {
		return "", msgType, errors.New("只处理文本消息")
	}
	if msg.Content == nil {
		return "", msgType, errors.New("消息内容为空")
	}
	var textContent struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*msg.Content), &textContent); err != nil {
		return "", msgType, err
	}
	text := stripFeishuMentions(textContent.Text)
	if text == "" {
		return "", msgType, errors.New("去掉 @ 之后没有文本")
	}
	return text, valueOrDefault(msgType, "text"), nil
}

func stripFeishuMentions(text string) string {
	text = feishuMentionPattern.ReplaceAllString(text, " ")
	return strings.Join(strings.Fields(text), " ")
}

func (f *FeishuChannel) bindChatID(chatID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return false, nil
	}
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	if containsString(f.chatIDs, chatID) {
		return false, nil
	}
	if f.stateStore == nil {
		f.chatIDs = append(f.chatIDs, chatID)
		return true, nil
	}
	bound := false
	var stored []string
	if err := f.stateStore.Update(func(state *RuntimeState) error {
		if !containsString(state.Feishu.ChatIDs, chatID) {
			state.Feishu.ChatIDs = append(state.Feishu.ChatIDs, chatID)
			bound = true
		}
		stored = append([]string(nil), state.Feishu.ChatIDs...)
		return nil
	}); err != nil {
		return false, err
	}
	f.chatIDs = mergeUniqueStrings(f.chatIDs, stored)
	return bound, nil
}

func (f *FeishuChannel) notificationChatIDs() []string {
	if f == nil {
		return nil
	}
	f.stateMu.RLock()
	defer f.stateMu.RUnlock()
	return append([]string(nil), f.chatIDs...)
}

func mergeUniqueStrings(groups ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			id := strings.TrimSpace(value)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}
