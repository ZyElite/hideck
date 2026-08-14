package notify

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

const (
	defaultWeComWebSocketURL = "wss://openws.work.weixin.qq.com"
	defaultWeComHeartbeat    = 30 * time.Second
	defaultWeComConnectWait  = 20 * time.Second
	defaultWeComRequestWait  = 15 * time.Second
	weComSeenMessageLimit    = 1000
)

type WeComBotOptions struct {
	Config            config.WeComBotConfig
	StateStore        RuntimeStateStore
	Dialer            WeComBotDialer
	HeartbeatInterval time.Duration
	ConnectTimeout    time.Duration
	RequestTimeout    time.Duration
	ReconnectBackoff  []time.Duration
	Now               func() time.Time
}

type WeComBotChannel struct {
	config            config.WeComBotConfig
	stateStore        RuntimeStateStore
	dialer            WeComBotDialer
	heartbeatInterval time.Duration
	connectTimeout    time.Duration
	requestTimeout    time.Duration
	reconnectBackoff  []time.Duration
	now               func() time.Time
	deviceID          string

	commandMu sync.RWMutex
	commands  map[string]CommandHandler
	stateMu   sync.RWMutex
	state     RuntimeState
	authMu    sync.Mutex

	runMu   sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	connMu  sync.RWMutex
	conn    WeComBotConnection
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan weComPendingResult
	seenMu    sync.Mutex
	seen      map[string]struct{}
	seenOrder []string
}

func NewWeComBotChannel(options WeComBotOptions) (*WeComBotChannel, error) {
	if !options.Config.Enabled {
		return nil, nil
	}
	if options.StateStore == nil {
		return nil, ErrRuntimeStateStoreUnavailable
	}
	cfg, err := normalizeWeComBotConfig(options.Config)
	if err != nil {
		return nil, err
	}
	state, err := options.StateStore.Load()
	if err != nil {
		return nil, err
	}
	deviceID, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("生成企业微信长连接设备 ID 失败: %w", err)
	}
	connectTimeout := durationOrDefault(options.ConnectTimeout, defaultWeComConnectWait)
	dialer := options.Dialer
	if dialer == nil {
		dialer = newGorillaWeComBotDialer(connectTimeout)
	}
	return &WeComBotChannel{
		config: cfg, stateStore: options.StateStore, dialer: dialer,
		heartbeatInterval: durationOrDefault(options.HeartbeatInterval, defaultWeComHeartbeat),
		connectTimeout:    connectTimeout, requestTimeout: durationOrDefault(options.RequestTimeout, defaultWeComRequestWait),
		reconnectBackoff: durationsOrDefault(options.ReconnectBackoff, defaultWeComReconnectBackoff),
		now:              functionOrNow(options.Now), deviceID: deviceID, commands: make(map[string]CommandHandler),
		state: state, pending: make(map[string]chan weComPendingResult), seen: make(map[string]struct{}),
	}, nil
}

func (w *WeComBotChannel) Name() string { return "wecom_bot" }

func (w *WeComBotChannel) Send(text string) error {
	state := w.snapshotState()
	target := strings.TrimSpace(state.WeComBot.DefaultTarget)
	if target == "" {
		return errors.New("企业微信长连接尚未绑定默认通知目标")
	}
	return w.sendText(context.Background(), target, "", text)
}

func (w *WeComBotChannel) RegisterCommand(name string, handler CommandHandler) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || handler == nil {
		return
	}
	w.commandMu.Lock()
	w.commands[name] = handler
	w.commandMu.Unlock()
}

func (w *WeComBotChannel) Start() error {
	ctx, done, err := w.beginRun()
	if err != nil {
		return err
	}
	defer w.finishRun(done)
	failures := 0
	for {
		connected, serveErr := w.serveConnection(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if serveErr != nil {
			logger.Warn("企业微信长连接中断", "err", serveErr)
		}
		if connected {
			failures = 0
		}
		delay := w.reconnectBackoff[min(failures, len(w.reconnectBackoff)-1)]
		failures++
		if !waitContext(ctx, delay) {
			return nil
		}
	}
}

func (w *WeComBotChannel) Close() error {
	w.runMu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel, w.done = nil, nil
	w.runMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if conn := w.currentConnection(); conn != nil {
		_ = conn.Close()
	}
	<-done
	return nil
}

func (w *WeComBotChannel) beginRun() (context.Context, chan struct{}, error) {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	if w.cancel != nil {
		return nil, nil, errors.New("企业微信长连接监听已经启动")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel, w.done = cancel, done
	return ctx, done, nil
}

func (w *WeComBotChannel) finishRun(done chan struct{}) {
	w.runMu.Lock()
	if w.done == done {
		w.cancel, w.done = nil, nil
	}
	w.runMu.Unlock()
	close(done)
}

func normalizeWeComBotConfig(cfg config.WeComBotConfig) (config.WeComBotConfig, error) {
	cfg.BotID, cfg.Secret = strings.TrimSpace(cfg.BotID), strings.TrimSpace(cfg.Secret)
	cfg.WebSocketURL = strings.TrimSpace(cfg.WebSocketURL)
	if cfg.WebSocketURL == "" {
		cfg.WebSocketURL = defaultWeComWebSocketURL
	}
	if cfg.BotID == "" || cfg.Secret == "" {
		return config.WeComBotConfig{}, errors.New("企业微信长连接已启用，但 Bot ID 或 Secret 为空")
	}
	parsed, err := url.Parse(cfg.WebSocketURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return config.WeComBotConfig{}, errors.New("企业微信长连接地址无效")
	}
	if parsed.Scheme != "wss" && !(parsed.Scheme == "ws" && isLoopbackWeComHost(parsed.Hostname())) {
		return config.WeComBotConfig{}, errors.New("企业微信长连接地址必须使用 WSS；本机测试可使用 WS")
	}
	cfg.AllowedUserIDs = append([]string(nil), cfg.AllowedUserIDs...)
	cfg.AllowedGroupIDs = append([]string(nil), cfg.AllowedGroupIDs...)
	return cfg, nil
}

func isLoopbackWeComHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func durationOrDefault(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func durationsOrDefault(values, fallback []time.Duration) []time.Duration {
	if len(values) == 0 {
		return append([]time.Duration(nil), fallback...)
	}
	return append([]time.Duration(nil), values...)
}

func functionOrNow(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}

func (w *WeComBotChannel) currentConnection() WeComBotConnection {
	w.connMu.RLock()
	defer w.connMu.RUnlock()
	return w.conn
}

func (w *WeComBotChannel) snapshotState() RuntimeState {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return cloneRuntimeState(w.state)
}

var _ Channel = (*WeComBotChannel)(nil)
