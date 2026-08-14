package notify

// CommandContext 传递命令上下文，使得异步操作可以精准回复本会话
type CommandContext interface {
	Reply(text string)
}

// CommandAttachment carries non-text output to command surfaces that support it.
type CommandAttachment struct {
	Type        string `json:"type"`
	Recording   string `json:"recording"`
	ContentType string `json:"content_type"`
	Path        string `json:"-"`
	Codec       string `json:"codec,omitempty"`
	Size        int64  `json:"size,omitempty"`
	SourcePath  string `json:"-"`
	SourceCodec string `json:"-"`
}

type commandAttachmentContext interface {
	ReplyWithAttachments(text string, attachments []CommandAttachment)
}

type commandProgressContext interface {
	Progress(text string)
}

func replyWithAttachments(ctx CommandContext, text string, attachments []CommandAttachment) {
	if rich, ok := ctx.(commandAttachmentContext); ok {
		rich.ReplyWithAttachments(text, attachments)
		return
	}
	ctx.Reply(text)
}

func reportProgress(ctx CommandContext, text string) {
	if rich, ok := ctx.(commandProgressContext); ok {
		rich.Progress(text)
		return
	}
	ctx.Reply(text)
}

// CommandHandler 命令处理器，接收上下文及参数切片，返回回复文本
type CommandHandler func(cmdCtx CommandContext, args []string) string

// ChannelCommandRequest identifies a command received from an interactive bot.
// User and conversation identifiers intentionally stay inside the channel adapter.
type ChannelCommandRequest struct {
	Channel   string
	Name      string
	Arguments []string
}

// ChannelCommandExecutor lets an external command surface execute through a
// shared timeline while keeping delivery in the originating bot context.
type ChannelCommandExecutor interface {
	ExecuteChannelCommand(CommandContext, ChannelCommandRequest, CommandHandler) string
}

// Channel 统一通知渠道接口
// 所有通知渠道（Telegram、飞书、未来的 Discord/Slack 等）均实现此接口
type Channel interface {
	// Name 返回渠道名称（如 "telegram"、"feishu"）
	Name() string

	// Send 发送文本消息
	Send(text string) error

	// RegisterCommand 注册命令处理器
	// cmd: 命令名（如 "send"），handler: 处理函数
	RegisterCommand(cmd string, handler CommandHandler)

	// Start 启动命令监听（阻塞式，应在 goroutine 中调用）
	Start() error

	// Close 释放资源，停止监听
	Close() error
}

// RegistrationHelpSender sends onboarding help to one newly registered target.
type RegistrationHelpSender interface {
	SendRegistrationHelp(target, text string) error
}
