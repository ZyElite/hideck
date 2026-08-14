package notify

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotificationChannelNotFound = errors.New("通知渠道未运行")
	ErrRegistrationHelpUnsupported = errors.New("通知渠道不支持定向注册帮助")
)

func (m *Manager) SendRegistrationHelp(channelName, target string) error {
	channelName = strings.TrimSpace(channelName)
	target = strings.TrimSpace(target)
	if channelName == "" || target == "" {
		return errors.New("注册帮助的渠道和目标不能为空")
	}
	delivery, ok := m.beginNamedChannelSend(channelName)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotificationChannelNotFound, channelName)
	}
	defer delivery.activity.sends.Done()
	sender, ok := delivery.channel.(RegistrationHelpSender)
	if !ok {
		return fmt.Errorf("%w: %s", ErrRegistrationHelpUnsupported, channelName)
	}
	return sender.SendRegistrationHelp(target, m.CommandService().HelpText())
}

func (m *Manager) beginNamedChannelSend(name string) (channelDelivery, bool) {
	if m == nil {
		return channelDelivery{}, false
	}
	m.channelsMu.Lock()
	defer m.channelsMu.Unlock()
	m.ensureChannelActivitiesLocked()
	for index, channel := range m.channels {
		if channel.Name() != name {
			continue
		}
		activity := m.channelActivity[index]
		activity.sends.Add(1)
		return channelDelivery{channel: channel, activity: activity}, true
	}
	return channelDelivery{}, false
}
