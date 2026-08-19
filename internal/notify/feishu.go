//go:build !(linux && arm)

package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// feishuLogger 将飞书 SDK 日志转发到 hideck 的 logger
type feishuLogger struct{}

func (l *feishuLogger) Debug(ctx context.Context, args ...interface{}) {
	logger.Debug("[飞书] " + fmt.Sprint(args...))
}
func (l *feishuLogger) Info(ctx context.Context, args ...interface{}) {
	logger.Info("[飞书] " + fmt.Sprint(args...))
}
func (l *feishuLogger) Warn(ctx context.Context, args ...interface{}) {
	logger.Warn("[飞书] " + fmt.Sprint(args...))
}
func (l *feishuLogger) Error(ctx context.Context, args ...interface{}) {
	logger.Error("[飞书] " + fmt.Sprint(args...))
}

// FeishuChannel 实现 Channel 接口的飞书通知渠道
// 使用飞书开放平台 Bot + WebSocket 长连接
type FeishuChannel struct {
	client       *lark.Client
	wsClient     *larkws.Client
	chatIDs      []string
	allowedUsers []string
	handlers     map[string]CommandHandler
	cfg          config.FeishuConfig
	stateStore   RuntimeStateStore
	stateMu      sync.RWMutex
	startMu      sync.Mutex
	cancel       context.CancelFunc
	replyText    func(msg *larkim.EventMessage, text string)
	media        feishuMediaAPI
}

// NewFeishuChannel 根据配置创建飞书渠道
func NewFeishuChannel(cfg config.FeishuConfig) (*FeishuChannel, error) {
	return NewFeishuChannelWithOptions(FeishuChannelOptions{Config: cfg})
}

func NewFeishuChannelWithOptions(options FeishuChannelOptions) (*FeishuChannel, error) {
	cfg := options.Config
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf("飞书配置缺少 app_id 或 app_secret")
	}
	binding := TrustedFeishuBindingFrom(cfg, FeishuRuntimeState{})
	if options.StateStore != nil {
		state, err := options.StateStore.Load()
		if err != nil {
			return nil, fmt.Errorf("读取飞书绑定状态失败: %w", err)
		}
		binding = TrustedFeishuBindingFrom(cfg, state.Feishu)
	}
	chatIDs, allowedUsers := binding.ChatIDs, binding.AllowedUsers
	if len(chatIDs) == 0 {
		logger.Warn("飞书渠道已启用但还没有 Chat ID，给机器人发一条消息后会自动绑定")
	}

	sdkLogger := &feishuLogger{}

	// 创建飞书 API 客户端（自动管理 tenant_access_token）
	client := lark.NewClient(cfg.AppID, cfg.AppSecret,
		lark.WithLogLevel(larkcore.LogLevelInfo),
		lark.WithLogger(sdkLogger),
	)

	logger.Info("飞书 Bot 客户端已创建", "app_id", cfg.AppID)

	return &FeishuChannel{
		client:       client,
		chatIDs:      chatIDs,
		allowedUsers: allowedUsers,
		handlers:     make(map[string]CommandHandler),
		cfg:          cfg,
		stateStore:   options.StateStore,
		media:        feishuSDKMedia{client: client},
	}, nil
}

func (f *FeishuChannel) Name() string { return "feishu" }

// Send 通过飞书 API 发送文本消息到指定的所有群聊
func (f *FeishuChannel) Send(text string) error {
	if f == nil || f.client == nil {
		return nil
	}
	chatIDs := f.notificationChatIDs()
	if len(chatIDs) == 0 {
		return fmt.Errorf("飞书还没有绑定 Chat ID，请先给这个机器人发一条消息")
	}
	var lastErr error
	for _, chatID := range chatIDs {
		if err := f.sendToChat(chatID, text); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (f *FeishuChannel) sendToChat(chatID, text string) error {
	if f == nil || f.client == nil {
		return nil
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return fmt.Errorf("飞书会话缺少 Chat ID")
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()
	resp, err := f.client.Im.Message.Create(context.Background(), req)
	if err != nil {
		logger.Error("发送飞书消息失败", "chat_id", chatID, "err", err)
		return err
	}
	if !resp.Success() {
		err = fmt.Errorf("飞书 API 错误 %d: %s", resp.Code, resp.Msg)
		logger.Error("发送飞书消息失败", "chat_id", chatID, "err", err)
		return err
	}
	return nil
}

func (f *FeishuChannel) RegisterCommand(cmd string, handler CommandHandler) {
	if f == nil {
		return
	}
	f.handlers[cmd] = handler
	logger.Info("注册飞书命令", "command", "/"+cmd)
}

// Start 通过 WebSocket 长连接启动命令监听（阻塞式）
func (f *FeishuChannel) Start() error {
	if f == nil || f.client == nil {
		return nil
	}

	// 创建事件分发器
	eventHandler := dispatcher.NewEventDispatcher("", "")

	// 注册消息接收事件
	eventHandler.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		f.handleMessageEvent(event)
		return nil
	})

	// 创建 WS 长连接客户端
	f.wsClient = larkws.NewClient(f.cfg.AppID, f.cfg.AppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
		larkws.WithLogger(&feishuLogger{}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	f.startMu.Lock()
	f.cancel = cancel
	f.startMu.Unlock()
	logger.Info("飞书 Bot WebSocket 长连接启动中...", "app_id", f.cfg.AppID)
	err := f.wsClient.Start(ctx)
	if err != nil {
		logger.Error("飞书 Bot WebSocket 连接失败", "app_id", f.cfg.AppID, "err", err)
	}
	return err
}

// feishuCommandContext 实现了 CommandContext 接口，允许异步回复飞书消息
type feishuCommandContext struct {
	channel  *FeishuChannel
	msg      *larkim.EventMessage
	senderID string
}

func (c *feishuCommandContext) Reply(text string) {
	c.channel.replyToMessage(c.msg, text)
}

func (c *feishuCommandContext) ReplyWithAttachments(text string, attachments []CommandAttachment) {
	if c == nil || c.channel == nil {
		return
	}
	c.channel.deliverCommandResult(c.msg, text, attachments)
}

func (c *feishuCommandContext) Confirm(prompt string) bool {
	// Feishu supports interactive replies. When UserKey is available,
	// confirmation is handled via confirmRegistry. This method is only
	// reached when UserKey is empty (edge case), so skip confirmation.
	return true
}

func (c *feishuCommandContext) UserKey() string {
	if c == nil || strings.TrimSpace(c.senderID) == "" {
		return ""
	}
	return fmt.Sprintf("feishu:%s", strings.TrimSpace(c.senderID))
}

// replyToMessage 回复飞书消息（使用 reply API）
func (f *FeishuChannel) replyToMessage(msg *larkim.EventMessage, text string) {
	if f != nil && f.replyText != nil {
		f.replyText(msg, text)
		return
	}
	if f == nil || f.client == nil {
		return
	}
	if msg == nil || msg.MessageId == nil {
		f.sendReplyFallback(msg, text)
		return
	}

	content, _ := json.Marshal(map[string]string{"text": text})

	req := larkim.NewReplyMessageReqBuilder().
		MessageId(*msg.MessageId).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()

	resp, err := f.client.Im.Message.Reply(context.Background(), req)
	if err != nil {
		logger.Warn("飞书回复消息失败，尝试直接发送", "err", err)
		f.sendReplyFallback(msg, text)
		return
	}
	if !resp.Success() {
		logger.Warn("飞书回复消息失败，尝试直接发送", "code", resp.Code, "msg", resp.Msg)
		f.sendReplyFallback(msg, text)
	}
}

func (f *FeishuChannel) sendReplyFallback(msg *larkim.EventMessage, text string) {
	chatID := feishuMessageChatID(msg)
	if err := f.sendToChat(chatID, text); err != nil {
		logger.Warn("飞书定向发送回复失败", "chat_id", chatID, "err", err)
	}
}

func (f *FeishuChannel) Close() error {
	if f == nil {
		return nil
	}
	f.startMu.Lock()
	cancel := f.cancel
	f.cancel = nil
	f.startMu.Unlock()
	if cancel != nil {
		cancel()
	}
	logger.Info("飞书 Bot 已关闭")
	return nil
}
