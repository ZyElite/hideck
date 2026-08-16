package notify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type tgCommandContext struct {
	channel  *TelegramChannel
	target   int64
	stateMu  sync.Mutex
	sendMu   sync.Mutex
	released bool
	pending  []telegramCommandReply
}

type telegramCommandReply struct {
	text        string
	attachments []CommandAttachment
}

func (c *tgCommandContext) Reply(text string) {
	if c == nil || c.channel == nil {
		return
	}

	c.enqueueOrSend(telegramCommandReply{text: text})
}

func (c *tgCommandContext) Confirm(prompt string) bool {
	// Send the prompt and rely on the Manager's confirmRegistry to wait
	// for /y or /n. The registry blocks until the user replies or timeout.
	return true // actual blocking is handled via UserKey in handleCmdCellCall
}

func (c *tgCommandContext) UserKey() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("tg:%d", c.target)
}

func (c *tgCommandContext) ReplyWithAttachments(text string, attachments []CommandAttachment) {
	if c == nil || c.channel == nil {
		return
	}
	c.enqueueOrSend(telegramCommandReply{
		text: text, attachments: append([]CommandAttachment(nil), attachments...),
	})
}

func (c *tgCommandContext) enqueueOrSend(reply telegramCommandReply) {
	c.stateMu.Lock()
	if !c.released {
		c.pending = append(c.pending, reply)
		c.stateMu.Unlock()
		return
	}
	c.stateMu.Unlock()
	go c.respondAndReport(reply)
}

func (c *tgCommandContext) release() {
	c.stateMu.Lock()
	if c.released {
		c.stateMu.Unlock()
		return
	}
	c.released = true
	pending := append([]telegramCommandReply(nil), c.pending...)
	c.pending = nil
	c.stateMu.Unlock()
	go func() {
		for _, reply := range pending {
			c.respondAndReport(reply)
		}
	}()
}

func (c *tgCommandContext) respond(reply telegramCommandReply) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	for _, attachment := range reply.attachments {
		if err := c.channel.sendRecording(c.target, attachment); err != nil {
			return fmt.Errorf("上传或发送 Telegram 录音附件失败: %w", err)
		}
	}
	if strings.TrimSpace(reply.text) == "" {
		return nil
	}
	return c.channel.sendTo(c.target, reply.text)
}

func (c *tgCommandContext) respondAndReport(reply telegramCommandReply) {
	if err := c.respond(reply); err != nil {
		logger.Warn("回复 Telegram 命令录音失败", "err", err)
		failure := "录音发送失败\n原因    " + err.Error()
		if sendErr := c.channel.sendTo(c.target, failure); sendErr != nil {
			logger.Warn("发送 Telegram 录音失败说明失败", "err", sendErr)
		}
	}
}

func (t *TelegramChannel) sendRecording(chatID int64, attachment CommandAttachment) error {
	path, err := validateTelegramRecordingAttachment(attachment)
	if err != nil {
		return err
	}
	switch t.recordingMode {
	case config.TelegramRecordingModeVoice:
		return t.sendVoice(chatID, path)
	case config.TelegramRecordingModeAudio:
		return t.sendAudio(chatID, path, attachment)
	default:
		return fmt.Errorf("未知的 Telegram 录音发送样式: %q", t.recordingMode)
	}
}

func validateTelegramRecordingAttachment(attachment CommandAttachment) (string, error) {
	path := strings.TrimSpace(attachment.Path)
	if path == "" {
		return "", errors.New("录音路径为空")
	}
	if !strings.EqualFold(strings.TrimSpace(attachment.Codec), "MP3") ||
		!strings.EqualFold(filepath.Ext(path), ".mp3") {
		return "", errors.New("Telegram 录音附件必须是 MP3")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", redactTelegramError(fmt.Errorf("读取录音文件失败: %w", err), path)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("录音路径不是普通文件")
	}
	return path, nil
}

func (t *TelegramChannel) sendVoice(chatID int64, path string) error {
	voice := tgbotapi.NewVoice(chatID, tgbotapi.FilePath(path))
	_, err := t.api.Send(voice)
	return t.redactAPIError(err, fmt.Sprint(chatID), path)
}

func (t *TelegramChannel) sendAudio(chatID int64, path string, attachment CommandAttachment) error {
	audio := tgbotapi.NewAudio(chatID, tgbotapi.FilePath(path))
	audio.Title = firstNonEmpty(strings.TrimSpace(attachment.Recording), filepath.Base(path))
	_, err := t.api.Send(audio)
	return t.redactAPIError(err, fmt.Sprint(chatID), path)
}

var telegramCommandDescriptions = map[string]string{
	"help": "查看命令与设备 ID", "list": "查看设备列表", "status": "查看设备状态",
	"send": "发送短信", "sms": "查看短信", "esim": "管理 eSIM", "switch": "切换 eSIM",
	"rotate": "切换公网 IP", "vocall": "发起 VoWiFi 呼叫", "cellcall": "发起蜂窝通话", "balance": "查询余额",
}

func (t *TelegramChannel) registerCommandMenu() error {
	commands := make([]string, 0, len(t.handlers))
	for name := range t.handlers {
		commands = append(commands, name)
	}
	sort.Strings(commands)
	menu := make([]tgbotapi.BotCommand, 0, len(commands)+1)
	menu = append(menu, tgbotapi.BotCommand{Command: "start", Description: "开始使用并查看帮助"})
	for _, name := range commands {
		description := telegramCommandDescriptions[name]
		if description == "" {
			description = "执行 " + name + " 命令"
		}
		menu = append(menu, tgbotapi.BotCommand{Command: name, Description: description})
	}
	_, err := t.api.Request(tgbotapi.NewSetMyCommands(menu...))
	return t.redactAPIError(err)
}
