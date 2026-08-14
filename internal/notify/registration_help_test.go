package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	qqbot "github.com/iniwex5/qqbot"
	"github.com/yibaiba/hideck/internal/config"
)

func TestManagerSendsCurrentHelpToOneRegistrationTarget(t *testing.T) {
	channel := &registrationHelpCaptureChannel{name: "qq"}
	service := NewCommandService(nil)
	service.SetHelpDevicesProvider(func() []HelpDevice {
		return []HelpDevice{{ID: "modem-1", Name: "主卡"}}
	})
	manager := &Manager{channels: []Channel{channel}, commandService: service}

	if err := manager.SendRegistrationHelp("qq", "user-1"); err != nil {
		t.Fatal(err)
	}
	target, message := channel.last()
	if target != "user-1" || !strings.Contains(message, "modem-1") || !strings.Contains(message, "/help") {
		t.Fatalf("registration help target=%q message=%q", target, message)
	}
}

func TestQQRegistrationHelpIsRestrictedToAllowedDirectTarget(t *testing.T) {
	app := &fakeQQApp{}
	channel := &QQChannel{app: app, allowedRecipients: map[string]qqbot.Recipient{
		"direct:user-1": {Kind: qqbot.DirectRecipient, ID: "user-1"},
	}}
	if err := channel.SendRegistrationHelp("user-1", "帮助内容"); err != nil {
		t.Fatal(err)
	}
	if len(app.sent) != 1 || app.sent[0].To.ID != "user-1" || app.sent[0].Body != "帮助内容" {
		t.Fatalf("deliveries = %+v", app.sent)
	}
	if err := channel.SendRegistrationHelp("user-2", "帮助内容"); err != ErrQQRecipientNotAllowed {
		t.Fatalf("unallowed error = %v", err)
	}
}

func TestTelegramFirstBindingSendsHelpBeforeRequestedCommand(t *testing.T) {
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	channel, capture := newTelegramTestChannel(t, store, 42, 0)
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "帮助内容" })
	channel.RegisterCommand("status", func(CommandContext, []string) string { return "状态内容" })

	channel.handleMessage(telegramCommandMessage("/status", "private", 42, 42))
	messages := waitForTelegramMessages(t, capture, 2)
	if messages[0] != "帮助内容" || messages[1] != "状态内容" {
		t.Fatalf("messages = %v", messages)
	}
	channel.handleMessage(telegramCommandMessage("/status", "private", 42, 42))
	messages = waitForTelegramMessages(t, capture, 3)
	if messages[2] != "状态内容" {
		t.Fatalf("second command messages = %v", messages)
	}
}

func TestWeixinFirstBindingSendsHelpBeforeRequestedCommand(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Items []struct {
					Text struct {
						Text string `json:"text"`
					} `json:"text_item"`
				} `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode message: %v", err)
		}
		mu.Lock()
		sent = append(sent, body.Message.Items[0].Text.Text)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
	}))
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{AccountID: "bot-1", Token: "token-1", BaseURL: provider.URL}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true}, StateStore: store, HTTPClient: provider.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "帮助内容" })
	channel.RegisterCommand("status", func(CommandContext, []string) string { return "状态内容" })
	message := weixinMessage{FromUserID: "user-1", ToUserID: "bot-1", ContextToken: "context-1", MessageType: 1}
	message.Items = textWeixinItems("/status")
	if err := channel.processMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(sent, ",") != "帮助内容,状态内容" {
		t.Fatalf("messages = %v", sent)
	}
}

func TestWeComFirstBindingSendsHelpBeforeRequestedCommand(t *testing.T) {
	helpPush := make(chan map[string]any, 1)
	statusReply := make(chan map[string]any, 1)
	provider := newWeComWebSocketServer(t, func(conn *websocket.Conn, _ int) {
		authenticateWeComTestConnection(t, conn)
		writeWeComTestFrame(t, conn, map[string]any{
			"cmd": weComCommandCallback, "headers": map[string]string{"req_id": "callback-1"},
			"body": map[string]any{
				"msgid": "message-1", "chatid": "direct-1", "chattype": "single", "msgtype": "text",
				"from": map[string]string{"userid": "user-1"}, "text": map[string]string{"content": "/status"},
			},
		})
		for {
			frame, err := readWeComTestFrame(conn)
			if err != nil {
				return
			}
			switch frameCommand(frame) {
			case weComCommandSend:
				helpPush <- frame
				ackWeComTestFrame(t, conn, frame)
			case weComCommandRespond:
				statusReply <- frame
				ackWeComTestFrame(t, conn, frame)
			}
		}
	})
	defer provider.Close()
	channel := newWeComBotTestChannel(t, provider, NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json")), nil)
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "帮助内容" })
	channel.RegisterCommand("status", func(CommandContext, []string) string { return "状态内容" })
	startDone := startWeComBotTestChannel(channel)

	push := receiveWeComTestFrame(t, helpPush)
	reply := receiveWeComTestFrame(t, statusReply)
	if frameChatID(push) != "direct-1" || frameMarkdown(push) != "帮助内容" {
		t.Fatalf("help push = %#v", push)
	}
	if frameRequestID(reply) != "callback-1" || frameMarkdown(reply) != "状态内容" {
		t.Fatalf("status reply = %#v", reply)
	}
	closeWeComBotTestChannel(t, channel, startDone)
}

type registrationHelpCaptureChannel struct {
	mu      sync.Mutex
	name    string
	target  string
	message string
}

func (c *registrationHelpCaptureChannel) Name() string                           { return c.name }
func (c *registrationHelpCaptureChannel) Send(string) error                      { return nil }
func (c *registrationHelpCaptureChannel) RegisterCommand(string, CommandHandler) {}
func (c *registrationHelpCaptureChannel) Start() error                           { return nil }
func (c *registrationHelpCaptureChannel) Close() error                           { return nil }
func (c *registrationHelpCaptureChannel) SendRegistrationHelp(target, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.target, c.message = target, message
	return nil
}
func (c *registrationHelpCaptureChannel) last() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.target, c.message
}
