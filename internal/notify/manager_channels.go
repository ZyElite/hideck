package notify

import (
	"sync"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

type channelActivity struct {
	sends sync.WaitGroup
}

type channelDelivery struct {
	channel  Channel
	activity *channelActivity
}

type channelCommandReceiver interface {
	StopReceivingCommands()
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
		{name: "qq", enabled: cfg.QQ.Enabled, build: func() (Channel, error) { return m.buildQQChannel(cfg.QQ) }},
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

func (m *Manager) buildQQChannel(cfg config.QQConfig) (Channel, error) {
	if m.qqChannelFactory != nil {
		return m.qqChannelFactory(cfg)
	}
	return NewQQChannel(cfg)
}

func (m *Manager) registerCommands(channels []Channel) {
	commands := m.CommandService().Handlers()
	for _, channel := range channels {
		channelName := channel.Name()
		for name, handler := range commands {
			channel.RegisterCommand(name, m.channelCommandHandler(channelName, name, handler))
		}
	}
}

func (m *Manager) installChannels(channels []Channel) {
	oldChannels, oldActivity := m.swapChannels(channels)
	if m.commandReceiversStarted {
		startChannels(channels)
	}
	retireChannels(oldChannels, oldActivity)
}

// StartCommandReceivers starts inbound Bot listeners after their shared command
// executor has been wired. Repeated calls are safe.
func (m *Manager) StartCommandReceivers() {
	if m == nil {
		return
	}
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	if m.commandReceiversStarted {
		return
	}
	m.commandReceiversStarted = true
	m.channelsMu.Lock()
	channels := append([]Channel(nil), m.channels...)
	m.channelsMu.Unlock()
	startChannels(channels)
}

func (m *Manager) swapChannels(channels []Channel) ([]Channel, []*channelActivity) {
	m.channelsMu.Lock()
	oldChannels, oldActivity := m.channels, m.channelActivity
	m.channels = append([]Channel(nil), channels...)
	m.channelActivity = newChannelActivities(len(channels))
	m.channelsMu.Unlock()
	return oldChannels, oldActivity
}

func (m *Manager) beginChannelSends() []channelDelivery {
	m.channelsMu.Lock()
	m.ensureChannelActivitiesLocked()
	deliveries := make([]channelDelivery, len(m.channels))
	for index, channel := range m.channels {
		activity := m.channelActivity[index]
		activity.sends.Add(1)
		deliveries[index] = channelDelivery{channel: channel, activity: activity}
	}
	m.channelsMu.Unlock()
	return deliveries
}

func (m *Manager) ensureChannelActivitiesLocked() {
	if len(m.channelActivity) != len(m.channels) {
		m.channelActivity = newChannelActivities(len(m.channels))
	}
}

func newChannelActivities(count int) []*channelActivity {
	activities := make([]*channelActivity, count)
	for index := range activities {
		activities[index] = &channelActivity{}
	}
	return activities
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

func retireChannels(channels []Channel, activities []*channelActivity) {
	for index, channel := range channels {
		if index < len(activities) && activities[index] != nil {
			activities[index].sends.Wait()
		}
		_ = channel.Close()
	}
}

func closeChannels(channels []Channel) {
	for _, channel := range channels {
		_ = channel.Close()
	}
}

func (m *Manager) Close() {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	m.commandReceiversStarted = false
	channels, activity := m.swapChannels(nil)
	retireChannels(channels, activity)
}

func (m *Manager) UpdateConfig(cfg *config.Config) error {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	return m.initChannels(cfg)
}

// RevokeChannel retires a channel immediately while leaving unrelated channels active.
func (m *Manager) RevokeChannel(name string) bool {
	if m == nil {
		return false
	}
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	m.channelsMu.Lock()
	m.ensureChannelActivitiesLocked()
	kept := make([]Channel, 0, len(m.channels))
	keptActivity := make([]*channelActivity, 0, len(m.channels))
	revoked := make([]Channel, 0, 1)
	revokedActivity := make([]*channelActivity, 0, 1)
	for index, channel := range m.channels {
		if channel.Name() == name {
			revoked = append(revoked, channel)
			revokedActivity = append(revokedActivity, m.channelActivity[index])
			continue
		}
		kept = append(kept, channel)
		keptActivity = append(keptActivity, m.channelActivity[index])
	}
	if len(revoked) == 0 {
		m.channelsMu.Unlock()
		return false
	}
	m.channels = kept
	m.channelActivity = keptActivity
	m.channelsMu.Unlock()

	stopChannelCommandReceivers(revoked)
	retireChannels(revoked, revokedActivity)
	return true
}

func stopChannelCommandReceivers(channels []Channel) {
	for _, channel := range channels {
		if receiver, ok := channel.(channelCommandReceiver); ok {
			receiver.StopReceivingCommands()
		}
	}
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
