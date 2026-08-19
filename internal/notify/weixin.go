package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

const (
	weixinRetryDelay   = 2 * time.Second
	weixinBackoffDelay = 30 * time.Second
	weixinSessionDelay = 10 * time.Minute
)

type WeixinChannelOptions struct {
	Config     config.WeixinConfig
	StateStore RuntimeStateStore
	HTTPClient *http.Client
}

type WeixinChannel struct {
	config     config.WeixinConfig
	stateStore RuntimeStateStore
	client     *weixinMessageClient

	authMu    sync.Mutex
	commandMu sync.RWMutex
	commands  map[string]CommandHandler
	stateMu   sync.RWMutex
	state     RuntimeState
	runMu     sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
}

type weixinAuthorizationRequest struct {
	Kind         string
	ChatID       string
	Sender       string
	ContextToken string
}

func NewWeixinChannel(options WeixinChannelOptions) (*WeixinChannel, error) {
	if !options.Config.Enabled {
		return nil, nil
	}
	if options.StateStore == nil {
		return nil, ErrRuntimeStateStoreUnavailable
	}
	state, err := options.StateStore.Load()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(state.Weixin.AccountID) == "" || strings.TrimSpace(state.Weixin.Token) == "" {
		return nil, errors.New("个人微信已启用，但尚未完成扫码登录")
	}
	return &WeixinChannel{
		config: options.Config, stateStore: options.StateStore,
		client: newWeixinMessageClient(options.HTTPClient), commands: make(map[string]CommandHandler), state: state,
	}, nil
}

func (w *WeixinChannel) Name() string { return "weixin" }

func (w *WeixinChannel) Send(text string) error {
	state := w.snapshotState()
	target := strings.TrimSpace(state.Weixin.DefaultTarget)
	if target == "" {
		return errors.New("个人微信尚未绑定默认通知目标")
	}
	return w.sendTo(context.Background(), target, text)
}

func (w *WeixinChannel) SendRegistrationHelp(target, text string) error {
	if w == nil {
		return errors.New("个人微信渠道未初始化")
	}
	if strings.TrimSpace(target) == "" {
		return errors.New("个人微信注册帮助目标为空")
	}
	return w.sendTo(context.Background(), target, text)
}

func (w *WeixinChannel) RegisterCommand(name string, handler CommandHandler) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || handler == nil {
		return
	}
	w.commandMu.Lock()
	w.commands[name] = handler
	w.commandMu.Unlock()
}

func (w *WeixinChannel) Start() error {
	w.runMu.Lock()
	if w.cancel != nil {
		w.runMu.Unlock()
		return errors.New("个人微信消息监听已经启动")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w.cancel, w.done = cancel, done
	w.runMu.Unlock()
	defer close(done)
	w.notifyStarted(ctx)
	return w.pollLoop(ctx)
}

func (w *WeixinChannel) Close() error {
	w.runMu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel, w.done = nil, nil
	w.runMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	<-done
	return w.notifyStopped()
}

func (w *WeixinChannel) notifyStarted(ctx context.Context) {
	lifecycleCtx, cancel := context.WithTimeout(ctx, weixinLifecycleTimeout)
	defer cancel()
	state := w.snapshotState()
	if err := w.client.notifyStart(lifecycleCtx, weixinCredentials(state)); err != nil {
		logger.Warn("个人微信在线状态登记失败", "action", "start", "err", err)
		return
	}
	logger.Info("个人微信在线状态已登记")
}

func (w *WeixinChannel) notifyStopped() error {
	lifecycleCtx, cancel := context.WithTimeout(context.Background(), weixinLifecycleTimeout)
	defer cancel()
	state := w.snapshotState()
	if err := w.client.notifyStop(lifecycleCtx, weixinCredentials(state)); err != nil {
		logger.Warn("个人微信在线状态登记失败", "action", "stop", "err", err)
		return err
	}
	logger.Info("个人微信离线状态已登记")
	return nil
}

func (w *WeixinChannel) authorizeMessage(request weixinAuthorizationRequest) (bool, bool, error) {
	w.authMu.Lock()
	defer w.authMu.Unlock()
	state := w.snapshotState()
	changed := false
	bound := false
	if request.Kind == "group" {
		if !containsString(w.config.AllowedGroupIDs, request.ChatID) || !w.allowedDirect(state, request.Sender) {
			return false, false, nil
		}
	} else {
		if !w.allowedDirect(state, request.Sender) {
			if len(w.config.AllowedUserIDs) > 0 || len(state.Weixin.AllowedUsers) > 0 || state.Weixin.DefaultTarget != "" {
				return false, false, nil
			}
			state.Weixin.AllowedUsers = append(state.Weixin.AllowedUsers, request.Sender)
			changed = true
			bound = true
		}
		if strings.TrimSpace(state.Weixin.DefaultTarget) == "" {
			if target := firstNonEmpty(request.ChatID, request.Sender); target != "" {
				state.Weixin.DefaultTarget = target
				changed = true
				bound = true
			}
		}
	}
	if strings.TrimSpace(request.ContextToken) != "" {
		if state.Weixin.ContextTokens == nil {
			state.Weixin.ContextTokens = make(map[string]string)
		}
		contextToken := strings.TrimSpace(request.ContextToken)
		if state.Weixin.ContextTokens[request.ChatID] != contextToken {
			state.Weixin.ContextTokens[request.ChatID] = contextToken
			changed = true
		}
	}
	if !changed {
		return true, bound, nil
	}
	if err := w.saveState(state); err != nil {
		return false, false, err
	}
	return true, bound, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (w *WeixinChannel) executeMessage(ctx context.Context, chatID, text string) error {
	name, args, err := parseCommand(text)
	if errors.Is(err, ErrInvalidCommand) {
		return w.sendTo(ctx, chatID, "请发送 /help 查看可用命令")
	}
	if err != nil {
		return err
	}
	w.commandMu.RLock()
	handler := w.commands[name]
	w.commandMu.RUnlock()
	if handler == nil {
		return w.sendTo(ctx, chatID, unknownCommandReply(text))
	}
	commandContext := &weixinCommandContext{channel: w, target: chatID}
	defer commandContext.release()
	response := handler(commandContext, args)
	if response != "" {
		if err := commandContext.respond(ctx, weixinCommandReply{text: response}); err != nil {
			return err
		}
	}
	return nil
}

func (w *WeixinChannel) sendTo(ctx context.Context, target, text string) error {
	state := w.snapshotState()
	target = strings.TrimSpace(target)
	contextToken := strings.TrimSpace(state.Weixin.ContextTokens[target])
	if contextToken == "" {
		return errors.New("个人微信还没有会话令牌，请先在微信里给这个机器人发一条消息")
	}
	return w.client.sendText(ctx, weixinSendTextRequest{
		Credentials: weixinCredentials(state), Target: target,
		Text: text, ContextToken: contextToken,
	})
}

func (w *WeixinChannel) allowedDirect(state RuntimeState, userID string) bool {
	return containsString(w.config.AllowedUserIDs, userID) || containsString(state.Weixin.AllowedUsers, userID)
}

func (w *WeixinChannel) snapshotState() RuntimeState {
	w.stateMu.RLock()
	defer w.stateMu.RUnlock()
	return cloneRuntimeState(w.state)
}

func (w *WeixinChannel) updateState(update func(*RuntimeState)) error {
	state := w.snapshotState()
	update(&state)
	return w.saveState(state)
}

func (w *WeixinChannel) saveState(state RuntimeState) error {
	var stored RuntimeState
	if err := w.stateStore.Update(func(current *RuntimeState) error {
		current.Weixin = cloneRuntimeState(state).Weixin
		stored = cloneRuntimeState(*current)
		return nil
	}); err != nil {
		return err
	}
	w.stateMu.Lock()
	w.state = stored
	w.stateMu.Unlock()
	return nil
}

func weixinCredentials(state RuntimeState) WeixinQRCredentials {
	return WeixinQRCredentials{
		AccountID: state.Weixin.AccountID, Token: state.Weixin.Token,
		BaseURL: state.WeixinBaseURL(), UserID: state.Weixin.UserID,
	}
}

func (s RuntimeState) WeixinBaseURL() string {
	if strings.TrimSpace(s.Weixin.BaseURL) != "" {
		return s.Weixin.BaseURL
	}
	return DefaultWeixinBaseURL
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func pollRetryDelay(failures int) time.Duration {
	if failures >= 3 {
		return weixinBackoffDelay
	}
	return weixinRetryDelay
}

func weixinResponseError(response weixinUpdatesResponse) (time.Duration, error) {
	if response.Ret == 0 && response.ErrCode == 0 {
		return 0, nil
	}
	delay := weixinRetryDelay
	if response.Ret == -14 || response.ErrCode == -14 {
		delay = weixinSessionDelay
	}
	return delay, fmt.Errorf("ret=%d errcode=%d errmsg=%s", response.Ret, response.ErrCode, response.ErrMsg)
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
