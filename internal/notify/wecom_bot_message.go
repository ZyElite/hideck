package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type weComCallbackBody struct {
	MessageID   string `json:"msgid"`
	ChatID      string `json:"chatid"`
	ChatType    string `json:"chattype"`
	MessageType string `json:"msgtype"`
	From        struct {
		UserID string `json:"userid"`
	} `json:"from"`
	Text struct {
		Content string `json:"content"`
	} `json:"text"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	Mixed struct {
		Items []struct {
			MessageType string `json:"msgtype"`
			Text        struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"msg_item"`
	} `json:"mixed"`
}

type weComAuthorizationRequest struct {
	kind   string
	chatID string
	sender string
}

func (w *WeComBotChannel) processCallback(ctx context.Context, frame weComFrame) error {
	var message weComCallbackBody
	if err := json.Unmarshal(frame.Body, &message); err != nil {
		return errors.New("企业微信回调消息格式无效")
	}
	sender := strings.TrimSpace(message.From.UserID)
	chatID := strings.TrimSpace(message.ChatID)
	if chatID == "" {
		chatID = sender
	}
	if sender == "" || chatID == "" {
		return nil
	}
	kind := "direct"
	if strings.EqualFold(strings.TrimSpace(message.ChatType), "group") {
		kind = "group"
	}
	text := extractWeComText(message)
	if kind == "group" {
		text = stripWeComMention(text)
	}
	if text == "" {
		return nil
	}
	allowed, bound, err := w.authorizeMessage(weComAuthorizationRequest{kind: kind, chatID: chatID, sender: sender})
	if err != nil || !allowed {
		return err
	}
	messageID := strings.TrimSpace(message.MessageID)
	if messageID == "" {
		messageID = strings.TrimSpace(frame.Headers.RequestID)
	}
	if messageID != "" && w.seenMessage(messageID) {
		return nil
	}
	if bound && !isHelpCommand(text) {
		if err := w.executeMessage(ctx, chatID, "", "/help"); err != nil {
			return fmt.Errorf("发送企业微信注册帮助失败: %w", err)
		}
	}
	return w.executeMessage(ctx, chatID, frame.Headers.RequestID, text)
}

func (w *WeComBotChannel) authorizeMessage(request weComAuthorizationRequest) (bool, bool, error) {
	w.authMu.Lock()
	defer w.authMu.Unlock()
	state := w.snapshotState()
	if request.kind == "group" {
		allowed := containsString(w.config.AllowedGroupIDs, request.chatID) && w.allowedDirect(state, request.sender)
		return allowed, false, nil
	}
	allowed := w.allowedDirect(state, request.sender)
	changed := false
	bound := false
	if !allowed {
		hasBinding := len(w.config.AllowedUserIDs) > 0 || len(state.WeComBot.AllowedUsers) > 0 || state.WeComBot.DefaultTarget != ""
		if hasBinding {
			return false, false, nil
		}
		state.WeComBot.AllowedUsers = append(state.WeComBot.AllowedUsers, request.sender)
		allowed = true
		changed = true
		bound = true
	}
	if state.WeComBot.DefaultTarget == "" {
		state.WeComBot.DefaultTarget = request.chatID
		changed = true
		bound = true
	}
	if changed {
		if err := w.saveState(state); err != nil {
			return false, false, err
		}
	}
	return allowed, bound, nil
}

func (w *WeComBotChannel) allowedDirect(state RuntimeState, userID string) bool {
	return containsString(w.config.AllowedUserIDs, userID) || containsString(state.WeComBot.AllowedUsers, userID)
}

func (w *WeComBotChannel) saveState(state RuntimeState) error {
	var stored RuntimeState
	if err := w.stateStore.Update(func(current *RuntimeState) error {
		current.WeComBot = cloneRuntimeState(state).WeComBot
		stored = cloneRuntimeState(*current)
		return nil
	}); err != nil {
		return err
	}
	w.stateMu.Lock()
	w.state = stored
	w.stateMu.Unlock()
	return nil
}

func (w *WeComBotChannel) executeMessage(ctx context.Context, chatID, requestID, text string) error {
	name, args, err := parseCommand(text)
	if errors.Is(err, ErrInvalidCommand) {
		return w.sendText(ctx, chatID, requestID, "请发送 /help 查看可用命令")
	}
	if err != nil {
		return err
	}
	w.commandMu.RLock()
	handler := w.commands[name]
	w.commandMu.RUnlock()
	if handler == nil {
		return w.sendText(ctx, chatID, requestID, unknownCommandReply(text))
	}
	commandContext := &weComCommandContext{channel: w, target: chatID, requestID: requestID}
	defer commandContext.release()
	if response := handler(commandContext, args); response != "" {
		return commandContext.respond(ctx, weComCommandReply{text: response})
	}
	return nil
}

func (w *WeComBotChannel) sendText(ctx context.Context, target, requestID, text string) error {
	content := truncateWeComText(strings.TrimSpace(text))
	if content == "" {
		return nil
	}
	command := weComCommandSend
	if strings.TrimSpace(requestID) != "" {
		command = weComCommandRespond
	}
	body := map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": content}}
	if command == weComCommandSend {
		body["chatid"] = strings.TrimSpace(target)
	}
	_, err := w.sendRequest(ctx, command, strings.TrimSpace(requestID), body)
	return err
}

func extractWeComText(message weComCallbackBody) string {
	if strings.EqualFold(message.MessageType, "mixed") {
		parts := make([]string, 0, len(message.Mixed.Items))
		for _, item := range message.Mixed.Items {
			if strings.EqualFold(item.MessageType, "text") && strings.TrimSpace(item.Text.Content) != "" {
				parts = append(parts, strings.TrimSpace(item.Text.Content))
			}
		}
		return strings.Join(parts, "\n")
	}
	if text := strings.TrimSpace(message.Text.Content); text != "" {
		return text
	}
	return strings.TrimSpace(message.Voice.Content)
}

func stripWeComMention(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "@") {
		return text
	}
	index := strings.IndexAny(text, " \t\r\n")
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(text[index:])
}

func truncateWeComText(text string) string {
	runes := []rune(text)
	if len(runes) <= weComMaxMessageRunes {
		return text
	}
	return string(runes[:weComMaxMessageRunes])
}

func (w *WeComBotChannel) seenMessage(messageID string) bool {
	w.seenMu.Lock()
	defer w.seenMu.Unlock()
	if _, exists := w.seen[messageID]; exists {
		return true
	}
	w.seen[messageID] = struct{}{}
	w.seenOrder = append(w.seenOrder, messageID)
	if len(w.seenOrder) > weComSeenMessageLimit {
		oldest := w.seenOrder[0]
		w.seenOrder = w.seenOrder[1:]
		delete(w.seen, oldest)
	}
	return false
}
