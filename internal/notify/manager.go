package notify

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/device"
	"github.com/yibaiba/hideck/pkg/logger"
)

// Manager 统一通知管理器
// 持有多个 Channel 实例，向所有已启用渠道广播通知和命令
type Manager struct {
	pool                    *device.Pool
	updateMu                sync.Mutex
	channelsMu              sync.Mutex
	channels                []Channel // 所有已启用的通知渠道
	channelActivity         []*channelActivity
	stateStore              RuntimeStateStore
	qqChannelFactory        func(config.QQConfig) (Channel, error)
	commandMu               sync.Mutex
	commandService          *CommandService
	commandExecutorMu       sync.RWMutex
	commandExecutor         ChannelCommandExecutor
	commandReceiversStarted bool
}

type ManagerOptions struct {
	StateStore                RuntimeStateStore
	QQChannelFactory          func(config.QQConfig) (Channel, error)
	DeferCommandReceiverStart bool
}

type NotificationContext struct {
	Event      string
	Text       string
	DeviceID   string
	DeviceName string
	Number     string
	Content    string
	Timestamp  time.Time
}

func (c NotificationContext) DeviceLabel() string {
	id := strings.TrimSpace(c.DeviceID)
	name := strings.TrimSpace(c.DeviceName)
	if name != "" && id != "" {
		return fmt.Sprintf("%s (%s)", name, id)
	}
	if name != "" {
		return name
	}
	if id != "" {
		return id
	}
	return "未知设备"
}

type contextualChannel interface {
	SendWithContext(ctx NotificationContext) error
}

// NewManager 根据配置创建通知管理器，初始化所有已启用的通知渠道
func NewManager(cfg *config.Config, pool *device.Pool) (*Manager, error) {
	return NewManagerWithOptions(cfg, pool, ManagerOptions{})
}

func NewManagerWithOptions(cfg *config.Config, pool *device.Pool, options ManagerOptions) (*Manager, error) {
	m := &Manager{
		pool:                    pool,
		stateStore:              options.StateStore,
		qqChannelFactory:        options.QQChannelFactory,
		commandReceiversStarted: !options.DeferCommandReceiverStart,
	}
	m.commandService = m.newCommandService()

	// 初始化所有通知渠道
	if err := m.initChannels(cfg); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) LoadRuntimeState() (RuntimeState, error) {
	if m == nil || m.stateStore == nil {
		return RuntimeState{}, ErrRuntimeStateStoreUnavailable
	}
	return m.stateStore.Load()
}

func (m *Manager) SaveRuntimeState(state RuntimeState) error {
	if m == nil || m.stateStore == nil {
		return ErrRuntimeStateStoreUnavailable
	}
	return m.stateStore.Save(state)
}

func (m *Manager) UpdateRuntimeState(update func(*RuntimeState) error) error {
	if m == nil || m.stateStore == nil {
		return ErrRuntimeStateStoreUnavailable
	}
	return m.stateStore.Update(update)
}

func (m *Manager) baseCommandHandlers() map[string]CommandHandler {
	return map[string]CommandHandler{
		"send":   m.handleCmdSendSMS,
		"status": m.handleCmdStatus,
		"rotate": m.handleCmdRotate,
		"list":   m.handleCmdList,
		"sms":    m.handleCmdSMSInbox,
		"esim":   m.handleCmdEsim,
		"switch": m.handleCmdSwitch,
		"vocall": m.handleCmdCall,
	}
}

func (m *Manager) CommandService() *CommandService {
	m.commandMu.Lock()
	defer m.commandMu.Unlock()
	if m.commandService == nil {
		m.commandService = m.newCommandService()
	}
	return m.commandService
}

func (m *Manager) newCommandService() *CommandService {
	service := NewCommandService(m.baseCommandHandlers())
	service.SetHelpDevicesProvider(m.helpDevices)
	return service
}

func (m *Manager) helpDevices() []HelpDevice {
	if m.pool == nil {
		return nil
	}
	labels := m.pool.WorkerLabels()
	devices := make([]HelpDevice, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label.ID) == "" {
			continue
		}
		devices = append(devices, HelpDevice{ID: label.ID, Name: label.Name})
	}
	return devices
}

func (m *Manager) SetBalanceCommandHandler(handler CommandHandler) error {
	return m.CommandService().SetHandler("balance", handler)
}

// NotifySMS 实现 device.Notifier 接口 — 收到短信通知
func (m *Manager) NotifySMS(deviceID, sender, content string, timestamp time.Time) {
	m.NotifySMSWithSource(deviceID, sender, content, "蜂窝", timestamp)
}

func (m *Manager) NotifySMSWithSource(deviceID, sender, content, source string, timestamp time.Time) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "蜂窝"
	}
	msg := fmt.Sprintf("收到新短信 / %s\n设备  %s\n号码  %s\n时间  %s\n内容  %s",
		source, deviceID, sender, timestamp.Format("2006-01-02 15:04:05"), content)

	logger.Info("开始发送短信通知",
		"event", "sms_received",
		"sms_device", deviceID,
		"source", source,
		"channel_count", m.channelCount())

	m.broadcastWithContext(NotificationContext{
		Event:      "sms_received",
		Text:       msg,
		DeviceID:   deviceID,
		DeviceName: m.resolveDeviceName(deviceID),
		Number:     sender,
		Content:    content,
		Timestamp:  timestamp,
	})
}

// NotifyRaw 发送原始文本通知到所有渠道
func (m *Manager) NotifyRaw(msg string) {
	m.broadcastWithContext(NotificationContext{
		Event:     "raw",
		Text:      msg,
		Timestamp: time.Now(),
	})
}

// NotifyIPRotated 实现 device.Notifier 接口 — IP 切换通知
func (m *Manager) NotifyIPRotated(deviceID, oldIP, newIP string, duration time.Duration) {
	displayName := deviceID
	if name := m.resolveDeviceName(deviceID); name != "" {
		displayName = fmt.Sprintf("%s (%s)", name, deviceID)
	}
	msg := fmt.Sprintf("公网切换 / 完成\n设备    %s\n旧 IP   %s\n新 IP   %s\n耗时    %s", displayName, oldIP, newIP, duration.String())
	m.broadcastWithContext(NotificationContext{
		Event:      "ip_rotated",
		Text:       msg,
		DeviceID:   deviceID,
		DeviceName: m.resolveDeviceName(deviceID),
		Timestamp:  time.Now(),
	})
}

// NotifyIncomingCall 实现 voice.CallNotifier 接口 — 来电通知
func (m *Manager) NotifyIncomingCall(deviceID, caller, callee string) {
	channelCount := m.channelCount()
	if channelCount == 0 {
		return
	}

	msg := fmt.Sprintf("来电通知\n设备    %s\n主叫    %s\n被叫    %s",
		deviceID, caller, callee)

	logger.Info("开始发送来电通知", "device", deviceID, "caller", caller, "channel_count", channelCount)

	m.broadcastWithContext(NotificationContext{
		Event:      "incoming_call",
		Text:       msg,
		DeviceID:   deviceID,
		DeviceName: m.resolveDeviceName(deviceID),
		Timestamp:  time.Now(),
	})
}

// NotifyCallResult publishes the terminal result after the call state machine
// has classified it. It is intentionally separate from the immediate ring.
func (m *Manager) NotifyCallResult(deviceID, peer, direction, status, reason string, at time.Time) {
	if m.channelCount() == 0 {
		return
	}
	labels := map[string]string{
		"completed": "已完成", "missed": "未接", "rejected": "已拒接",
		"busy": "忙线", "failed": "失败",
	}
	label := labels[status]
	if label == "" {
		label = status
	}
	message := fmt.Sprintf("通话结果 / %s\n设备    %s\n号码    %s\n方向    %s", label, deviceID, peer, direction)
	if strings.TrimSpace(reason) != "" {
		message += "\n原因    " + strings.TrimSpace(reason)
	}
	m.broadcastWithContext(NotificationContext{
		Event: "call_" + status, Text: message, DeviceID: deviceID,
		DeviceName: m.resolveDeviceName(deviceID), Timestamp: at,
	})
}

func (m *Manager) resolveDeviceName(deviceID string) string {
	if strings.TrimSpace(deviceID) == "" || m.pool == nil {
		return ""
	}
	return strings.TrimSpace(m.pool.WorkerName(deviceID))
}

func (m *Manager) deviceLabel(deviceID string) string {
	return (NotificationContext{DeviceID: deviceID, DeviceName: m.resolveDeviceName(deviceID)}).DeviceLabel()
}

func (m *Manager) broadcastWithContext(ctx NotificationContext) {
	ctx.Text = strings.TrimSpace(ctx.Text)
	if ctx.Text == "" {
		return
	}
	if ctx.Timestamp.IsZero() {
		ctx.Timestamp = time.Now()
	}
	if strings.TrimSpace(ctx.Event) == "" {
		ctx.Event = "notification"
	}

	deliveries := m.beginChannelSends()
	for _, delivery := range deliveries {
		delivery := delivery
		go func() {
			defer delivery.activity.sends.Done()
			ch := delivery.channel
			if withCtx, ok := ch.(contextualChannel); ok {
				if err := withCtx.SendWithContext(ctx); err != nil {
					logger.Warn("通知渠道发送失败", "channel", ch.Name(), "event", ctx.Event, "err", err)
				}
				return
			}
			if err := ch.Send(ctx.Text); err != nil {
				logger.Warn("通知渠道发送失败", "channel", ch.Name(), "event", ctx.Event, "err", err)
			}
		}()
	}
}
