package notify

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramChannel 实现 Channel 接口的 Telegram 通知渠道
type TelegramChannel struct {
	api              *tgbotapi.BotAPI
	configuredChatID int64
	adminID          int64
	stateStore       RuntimeStateStore
	stateMu          sync.RWMutex
	defaultTarget    int64
	handlers         map[string]CommandHandler
}

type TelegramChannelOptions struct {
	Config     config.TelegramConfig
	StateStore RuntimeStateStore
}

var ErrNoTelegramTarget = errors.New("Telegram 尚未绑定通知目标")

// NewTelegramChannel 根据配置创建 Telegram 渠道
func NewTelegramChannel(cfg config.TelegramConfig) (*TelegramChannel, error) {
	return NewTelegramChannelWithOptions(TelegramChannelOptions{Config: cfg})
}

func NewTelegramChannelWithOptions(options TelegramChannelOptions) (*TelegramChannel, error) {
	cfg := options.Config
	if !cfg.Enabled {
		return nil, nil
	}
	bot, err := newTelegramBotAPI(cfg)
	if err != nil {
		return nil, err
	}
	logger.Info("已授权账户 (TG)", "username", bot.Self.UserName)

	channel := &TelegramChannel{
		api: bot, configuredChatID: cfg.ChatID, adminID: cfg.AdminID,
		stateStore: options.StateStore, defaultTarget: cfg.ChatID,
		handlers: make(map[string]CommandHandler),
	}
	if cfg.ChatID == 0 && options.StateStore != nil {
		state, loadErr := options.StateStore.Load()
		if loadErr != nil {
			return nil, fmt.Errorf("读取 Telegram 绑定状态失败: %w", loadErr)
		}
		channel.defaultTarget = state.Telegram.DefaultTarget
	}
	return channel, nil
}

func newTelegramBotAPI(cfg config.TelegramConfig) (*tgbotapi.BotAPI, error) {
	endpoint := tgbotapi.APIEndpoint
	if cfg.BaseURL != "" {
		endpoint = cfg.BaseURL
		if !strings.Contains(endpoint, "bot%s/%s") {
			endpoint = strings.TrimSuffix(endpoint, "/") + "/bot%s/%s"
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			logger.Error("解析 Telegram 代理地址失败", "err", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			logger.Info("Telegram Bot 使用代理")
		}
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second,
	}

	bot, err := tgbotapi.NewBotAPIWithClient(cfg.BotToken, endpoint, httpClient)
	if err != nil {
		msg := err.Error()
		if cfg.BotToken != "" {
			msg = strings.ReplaceAll(msg, cfg.BotToken, "<redacted>")
		}
		return nil, fmt.Errorf("创建 telegram bot 失败: %s", msg)
	}
	return bot, nil
}

func (t *TelegramChannel) Name() string { return "telegram" }

func buildTelegramTextMessage(chatID int64, text string) tgbotapi.MessageConfig {
	// 过滤非法的 UTF-8 字符，防止 Telegram API 报错
	cleanText := strings.Map(func(r rune) rune {
		if r == utf8.RuneError {
			return -1 // 丢弃非法字符
		}
		return r
	}, text)

	escaped := html.EscapeString(cleanText)
	msg := tgbotapi.NewMessage(chatID, escaped)
	// 使用 HTML 模式，但先转义短信原文，避免 "<#>" 等内容被当作标签解析。
	msg.ParseMode = "HTML"
	return msg
}

func (t *TelegramChannel) Send(text string) error {
	if t == nil || t.api == nil {
		return nil
	}
	t.stateMu.RLock()
	target := t.defaultTarget
	t.stateMu.RUnlock()
	if target == 0 {
		return ErrNoTelegramTarget
	}
	return t.sendTo(target, text)
}

func (t *TelegramChannel) sendTo(chatID int64, text string) error {
	msg := buildTelegramTextMessage(chatID, text)
	_, err := t.api.Send(msg)
	if err != nil {
		err = t.redactAPIError(err)
		logger.Error("发送 telegram 消息失败", "err", err)
		return err
	}
	return nil
}

func (t *TelegramChannel) RegisterCommand(cmd string, handler CommandHandler) {
	if t == nil {
		return
	}
	t.handlers[cmd] = handler
	logger.Info("注册 Telegram 命令", "command", "/"+cmd)
}

func (t *TelegramChannel) authorized(message *tgbotapi.Message) bool {
	if message == nil || message.Chat == nil {
		return false
	}
	if t.adminID != 0 {
		if message.From == nil || message.From.ID != t.adminID {
			return false
		}
		return message.Chat.IsPrivate() ||
			(t.configuredChatID != 0 && message.Chat.ID == t.configuredChatID)
	}
	return t.configuredChatID != 0 && message.Chat.ID == t.configuredChatID
}

type telegramAPIError struct {
	err   error
	token string
}

func (e *telegramAPIError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return strings.ReplaceAll(e.err.Error(), e.token, "<redacted>")
}

func (e *telegramAPIError) Unwrap() error { return e.err }

func (t *TelegramChannel) redactAPIError(err error) error {
	if err == nil || t == nil || t.api == nil || t.api.Token == "" {
		return err
	}
	return &telegramAPIError{err: err, token: t.api.Token}
}

func (t *TelegramChannel) bindPrivateTarget(message *tgbotapi.Message) error {
	if message.Chat == nil || !message.Chat.IsPrivate() || t.configuredChatID != 0 {
		return nil
	}
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	if t.defaultTarget != 0 {
		return nil
	}
	if t.stateStore != nil {
		if err := t.stateStore.Update(func(state *RuntimeState) error {
			if state.Telegram.DefaultTarget == 0 {
				state.Telegram.DefaultTarget = message.Chat.ID
			}
			t.defaultTarget = state.Telegram.DefaultTarget
			return nil
		}); err != nil {
			return fmt.Errorf("保存 Telegram 默认通知目标失败: %w", err)
		}
		return nil
	}
	t.defaultTarget = message.Chat.ID
	return nil
}

func (t *TelegramChannel) handleMessage(message *tgbotapi.Message) {
	if message == nil || !message.IsCommand() || !t.authorized(message) {
		return
	}
	ctx := &tgCommandContext{channel: t, target: message.Chat.ID}
	if err := t.bindPrivateTarget(message); err != nil {
		ctx.Reply("Telegram 绑定失败\n原因    " + err.Error())
		return
	}
	command := message.Command()
	if command == "start" {
		command = "help"
	}
	args := strings.Fields(message.CommandArguments())
	logger.Info("收到 Telegram 命令", "command", command)
	handler, ok := t.handlers[command]
	if !ok {
		ctx.Reply(unknownCommandReply(command))
		return
	}
	if response := handler(ctx, args); response != "" {
		ctx.Reply(response)
	}
}

// Start 启动 Telegram long-polling 命令监听（阻塞式）
func (t *TelegramChannel) Start() error {
	if t == nil || t.api == nil {
		return nil
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	if err := t.registerCommandMenu(); err != nil {
		logger.Warn("注册 Telegram 命令菜单失败", "err", err)
	}

	updates := t.api.GetUpdatesChan(u)
	logger.Info("Telegram Bot 命令监听已启动")

	for update := range updates {
		t.handleMessage(update.Message)
	}
	return nil
}

func (t *TelegramChannel) Close() error {
	if t == nil || t.api == nil {
		return nil
	}
	t.api.StopReceivingUpdates()
	return nil
}
