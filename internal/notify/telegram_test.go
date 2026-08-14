package notify

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

const telegramTestToken = "123456:test-token"

type telegramAPICapture struct {
	mu          sync.Mutex
	methods     []string
	messages    []string
	commands    []tgbotapi.BotCommand
	audioFiles  [][]byte
	sequence    []string
	rejectAudio bool
	rejectText  bool
}

type telegramToggleStateStore struct {
	mu         sync.Mutex
	state      RuntimeState
	updateFail bool
}

func (s *telegramToggleStateStore) Load() (RuntimeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRuntimeState(s.state), nil
}

func (s *telegramToggleStateStore) Save(state RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = cloneRuntimeState(state)
	return nil
}

func (s *telegramToggleStateStore) Update(update func(*RuntimeState) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneRuntimeState(s.state)
	if err := update(&next); err != nil {
		return err
	}
	if s.updateFail {
		return errors.New("simulated state write failure")
	}
	s.state = next
	return nil
}

func (s *telegramToggleStateStore) allowUpdates() {
	s.mu.Lock()
	s.updateFail = false
	s.mu.Unlock()
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
		reject := c.rejectText
		c.messages = append(c.messages, text)
		c.sequence = append(c.sequence, "text")
		c.mu.Unlock()
		if reject {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "description": "rejected " + telegramTestToken + " for chat 42",
			})
			return
		}
		writeTelegramResult(w, map[string]any{
			"message_id": 1, "date": 1, "chat": map[string]any{"id": 42, "type": "private"}, "text": text,
		})
	case "sendAudio":
		file, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, `{"ok":false,"description":"missing audio"}`, http.StatusBadRequest)
			return
		}
		data, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil {
			http.Error(w, `{"ok":false,"description":"read audio"}`, http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		c.audioFiles = append(c.audioFiles, data)
		c.sequence = append(c.sequence, "audio")
		reject := c.rejectAudio
		c.mu.Unlock()
		if reject {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "description": "rejected " + telegramTestToken + " for chat 42",
			})
			return
		}
		writeTelegramResult(w, map[string]any{
			"message_id": 2, "date": 1, "chat": map[string]any{"id": 42, "type": "private"},
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

func waitForTelegramDelivery(t *testing.T, capture *telegramAPICapture, audio, messages int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		capture.mu.Lock()
		ready := len(capture.audioFiles) >= audio && len(capture.messages) >= messages
		capture.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Telegram delivery did not reach audio=%d messages=%d", audio, messages)
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

func TestTelegramBindingPublishesTargetOnlyAfterStateUpdateSucceeds(t *testing.T) {
	store := &telegramToggleStateStore{state: newRuntimeState(), updateFail: true}
	channel, capture := newTelegramTestChannel(t, store, 42, 0)
	channel.RegisterCommand("help", func(CommandContext, []string) string { return "help" })
	message := telegramCommandMessage("/start", "private", 42, 42)

	channel.handleMessage(message)
	if replies := waitForTelegramMessages(t, capture, 1); !strings.Contains(replies[0], "绑定失败") {
		t.Fatalf("reply = %q", replies[0])
	}
	if err := channel.Send("must not send"); !errors.Is(err, ErrNoTelegramTarget) {
		t.Fatalf("Send() error = %v, want ErrNoTelegramTarget", err)
	}

	store.allowUpdates()
	channel.handleMessage(message)
	replies := waitForTelegramMessages(t, capture, 2)
	if replies[1] != "help" {
		t.Fatalf("retry reply = %q", replies[1])
	}
	state, err := store.Load()
	if err != nil || state.Telegram.DefaultTarget != 42 {
		t.Fatalf("state = %+v, error = %v", state, err)
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

func TestTelegramVoiceCallCompletionSendsValidMP3BeforeText(t *testing.T) {
	channel, capture := newTelegramTestChannel(t, nil, 42, 0)
	path := filepath.Join(t.TempDir(), "call.mp3")
	mp3 := validSilentMP3()
	if contentType := http.DetectContentType(mp3); contentType != "audio/mpeg" {
		t.Fatalf("fixture content type = %q", contentType)
	}
	if err := os.WriteFile(path, mp3, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &tgCommandContext{channel: channel, target: 42}
	replyVoiceCallCompletion(ctx, "呼叫完成", &voicehost.SimulateCallResult{
		Success: true, AudioPath: path, AudioCodec: "MP3",
	})
	ctx.release()
	waitForTelegramDelivery(t, capture, 1, 1)

	capture.mu.Lock()
	sequence := append([]string(nil), capture.sequence...)
	audio := append([]byte(nil), capture.audioFiles[0]...)
	messages := append([]string(nil), capture.messages...)
	capture.mu.Unlock()
	if strings.Join(sequence, ",") != "audio,text" || !bytes.Equal(audio, mp3) {
		t.Fatalf("sequence = %v, audio bytes = %d", sequence, len(audio))
	}
	if messages[0] != "呼叫完成\n录音    call.mp3" {
		t.Fatalf("message = %q", messages[0])
	}
}

func validSilentMP3() []byte {
	const (
		frameSize  = 417
		frameCount = 4
	)
	id3Header := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
	data := make([]byte, len(id3Header)+frameSize*frameCount)
	copy(data, id3Header)
	for offset := len(id3Header); offset < len(data); offset += frameSize {
		// MPEG-1 Layer III, 128 kbps, 44.1 kHz, joint stereo, no CRC.
		copy(data[offset:], []byte{0xff, 0xfb, 0x90, 0x64})
	}
	return data
}

func TestTelegramAudioRejectionSendsOnlyFailure(t *testing.T) {
	channel, capture := newTelegramTestChannel(t, nil, 42, 0)
	capture.mu.Lock()
	capture.rejectAudio = true
	capture.mu.Unlock()
	path := filepath.Join(t.TempDir(), "call.mp3")
	if err := os.WriteFile(path, []byte("mp3"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &tgCommandContext{channel: channel, target: 42}
	ctx.ReplyWithAttachments("呼叫完成", []CommandAttachment{{Path: path, Codec: "MP3"}})
	ctx.release()
	waitForTelegramDelivery(t, capture, 1, 1)

	capture.mu.Lock()
	messages := append([]string(nil), capture.messages...)
	capture.mu.Unlock()
	if len(messages) != 1 || !strings.Contains(messages[0], "录音发送失败") ||
		strings.Contains(messages[0], "呼叫完成") || strings.Contains(messages[0], telegramTestToken) ||
		strings.Contains(messages[0], "42") || strings.Contains(messages[0], path) {
		t.Fatalf("messages = %v", messages)
	}
}

func TestTelegramTextErrorRedactsTokenAndTargetAndPreservesCause(t *testing.T) {
	channel, capture := newTelegramTestChannel(t, nil, 42, 0)
	capture.mu.Lock()
	capture.rejectText = true
	capture.mu.Unlock()
	err := channel.sendTo(42, "test")
	if err == nil || strings.Contains(err.Error(), telegramTestToken) || strings.Contains(err.Error(), "42") {
		t.Fatalf("sendTo() error = %v", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("sendTo() error does not preserve cause: %v", err)
	}
}

func TestTelegramAudioRejectsMissingAndNonMP3Files(t *testing.T) {
	channel, _ := newTelegramTestChannel(t, nil, 42, 0)
	missingPath := "/missing/private/call.mp3"
	if err := channel.sendAudio(42, CommandAttachment{Path: missingPath, Codec: "MP3"}); err == nil || strings.Contains(err.Error(), missingPath) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sendAudio() missing-file error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "call.wav")
	if err := os.WriteFile(path, []byte("wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := channel.sendAudio(42, CommandAttachment{Path: path, Codec: "WAV"}); err == nil {
		t.Fatal("sendAudio() accepted non-MP3 file")
	}
}
