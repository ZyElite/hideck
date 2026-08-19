//go:build !(linux && arm)

package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/yibaiba/hideck/internal/config"
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
	channel.handleMessageEvent(feishuTextEvent("oc_chat", "om_1", "p2p", "ou_owner", "你好"))
	if !reflect.DeepEqual(texts, []string{"帮助内容"}) {
		t.Fatalf("texts = %#v", texts)
	}
	state, err := store.Load()
	if err != nil || !reflect.DeepEqual(state.Feishu.ChatIDs, []string{"oc_chat"}) ||
		!reflect.DeepEqual(state.Feishu.AllowedUsers, []string{"ou_owner"}) {
		t.Fatalf("state = %+v, err = %v", state, err)
	}
}

func TestFeishuFirstPrivateBindingRemovesUnverifiedRuntimeChats(t *testing.T) {
	store := NewFileRuntimeStateStore(t.TempDir() + "/notification-state.json")
	if err := store.Save(RuntimeState{
		Feishu: FeishuRuntimeState{ChatIDs: []string{"oc_unverified"}},
	}); err != nil {
		t.Fatal(err)
	}
	channel := &FeishuChannel{
		cfg:        config.FeishuConfig{ChatIDs: []string{"oc_configured"}},
		stateStore: store,
		handlers: map[string]CommandHandler{
			"help": func(CommandContext, []string) string { return "帮助内容" },
		},
		replyText: func(*larkim.EventMessage, string) {},
	}
	channel.handleMessageEvent(feishuTextEvent("oc_owner", "om_owner", "p2p", "ou_owner", "你好"))
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.Feishu.ChatIDs, []string{"oc_owner"}) {
		t.Fatalf("runtime chat ids = %#v", state.Feishu.ChatIDs)
	}
	if !reflect.DeepEqual(state.Feishu.AllowedUsers, []string{"ou_owner"}) {
		t.Fatalf("allowed users = %#v", state.Feishu.AllowedUsers)
	}
	if !state.Feishu.BindingVerified {
		t.Fatal("binding was not marked verified")
	}
	if got := channel.notificationChatIDs(); !reflect.DeepEqual(got, []string{"oc_configured", "oc_owner"}) {
		t.Fatalf("notification chat ids = %#v", got)
	}
}

func TestFeishuChannelIgnoresUnverifiedRuntimeChatsOnStartup(t *testing.T) {
	store := NewFileRuntimeStateStore(t.TempDir() + "/notification-state.json")
	if err := store.Save(RuntimeState{
		Feishu: FeishuRuntimeState{
			ChatIDs: []string{"oc_unverified"}, AllowedUsers: []string{"ou_owner"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	channel, err := NewFeishuChannelWithOptions(FeishuChannelOptions{
		Config: config.FeishuConfig{
			Enabled: true, AppID: "cli_test", AppSecret: "secret",
			ChatIDs: []string{"oc_configured"},
		},
		StateStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := channel.notificationChatIDs(); !reflect.DeepEqual(got, []string{"oc_configured"}) {
		t.Fatalf("notification chat ids = %#v", got)
	}
}

func TestFeishuChannelLoadsVerifiedRuntimeChatsOnStartup(t *testing.T) {
	store := NewFileRuntimeStateStore(t.TempDir() + "/notification-state.json")
	if err := store.Save(RuntimeState{
		Feishu: FeishuRuntimeState{
			ChatIDs: []string{"oc_verified"}, AllowedUsers: []string{"ou_owner"},
			BindingVerified: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	channel, err := NewFeishuChannelWithOptions(FeishuChannelOptions{
		Config: config.FeishuConfig{
			Enabled: true, AppID: "cli_test", AppSecret: "secret",
			ChatIDs: []string{"oc_configured"},
		},
		StateStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := channel.notificationChatIDs(); !reflect.DeepEqual(
		got, []string{"oc_configured", "oc_verified"},
	) {
		t.Fatalf("notification chat ids = %#v", got)
	}
}

func TestFeishuLaterPlainTextAsksForHelp(t *testing.T) {
	var texts []string
	channel := &FeishuChannel{
		chatIDs:      []string{"oc_chat"},
		allowedUsers: []string{"ou_owner"},
		handlers:     map[string]CommandHandler{"help": func(CommandContext, []string) string { return "帮助内容" }},
		replyText:    func(_ *larkim.EventMessage, text string) { texts = append(texts, text) },
	}
	channel.handleMessageEvent(feishuTextEvent("oc_chat", "om_2", "p2p", "ou_owner", "在吗"))
	if !reflect.DeepEqual(texts, []string{"请发送 /help 查看可用命令"}) {
		t.Fatalf("texts = %#v", texts)
	}
}

func TestFeishuMentionedCommandStillRuns(t *testing.T) {
	var texts []string
	channel := &FeishuChannel{
		chatIDs:      []string{"oc_group"},
		allowedUsers: []string{"ou_owner"},
		handlers: map[string]CommandHandler{
			"list": func(CommandContext, []string) string { return "设备列表" },
		},
		replyText: func(_ *larkim.EventMessage, text string) { texts = append(texts, text) },
	}
	channel.handleMessageEvent(feishuTextEvent(
		"oc_group", "om_3", "group", "ou_owner", `<at user_id="ou_bot">@hideck</at> /list`,
	))
	if !reflect.DeepEqual(texts, []string{"设备列表"}) {
		t.Fatalf("texts = %#v", texts)
	}
}

func TestFeishuRejectsLaterDirectUserAndUnboundGroup(t *testing.T) {
	var texts []string
	channel := &FeishuChannel{
		chatIDs:      []string{"oc_owner", "oc_group"},
		allowedUsers: []string{"ou_owner"},
		handlers: map[string]CommandHandler{
			"list": func(CommandContext, []string) string { return "设备列表" },
		},
		replyText: func(_ *larkim.EventMessage, text string) { texts = append(texts, text) },
	}
	channel.handleMessageEvent(feishuTextEvent("oc_other", "om_4", "p2p", "ou_other", "/list"))
	channel.handleMessageEvent(feishuTextEvent("oc_unknown_group", "om_5", "group", "ou_owner", "/list"))
	channel.handleMessageEvent(feishuTextEvent("oc_group", "om_6", "group", "ou_other", "/list"))
	if len(texts) != 0 {
		t.Fatalf("unauthorized replies = %#v", texts)
	}
}

func TestFeishuBindingFailureDoesNotExecuteCommand(t *testing.T) {
	store := &telegramToggleStateStore{updateFail: true}
	executed := false
	channel := &FeishuChannel{
		stateStore: store,
		handlers: map[string]CommandHandler{
			"list": func(CommandContext, []string) string { executed = true; return "设备列表" },
		},
		replyText: func(*larkim.EventMessage, string) {},
	}
	channel.handleMessageEvent(feishuTextEvent("oc_chat", "om_7", "p2p", "ou_owner", "/list"))
	if executed {
		t.Fatal("command executed after binding persistence failed")
	}
}

func TestFeishuCommandContextKeysConfirmationBySender(t *testing.T) {
	ctx := &feishuCommandContext{senderID: "ou_owner"}
	if got := ctx.UserKey(); got != "feishu:ou_owner" {
		t.Fatalf("UserKey() = %q", got)
	}
}

func TestFeishuReplyFailureDoesNotBroadcastToOtherChats(t *testing.T) {
	var directTargets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "test-token", "expire": 7200,
			})
		case strings.HasSuffix(r.URL.Path, "/reply"):
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "msg": "reply rejected"})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var body struct {
				ReceiveID string `json:"receive_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			directTargets = append(directTargets, body.ReceiveID)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 2, "msg": "send rejected"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	channel := &FeishuChannel{
		client:  lark.NewClient("cli_test", "secret", lark.WithOpenBaseUrl(server.URL)),
		chatIDs: []string{"oc_other"},
	}
	channel.replyToMessage(&larkim.EventMessage{
		MessageId: strPtr("om_source"), ChatId: strPtr("oc_source"),
	}, "private response")
	if !reflect.DeepEqual(directTargets, []string{"oc_source"}) {
		t.Fatalf("direct targets = %#v", directTargets)
	}
}

func feishuTextEvent(chatID, messageID, chatType, senderID, text string) *larkim.P2MessageReceiveV1 {
	msgType := "text"
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		panic(err)
	}
	contentText := string(content)
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{SenderId: &larkim.UserId{OpenId: strPtr(senderID)}},
			Message: &larkim.EventMessage{
				ChatId:      &chatID,
				MessageId:   &messageID,
				ChatType:    &chatType,
				MessageType: &msgType,
				Content:     &contentText,
			},
		},
	}
}
