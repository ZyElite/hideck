//go:build !(linux && arm)

package notify

import (
	"encoding/json"
	"reflect"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestStripFeishuMentions(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/help", "/help"},
		{"<at user_id=\"ou_1\">@bot</at> /list", "/list"},
		{"@_user_1 /status modem-1", "/status modem-1"},
		{"@_user_1/help", "/help"},
		{"你好", "你好"},
		{"<at user_id=\"ou_1\">@bot</at>", ""},
	}
	for _, item := range cases {
		if got := stripFeishuMentions(item.in); got != item.want {
			t.Fatalf("stripFeishuMentions(%q) = %q, want %q", item.in, got, item.want)
		}
	}
}

func TestFeishuAnyTextRepliesWithHelpOnFirstMessage(t *testing.T) {
	store := &telegramToggleStateStore{}
	var texts []string
	channel := &FeishuChannel{
		stateStore: store,
		handlers: map[string]CommandHandler{
			"help": func(CommandContext, []string) string { return "帮助内容" },
			"list": func(CommandContext, []string) string { return "设备列表" },
		},
		replyText: func(_ *larkim.EventMessage, text string) { texts = append(texts, text) },
	}
	channel.handleMessageEvent(feishuTextEvent("oc_chat", "om_1", "你好"))
	if !reflect.DeepEqual(texts, []string{"帮助内容"}) {
		t.Fatalf("texts = %#v", texts)
	}
	state, err := store.Load()
	if err != nil || !reflect.DeepEqual(state.Feishu.ChatIDs, []string{"oc_chat"}) {
		t.Fatalf("state = %+v, err = %v", state, err)
	}
}

func TestFeishuLaterPlainTextAsksForHelp(t *testing.T) {
	var texts []string
	channel := &FeishuChannel{
		chatIDs:   []string{"oc_chat"},
		handlers:  map[string]CommandHandler{"help": func(CommandContext, []string) string { return "帮助内容" }},
		replyText: func(_ *larkim.EventMessage, text string) { texts = append(texts, text) },
	}
	channel.handleMessageEvent(feishuTextEvent("oc_chat", "om_2", "在吗"))
	if !reflect.DeepEqual(texts, []string{"请发送 /help 查看可用命令"}) {
		t.Fatalf("texts = %#v", texts)
	}
}

func TestFeishuMentionedCommandStillRuns(t *testing.T) {
	var texts []string
	channel := &FeishuChannel{
		chatIDs: []string{"oc_group"},
		handlers: map[string]CommandHandler{
			"list": func(CommandContext, []string) string { return "设备列表" },
		},
		replyText: func(_ *larkim.EventMessage, text string) { texts = append(texts, text) },
	}
	channel.handleMessageEvent(feishuTextEvent("oc_group", "om_3", `<at user_id="ou_bot">@hideck</at> /list`))
	if !reflect.DeepEqual(texts, []string{"设备列表"}) {
		t.Fatalf("texts = %#v", texts)
	}
}

func feishuTextEvent(chatID, messageID, text string) *larkim.P2MessageReceiveV1 {
	msgType := "text"
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		panic(err)
	}
	contentText := string(content)
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatId:      &chatID,
				MessageId:   &messageID,
				MessageType: &msgType,
				Content:     &contentText,
			},
		},
	}
}
