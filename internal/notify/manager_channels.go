package notify

import (
	"sync"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

type channelActivity struct {
	sends sync.WaitGroup
}

type channelFactory struct {
	name    string
	enabled bool
	build   func() (Channel, error)
}

// initChannels builds a complete replacement before publishing it.
func (m *Manager) initChannels(cfg *config.Config) error {
	channels, err := m.buildChannels(cfg)
	if err != nil {
		return err
	}
	m.registerCommands(channels)
	m.installChannels(channels)
	return nil
}

func (m *Manager) buildChannels(cfg *config.Config) ([]Channel, error) {
	channels := make([]Channel, 0)
	for _, factory := range m.channelFactories(cfg) {
		if !factory.enabled {
			continue
		}
		channel, err := factory.build()
		if err != nil {
			closeChannels(channels)
			logger.Error("初始化通知渠道失败", "channel", factory.name, "err", err)
			return nil, err
		}
		if channel != nil {
			channels = append(channels, channel)
		}
	}
	return channels, nil
}

func (m *Manager) channelFactories(cfg *config.Config) []channelFactory {
	return []channelFactory{
		{name: "telegram", enabled: cfg.Telegram.Enabled, build: func() (Channel, error) {
			return NewTelegramChannelWithOptions(TelegramChannelOptions{
				Config: cfg.Telegram, StateStore: m.stateStore,
			})
		}},
		{name: "feishu", enabled: cfg.Feishu.Enabled, build: func() (Channel, error) { return NewFeishuChannel(cfg.Feishu) }},
		{name: "qq", enabled: cfg.QQ.Enabled, build: func() (Channel, error) { return NewQQChannel(cfg.QQ) }},
		{name: "weixin", enabled: cfg.Weixin.Enabled, build: func() (Channel, error) {
			return NewWeixinChannel(WeixinChannelOptions{Config: cfg.Weixin, StateStore: m.stateStore})
		}},
		{name: "wecom-bot", enabled: cfg.WeComBot.Enabled, build: func() (Channel, error) {
			return NewWeComBotChannel(WeComBotOptions{Config: cfg.WeComBot, StateStore: m.stateStore})
		}},
		{name: "webhook", enabled: cfg.Webhook.Enabled, build: func() (Channel, error) { return NewWebhookChannel(cfg.Webhook) }},
		{name: "bark", enabled: cfg.Bark.Enabled, build: func() (Channel, error) { return NewBarkChannel(cfg.Bark) }},
		{name: "email", enabled: cfg.Email.Enabled, build: func() (Channel, error) { return NewEmailChannel(cfg.Email) }},
		{name: "pushplus", enabled: cfg.Pushplus.Enabled, build: func() (Channel, error) { return NewPushplusChannel(cfg.Pushplus) }},
		{name: "wecom", enabled: cfg.WeCom.Enabled, build: func() (Channel, error) { return NewWeComChannel(cfg.WeCom) }},
	}
}

func (m *Manager) registerCommands(channels []Channel) {
	commands := m.CommandService().Handlers()
	for _, channel := range channels {
		for name, handler := range commands {
			channel.RegisterCommand(name, handler)
		}
	}
}

func (m *Manager) installChannels(channels []Channel) {
	oldChannels, oldActivity := m.swapChannels(channels)
	startChannels(channels)
	retireChannels(oldChannels, oldActivity)
}

func (m *Manager) swapChannels(channels []Channel) ([]Channel, *channelActivity) {
	m.channelsMu.Lock()
	oldChannels, oldActivity := m.channels, m.channelActivity
	m.channels = append([]Channel(nil), channels...)
	m.channelActivity = &channelActivity{}
	m.channelsMu.Unlock()
	return oldChannels, oldActivity
}

func (m *Manager) beginChannelSends() ([]Channel, *channelActivity) {
	m.channelsMu.Lock()
	if m.channelActivity == nil {
		m.channelActivity = &channelActivity{}
	}
	channels := append([]Channel(nil), m.channels...)
	activity := m.channelActivity
	activity.sends.Add(len(channels))
	m.channelsMu.Unlock()
	return channels, activity
}

func (m *Manager) channelCount() int {
	m.channelsMu.Lock()
	defer m.channelsMu.Unlock()
	return len(m.channels)
}

func startChannels(channels []Channel) {
	for _, channel := range channels {
		channel := channel
		go func() {
			if err := channel.Start(); err != nil {
				logger.Error("通知渠道命令监听失败", "channel", channel.Name(), "err", err)
			}
		}()
	}
}

func retireChannels(channels []Channel, activity *channelActivity) {
	if activity != nil {
		activity.sends.Wait()
	}
	closeChannels(channels)
}

func closeChannels(channels []Channel) {
	for _, channel := range channels {
		_ = channel.Close()
	}
}

func (m *Manager) Close() {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	channels, activity := m.swapChannels(nil)
	retireChannels(channels, activity)
}

func (m *Manager) UpdateConfig(cfg *config.Config) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	return m.initChannels(cfg)
}

func (m *Manager) GetChannelNames() []string {
	m.channelsMu.Lock()
	defer m.channelsMu.Unlock()
	names := make([]string, 0, len(m.channels))
	for _, channel := range m.channels {
		names = append(names, channel.Name())
	}
	return names
}
