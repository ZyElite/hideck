package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	qqbot "github.com/iniwex5/qqbot"
)

func TestQQChannelSendBroadcastsToAllowedRecipients(t *testing.T) {
	t.Parallel()

	app := &fakeQQApp{}
	channel := &QQChannel{
		app: app,
		allowedRecipients: map[string]qqbot.Recipient{
			"direct:user-1": {Kind: qqbot.DirectRecipient, ID: "user-1"},
			"group:group-1": {Kind: qqbot.GroupRecipient, ID: "group-1"},
		},
	}

	if err := channel.Send("hello"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(app.sent) != 2 {
		t.Fatalf("sent count = %d, want 2", len(app.sent))
	}
}

func TestQQChannelRegisterCommandWithAllowedRecipient(t *testing.T) {
	t.Parallel()

	app := &fakeQQApp{}
	channel := &QQChannel{
		app: app,
		allowedRecipients: map[string]qqbot.Recipient{
			"direct:user-1": {Kind: qqbot.DirectRecipient, ID: "user-1"},
		},
	}

	channel.RegisterCommand("status", func(cmdCtx CommandContext, args []string) string {
		cmdCtx.Reply("async")
		return "sync"
	})

	handler, ok := app.commands["status"]
	if !ok {
		t.Fatalf("expected command handler registered")
	}

	// 白名单内的会话应该能正常回复
	conv := &fakeConversation{
		incoming: qqbot.Incoming{
			ID: "msg-1",
			To: qqbot.Recipient{Kind: qqbot.DirectRecipient, ID: "user-1"},
		},
	}

	if err := handler(context.Background(), conv, qqbot.ParsedCommand{Name: "status"}); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if len(conv.replies) == 0 {
		t.Fatalf("expected reply sent for allowed recipient")
	}
	if len(conv.replies) != 2 || conv.replies[0] != "sync" || conv.replies[1] != "async" {
		t.Fatalf("reply order = %v, want [sync async]", conv.replies)
	}
}

func TestQQChannelRegisterCommandBlocksUnallowed(t *testing.T) {
	t.Parallel()

	app := &fakeQQApp{}
	channel := &QQChannel{
		app: app,
		allowedRecipients: map[string]qqbot.Recipient{
			"direct:user-1": {Kind: qqbot.DirectRecipient, ID: "user-1"},
		},
	}

	channel.RegisterCommand("status", func(cmdCtx CommandContext, args []string) string {
		return "should not reach"
	})

	handler := app.commands["status"]

	// 白名单外的会话不应该收到回复
	conv := &fakeConversation{
		incoming: qqbot.Incoming{
			ID: "msg-2",
			To: qqbot.Recipient{Kind: qqbot.DirectRecipient, ID: "user-unknown"},
		},
	}

	if err := handler(context.Background(), conv, qqbot.ParsedCommand{Name: "status"}); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if len(conv.replies) != 0 {
		t.Fatalf("expected no reply for unallowed recipient, got %d", len(conv.replies))
	}
}

func TestQQChannelCommandSendsVoiceAttachmentBeforeCompletionText(t *testing.T) {
	t.Parallel()
	app := &fakeQQApp{}
	channel := &QQChannel{
		app: app,
		allowedRecipients: map[string]qqbot.Recipient{
			"direct:user-1": {Kind: qqbot.DirectRecipient, ID: "user-1"},
		},
	}
	channel.RegisterCommand("vocall", func(commandContext CommandContext, _ []string) string {
		rich := commandContext.(interface {
			ReplyWithAttachments(string, []CommandAttachment)
		})
		rich.ReplyWithAttachments("呼叫完成", []CommandAttachment{{
			Type: "audio", Recording: "call.mp3", Path: "/recordings/call.mp3", Codec: "MP3",
		}})
		return ""
	})
	conversation := &fakeConversation{incoming: qqbot.Incoming{
		ID: "msg-voice", To: qqbot.Recipient{Kind: qqbot.DirectRecipient, ID: "user-1"},
	}}
	if err := app.commands["vocall"](context.Background(), conversation, qqbot.ParsedCommand{}); err != nil {
		t.Fatal(err)
	}
	if len(conversation.deliveries) != 1 {
		t.Fatalf("deliveries = %+v", conversation.deliveries)
	}
	delivery := conversation.deliveries[0]
	if delivery.Kind != qqbot.Voice || delivery.MediaPath != "/recordings/call.mp3" || delivery.FileName != "call.mp3" {
		t.Fatalf("voice delivery = %+v", delivery)
	}
	if len(conversation.replies) != 1 || conversation.replies[0] != "呼叫完成" {
		t.Fatalf("replies = %v", conversation.replies)
	}
}

func TestQQCommandVoiceFailureDoesNotSendCompletionText(t *testing.T) {
	t.Parallel()
	conversation := &fakeConversation{respondErr: errors.New("upload rejected")}
	commandContext := &qqCommandContext{conversation: conversation, released: true}
	commandContext.respondAndReport(qqCommandReply{
		text: "呼叫完成",
		attachments: []CommandAttachment{{
			Type: "audio", Recording: "call.mp3", Path: "/recordings/call.mp3", Codec: "MP3",
		}},
	})
	if len(conversation.replies) != 1 || !strings.Contains(conversation.replies[0], "录音发送失败") ||
		strings.Contains(conversation.replies[0], "呼叫完成") {
		t.Fatalf("replies = %v", conversation.replies)
	}
}

func TestQQChannelStartAndCloseDelegateToApp(t *testing.T) {
	t.Parallel()

	app := &fakeQQApp{}
	channel := &QQChannel{app: app, allowedRecipients: make(map[string]qqbot.Recipient)}

	if err := channel.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !app.runCalled {
		t.Fatalf("expected Run() called")
	}
	if err := channel.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !app.closeCalled {
		t.Fatalf("expected Close() called")
	}
}

func TestQQChannelSendNoRecipientsNoError(t *testing.T) {
	t.Parallel()

	// 没有白名单时不报错（静默跳过）
	channel := &QQChannel{
		app:               &fakeQQApp{},
		allowedRecipients: make(map[string]qqbot.Recipient),
	}

	if err := channel.Send("hello"); err != nil {
		t.Fatalf("expected no error when no allowed recipients, got %v", err)
	}
}

func TestParseAllowedRecipients(t *testing.T) {
	t.Parallel()

	result := parseAllowedRecipients("G123", "U456")
	if len(result) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(result))
	}
	if r, ok := result["group:G123"]; !ok || r.Kind != qqbot.GroupRecipient || r.ID != "G123" {
		t.Fatalf("unexpected group recipient: %+v", r)
	}
	if r, ok := result["direct:U456"]; !ok || r.Kind != qqbot.DirectRecipient || r.ID != "U456" {
		t.Fatalf("unexpected direct recipient: %+v", r)
	}
}

func TestParseAllowedRecipientsEmpty(t *testing.T) {
	t.Parallel()

	result := parseAllowedRecipients("", "")
	if len(result) != 0 {
		t.Fatalf("expected 0 recipients for empty string, got %d", len(result))
	}
}

type fakeQQApp struct {
	sent        []qqbot.Delivery
	commands    map[string]qqbot.CommandHandler
	text        qqbot.TextHandler
	runCalled   bool
	closeCalled bool
	sendErr     error
}

func (f *fakeQQApp) Send(ctx context.Context, delivery qqbot.Delivery) (qqbot.Receipt, error) {
	if f.sendErr != nil {
		return qqbot.Receipt{}, f.sendErr
	}
	f.sent = append(f.sent, delivery)
	return qqbot.Receipt{ID: "r-1"}, nil
}

func (f *fakeQQApp) Command(name string, handler qqbot.CommandHandler) {
	if f.commands == nil {
		f.commands = make(map[string]qqbot.CommandHandler)
	}
	f.commands[name] = handler
}

func (f *fakeQQApp) OnText(handler qqbot.TextHandler) {
	f.text = handler
}

func (f *fakeQQApp) Run(ctx context.Context) error {
	f.runCalled = true
	return nil
}

func (f *fakeQQApp) Close() error {
	f.closeCalled = true
	return nil
}

type fakeConversation struct {
	incoming   qqbot.Incoming
	replies    []string
	deliveries []qqbot.Delivery
	err        error
	respondErr error
	textErr    error
}

func (f *fakeConversation) Incoming() qqbot.Incoming {
	return f.incoming
}

func (f *fakeConversation) Respond(ctx context.Context, delivery qqbot.Delivery) (qqbot.Receipt, error) {
	if f.respondErr != nil {
		return qqbot.Receipt{}, f.respondErr
	}
	if f.err != nil {
		return qqbot.Receipt{}, f.err
	}
	f.deliveries = append(f.deliveries, delivery)
	if delivery.Body != "" {
		f.replies = append(f.replies, delivery.Body)
	}
	return qqbot.Receipt{ID: "reply"}, nil
}

func (f *fakeConversation) RespondText(ctx context.Context, text string) (qqbot.Receipt, error) {
	if f.textErr != nil {
		return qqbot.Receipt{}, f.textErr
	}
	if f.err != nil {
		return qqbot.Receipt{}, f.err
	}
	f.replies = append(f.replies, text)
	return qqbot.Receipt{ID: "reply"}, nil
}

var _ qqbot.Conversation = (*fakeConversation)(nil)
var _ qqApp = (*fakeQQApp)(nil)
