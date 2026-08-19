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
	senderID := feishuSenderID(event)
	text, msgType, err := feishuInboundText(msg)
	if err != nil {
		logger.Debug("忽略飞书消息", "chat_id", chatID, "type", msgType, "err", err)
		return
	}
	allowed, bound, authErr := f.authorizeMessage(event)
	if authErr != nil {
		logger.Warn("飞书消息鉴权失败", "chat_id", chatID, "err", authErr)
		return
	}
	if !allowed {
		logger.Warn("忽略未授权的飞书消息", "chat_id", chatID, "type", msgType)
		return
	}
	logger.Info("收到飞书消息", "chat_id", chatID, "type", msgType, "chars", len([]rune(text)))
	ctx := &feishuCommandContext{channel: f, msg: msg, senderID: senderID}
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

func (f *FeishuChannel) authorizeMessage(event *larkim.P2MessageReceiveV1) (bool, bool, error) {
	if f == nil || event == nil || event.Event == nil || event.Event.Message == nil {
		return false, false, nil
	}
	chatID := feishuMessageChatID(event.Event.Message)
	senderID := feishuSenderID(event)
	kind := feishuMessageKind(event.Event.Message)
	if chatID == "" || senderID == "" || kind == "" {
		return false, false, nil
	}
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	senderAllowed := containsString(f.allowedUsers, senderID)
	if kind == "group" {
		return senderAllowed && containsString(f.chatIDs, chatID), false, nil
	}
	if len(f.allowedUsers) > 0 && !senderAllowed {
		return false, false, nil
	}
	if senderAllowed && containsString(f.chatIDs, chatID) {
		return true, false, nil
	}
	bound, err := f.persistDirectBindingLocked(chatID, senderID)
	return err == nil, bound, err
}

func (f *FeishuChannel) persistDirectBindingLocked(chatID, senderID string) (bool, error) {
	if f.stateStore == nil {
		f.chatIDs = mergeUniqueStrings(f.chatIDs, []string{chatID})
		f.allowedUsers = mergeUniqueStrings(f.allowedUsers, []string{senderID})
		return true, nil
	}
	bound := false
	var stored FeishuRuntimeState
	if err := f.stateStore.Update(func(state *RuntimeState) error {
		if !containsString(state.Feishu.ChatIDs, chatID) {
			state.Feishu.ChatIDs = append(state.Feishu.ChatIDs, chatID)
			bound = true
		}
		if !containsString(state.Feishu.AllowedUsers, senderID) {
			state.Feishu.AllowedUsers = append(state.Feishu.AllowedUsers, senderID)
			bound = true
		}
		stored = cloneRuntimeState(*state).Feishu
		return nil
	}); err != nil {
		return false, err
	}
	f.chatIDs = mergeUniqueStrings(f.chatIDs, stored.ChatIDs)
	f.allowedUsers = mergeUniqueStrings(f.allowedUsers, stored.AllowedUsers)
	return bound, nil
}

func feishuSenderID(event *larkim.P2MessageReceiveV1) string {
	if event == nil || event.Event == nil || event.Event.Sender == nil || event.Event.Sender.SenderId == nil {
		return ""
	}
	sender := event.Event.Sender.SenderId
	return firstNonEmpty(ptrText(sender.OpenId), ptrText(sender.UserId), ptrText(sender.UnionId))
}

func feishuMessageKind(msg *larkim.EventMessage) string {
	if msg == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(ptrText(msg.ChatType))) {
	case "p2p":
		return "direct"
	case "group", "topic_group":
		return "group"
	default:
		return ""
	}
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
