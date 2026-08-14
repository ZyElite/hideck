package notify

import (
	"sort"

	"github.com/yibaiba/hideck/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type tgCommandContext struct {
	channel *TelegramChannel
	target  int64
}

func (c *tgCommandContext) Reply(text string) {
	if c == nil || c.channel == nil {
		return
	}
	go func() {
		if err := c.channel.sendTo(c.target, text); err != nil {
			logger.Warn("回复 Telegram 命令失败", "err", err)
		}
	}()
}

var telegramCommandDescriptions = map[string]string{
	"help": "查看命令与设备 ID", "list": "查看设备列表", "status": "查看设备状态",
	"send": "发送短信", "sms": "查看短信", "esim": "管理 eSIM", "switch": "切换 eSIM",
	"rotate": "切换公网 IP", "vocall": "发起 VoWiFi 呼叫", "balance": "查询余额",
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
