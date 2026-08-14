package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const telegramTestToken = "123456:test-token"

type telegramAPICapture struct {
	mu       sync.Mutex
	methods  []string
	messages []string
	commands []tgbotapi.BotCommand
}

func (c *telegramAPICapture) handler(w http.ResponseWriter, r *http.Request) {
	method := filepath.Base(r.URL.Path)
	c.mu.Lock()
	c.methods = append(c.methods, method)
	c.mu.Unlock()
	switch method {
	case "getMe":
		writeTelegramResult(w, map[string]any{
			"id": 999, "is_bot": true, "first_name": "VoHive", "username": "vohive_test_bot",
		})
	case "setMyCommands":
		_ = r.ParseForm()
		var commands []tgbotapi.BotCommand
		_ = json.Unmarshal([]byte(r.Form.Get("commands")), &commands)
		c.mu.Lock()
		c.commands = commands
		c.mu.Unlock()
		writeTelegramResult(w, true)
	case "sendMessage":
		_ = r.ParseForm()
		text := r.Form.Get("text")
		c.mu.Lock()
		c.messages = append(c.messages, text)
		c.mu.Unlock()
		writeTelegramResult(w, map[string]any{
			"message_id": 1, "date": 1, "chat": map[string]any{"id": 42, "type": "private"}, "text": text,
		})
	default:
		http.Error(w, `{"ok":false,"description":"unsupported"}`, http.StatusBadRequest)
	}
}

func writeTelegramResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func newTelegramTestChannel(
	t *testing.T,
	store RuntimeStateStore,
	adminID int64,
	chatID int64,
) (*TelegramChannel, *telegramAPICapture) {
	t.Helper()
	capture := &telegramAPICapture{}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	t.Cleanup(server.Close)
	channel, err := NewTelegramChannelWithOptions(TelegramChannelOptions{
		Config: config.TelegramConfig{
			Enabled: true, BotToken: telegramTestToken, AdminID: adminID, ChatID: chatID,
			BaseURL: server.URL + "/bot%s/%s",
		},
		StateStore: store,
	})
	if err != nil {
		t.Fatalf("NewTelegramChannelWithOptions() error = %v", err)
	}
	return channel, capture
}

func telegramCommandMessage(command, chatType string, chatID, userID int64) *tgbotapi.Message {
	return &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID}, Chat: &tgbotapi.Chat{ID: chatID, Type: chatType},
		Text: command, Entities: []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: len(command)}},
	}
}

func waitForTelegramMessages(t *testing.T, capture *telegramAPICapture, count int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		capture.mu.Lock()
		messages := append([]string(nil), capture.messages...)
		capture.mu.Unlock()
		if len(messages) >= count {
			return messages
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Telegram messages did not reach %d", count)
	return nil
}

func TestBuildTelegramTextMessageKeepsRawSMSContent(t *testing.T) {
	t.Parallel()

	text := "📩 收到新短信\n内容: <#> 验证码 #123456 <b>TAG</b>"
	msg := buildTelegramTextMessage(12345, text)

	wantText := "📩 收到新短信\n内容: &lt;#&gt; 验证码 #123456 &lt;b&gt;TAG&lt;/b&gt;"
	if msg.Text != wantText {
		t.Fatalf("Text = %q, want %q", msg.Text, wantText)
	}
	if msg.ParseMode != "HTML" {
		t.Fatalf("ParseMode = %q, want HTML", msg.ParseMode)
	}
	if msg.ChatID != 12345 {
		t.Fatalf("ChatID = %d, want 12345", msg.ChatID)
	}
}

func TestUnknownCommandReplyUsesPlainTemplate(t *testing.T) {
	t.Parallel()

	got := unknownCommandReply("badcmd")
	want := "未知命令 / badcmd\n提示    请检查命令名或使用 /list、/status、/send 等已注册命令"
	if got != want {
		t.Fatalf("unknownCommandReply() = %q, want %q", got, want)
	}
}

func TestTelegramAdminPrivateStartBindsTargetAndUsesHelp(t *testing.T) {
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "notification-state.json"))
	channel, capture := newTelegramTestChannel(t, store, 42, 0)
	channel.RegisterCommand("help", func(CommandContext, []string) string {
		return "帮助内容\n设备    modem-a"
	})

	channel.handleMessage(telegramCommandMessage("/start", "private", 42, 42))
	messages := waitForTelegramMessages(t, capture, 1)
	if !strings.Contains(messages[0], "modem-a") {
		t.Fatalf("reply = %q", messages[0])
	}
	state, err := store.Load()
	if err != nil || state.Telegram.DefaultTarget != 42 {
		t.Fatalf("state = %+v, error = %v", state, err)
	}

	restarted, restartedCapture := newTelegramTestChannel(t, store, 42, 0)
	if err := restarted.Send("重启后通知"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if messages := waitForTelegramMessages(t, restartedCapture, 1); messages[0] != "重启后通知" {
		t.Fatalf("restarted reply = %q", messages[0])
	}
}

func TestTelegramRejectsUnauthorizedAndUnboundGroupCommands(t *testing.T) {
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "notification-state.json"))
	channel, capture := newTelegramTestChannel(t, store, 42, 0)
	var calls atomic.Int32
	channel.RegisterCommand("help", func(CommandContext, []string) string {
		calls.Add(1)
		return "should not send"
	})

	channel.handleMessage(telegramCommandMessage("/help", "private", 7, 7))
	channel.handleMessage(telegramCommandMessage("/help", "supergroup", -100, 42))
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("handler calls = %d, want 0", calls.Load())
	}
	capture.mu.Lock()
	messageCount := len(capture.messages)
	capture.mu.Unlock()
	if messageCount != 0 {
		t.Fatalf("messages = %d, want 0", messageCount)
	}
	state, err := store.Load()
	if err != nil || state.Telegram.DefaultTarget != 0 {
		t.Fatalf("state = %+v, error = %v", state, err)
	}
}

func TestTelegramExplicitChatIDRemainsCompatible(t *testing.T) {
	channel, capture := newTelegramTestChannel(t, nil, 0, -100)
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "legacy" })
	channel.handleMessage(telegramCommandMessage("/help", "group", -100, 7))
	if messages := waitForTelegramMessages(t, capture, 1); messages[0] != "legacy" {
		t.Fatalf("reply = %q", messages[0])
	}
}

func TestTelegramCommandMenuContainsSharedCommands(t *testing.T) {
	channel, capture := newTelegramTestChannel(t, nil, 42, 0)
	channel.RegisterCommand("vocall", func(CommandContext, []string) string { return "" })
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "" })
	if err := channel.registerCommandMenu(); err != nil {
		t.Fatalf("registerCommandMenu() error = %v", err)
	}
	capture.mu.Lock()
	commands := append([]tgbotapi.BotCommand(nil), capture.commands...)
	capture.mu.Unlock()
	got := make([]string, 0, len(commands))
	for _, command := range commands {
		got = append(got, command.Command)
	}
	if strings.Join(got, ",") != "start,help,vocall" {
		t.Fatalf("commands = %v", got)
	}
}
