package notify

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/pkg/logger"
)

type captureChannel struct {
	mu    sync.Mutex
	msgs  []string
	calls []NotificationContext
}

type blockingChannel struct {
	started   chan struct{}
	release   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

type namedLifecycleChannel struct {
	name      string
	closeOnce sync.Once
	closed    chan struct{}
}

func (c *namedLifecycleChannel) Name() string                           { return c.name }
func (c *namedLifecycleChannel) Send(string) error                      { return nil }
func (c *namedLifecycleChannel) RegisterCommand(string, CommandHandler) {}
func (c *namedLifecycleChannel) Start() error                           { return nil }
func (c *namedLifecycleChannel) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *blockingChannel) Name() string { return "blocking" }
func (c *blockingChannel) Send(string) error {
	c.startOnce.Do(func() { close(c.started) })
	<-c.release
	return nil
}
func (c *blockingChannel) RegisterCommand(string, CommandHandler) {}
func (c *blockingChannel) Start() error                           { return nil }
func (c *blockingChannel) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *captureChannel) Name() string { return "capture" }

func (c *captureChannel) Send(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, text)
	return nil
}

func (c *captureChannel) SendWithContext(ctx NotificationContext) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, ctx)
	c.msgs = append(c.msgs, ctx.Text)
	return nil
}

func (c *captureChannel) RegisterCommand(_ string, _ CommandHandler) {}
func (c *captureChannel) Start() error                               { return nil }
func (c *captureChannel) Close() error                               { return nil }

func (c *captureChannel) Last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return ""
	}
	return c.msgs[len(c.msgs)-1]
}

func (c *captureChannel) LastContext() NotificationContext {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return NotificationContext{}
	}
	return c.calls[len(c.calls)-1]
}

func readLogFields(t *testing.T, entry logger.LogEntry) map[string]any {
	t.Helper()
	if entry.Fields == "" {
		return map[string]any{}
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(entry.Fields), &fields); err != nil {
		t.Fatalf("failed to parse log fields: %v", err)
	}
	return fields
}

func waitLogEntry(t *testing.T, ch <-chan logger.LogEntry, match func(entry logger.LogEntry) bool) logger.LogEntry {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case entry := <-ch:
			if match(entry) {
				return entry
			}
		case <-deadline:
			t.Fatal("matched log entry not found")
		}
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestManagerNotifyEventsToWebhookWithTemplate(t *testing.T) {
	var mu sync.Mutex
	var payloads []webhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload webhookPayload
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh, err := NewWebhookChannel(webhookConfigForTest(srv.URL, "[{{device_label}}] {{text}}"))
	if err != nil {
		t.Fatalf("NewWebhookChannel() error = %v", err)
	}

	m := &Manager{channels: []Channel{wh}}

	ts := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	m.NotifySMS("wwan0", "+8613800000000", "hello", ts)
	m.NotifyIPRotated("wwan0", "1.1.1.1", "2.2.2.2", 2*time.Second)
	m.NotifyRaw("raw message")

	waitUntil(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(payloads) == 3
	})
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 3 {
		t.Fatalf("payload count=%d, want=3", len(payloads))
	}
	byEvent := make(map[string]webhookPayload, len(payloads))
	for _, payload := range payloads {
		byEvent[payload.Event] = payload
	}
	if got := byEvent["sms_received"].Text; got != "[wwan0] 收到新短信 / 蜂窝\n设备  wwan0\n号码  +8613800000000\n时间  2026-04-13 12:00:00\n内容  hello" {
		t.Fatalf("sms text=%q", got)
	}
	if got := byEvent["ip_rotated"].Meta.DeviceID; got != "wwan0" {
		t.Fatalf("ip_rotated meta.device_id=%q", got)
	}
	if _, ok := byEvent["raw"]; !ok {
		t.Fatal("raw event missing")
	}
}

func TestManagerNotifyRawKeepsPlainChannelText(t *testing.T) {
	capture := &captureChannel{}
	m := &Manager{channels: []Channel{capture}}

	m.NotifyRaw("plain channel text")
	waitUntil(t, time.Second, func() bool { return capture.Last() != "" })
	if got := capture.Last(); got != "plain channel text" {
		t.Fatalf("plain channel text=%q", got)
	}
}

func TestManagerCloseWaitsForInFlightChannelSend(t *testing.T) {
	channel := &blockingChannel{
		started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
	}
	manager := &Manager{channels: []Channel{channel}}
	manager.NotifyRaw("wait for delivery")
	select {
	case <-channel.started:
	case <-time.After(time.Second):
		t.Fatal("channel send did not start")
	}

	closed := make(chan struct{})
	go func() {
		manager.Close()
		close(closed)
	}()
	select {
	case <-channel.closed:
		t.Fatal("channel closed before in-flight send completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(channel.release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("manager close did not finish")
	}
}

func TestManagerChannelLifecycleConcurrentAccess(t *testing.T) {
	manager := &Manager{channels: []Channel{&captureChannel{}}}
	cfg := &config.Config{}
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		for i := 0; i < 100; i++ {
			if err := manager.UpdateConfig(cfg); err != nil {
				t.Errorf("UpdateConfig() error = %v", err)
				return
			}
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < 100; i++ {
			manager.NotifyRaw("concurrent")
		}
	}()
	go func() {
		defer wait.Done()
		for i := 0; i < 100; i++ {
			_ = manager.GetChannelNames()
		}
	}()
	wait.Wait()
	manager.Close()
}

func TestManagerUpdateConfigFailureKeepsCurrentChannels(t *testing.T) {
	capture := &captureChannel{}
	manager := &Manager{channels: []Channel{capture}}
	err := manager.UpdateConfig(&config.Config{Weixin: config.WeixinConfig{Enabled: true}})
	if !errors.Is(err, ErrRuntimeStateStoreUnavailable) {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if names := manager.GetChannelNames(); len(names) != 1 || names[0] != "capture" {
		t.Fatalf("GetChannelNames() = %v", names)
	}
	manager.NotifyRaw("still active")
	waitUntil(t, time.Second, func() bool { return capture.Last() == "still active" })
	manager.Close()
}

func TestManagerRevokeChannelRetiresOnlyMatchingChannel(t *testing.T) {
	telegram := &namedLifecycleChannel{name: "telegram", closed: make(chan struct{})}
	other := &namedLifecycleChannel{name: "other", closed: make(chan struct{})}
	manager := &Manager{channels: []Channel{telegram, other}}

	if !manager.RevokeChannel("telegram") {
		t.Fatal("RevokeChannel() = false, want true")
	}
	select {
	case <-telegram.closed:
	default:
		t.Fatal("Telegram channel was not closed")
	}
	select {
	case <-other.closed:
		t.Fatal("unrelated channel was closed")
	default:
	}
	if names := manager.GetChannelNames(); len(names) != 1 || names[0] != "other" {
		t.Fatalf("GetChannelNames() = %v", names)
	}
	manager.Close()
}

func TestManagerNotifyIPRotatedUsesPlainTemplate(t *testing.T) {
	capture := &captureChannel{}
	m := &Manager{channels: []Channel{capture}}

	m.NotifyIPRotated("wwan0", "1.1.1.1", "2.2.2.2", 2*time.Second)
	waitUntil(t, time.Second, func() bool { return capture.Last() != "" })
	want := "公网切换 / 完成\n设备    wwan0\n旧 IP   1.1.1.1\n新 IP   2.2.2.2\n耗时    2s"
	if got := capture.Last(); got != want {
		t.Fatalf("ip rotated text=%q, want %q", got, want)
	}
}

func TestManagerNotifyIncomingCallUsesPlainTemplate(t *testing.T) {
	capture := &captureChannel{}
	m := &Manager{channels: []Channel{capture}}

	m.NotifyIncomingCall("wwan0", "10086", "10010")
	time.Sleep(20 * time.Millisecond)
	want := "来电通知\n设备    wwan0\n主叫    10086\n被叫    10010"
	if got := capture.Last(); got != want {
		t.Fatalf("incoming call text=%q, want %q", got, want)
	}
}

func TestManagerNotifyCallResultIsSeparateFromIncomingCall(t *testing.T) {
	capture := &captureChannel{}
	m := &Manager{channels: []Channel{capture}}
	at := time.Date(2026, 8, 13, 13, 0, 0, 0, time.UTC)

	m.NotifyIncomingCall("wwan0", "10086", "10010")
	m.NotifyCallResult("wwan0", "10086", "incoming", "missed", "remote_cancel", at)

	waitUntil(t, time.Second, func() bool {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		return len(capture.calls) == 2
	})
	capture.mu.Lock()
	defer capture.mu.Unlock()
	events := make(map[string]NotificationContext, len(capture.calls))
	for _, call := range capture.calls {
		events[call.Event] = call
	}
	if _, ok := events["incoming_call"]; !ok {
		t.Fatal("incoming_call event missing")
	}
	result, ok := events["call_missed"]
	if !ok {
		t.Fatal("call_missed event missing")
	}
	if result.Timestamp != at {
		t.Fatalf("result timestamp=%s, want %s", result.Timestamp, at)
	}
	want := "通话结果 / 未接\n设备    wwan0\n号码    10086\n方向    incoming\n原因    remote_cancel"
	if result.Text != want {
		t.Fatalf("result text=%q, want %q", result.Text, want)
	}
}

func TestManagerNotifySMSLogsBroadcastSummary(t *testing.T) {
	logger.Setup(logger.LogConfig{Debug: true, Filename: filepath.Join(t.TempDir(), "app.log")})
	capture := &captureChannel{}
	m := &Manager{channels: []Channel{capture}}
	ch := logger.GlobalBroadcaster.Subscribe()
	defer logger.GlobalBroadcaster.Unsubscribe(ch)

	ts := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	m.NotifySMS("wwan0", "+8613800000000", "hello", ts)

	entry := waitLogEntry(t, ch, func(entry logger.LogEntry) bool {
		return entry.Message == "开始发送短信通知"
	})
	fields := readLogFields(t, entry)
	if fields["event"] != "sms_received" {
		t.Fatalf("event=%v want sms_received", fields["event"])
	}
	if fields["channel_count"] != float64(1) {
		t.Fatalf("channel_count=%v want 1", fields["channel_count"])
	}
}

func TestManagerNotifySMSWithSourceUsesProvidedSourceLabel(t *testing.T) {
	capture := &captureChannel{}
	m := &Manager{channels: []Channel{capture}}
	notifier, ok := any(m).(interface {
		NotifySMSWithSource(deviceID, sender, content, source string, timestamp time.Time)
	})
	if !ok {
		t.Fatal("NotifySMSWithSource missing")
	}

	ts := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	notifier.NotifySMSWithSource("wwan0", "+8613800000000", "hello", "VoWiFi", ts)

	waitUntil(t, time.Second, func() bool { return capture.Last() != "" })
	want := "收到新短信 / VoWiFi\n设备  wwan0\n号码  +8613800000000\n时间  2026-04-13 12:00:00\n内容  hello"
	if got := capture.Last(); got != want {
		t.Fatalf("text=%q, want %q", got, want)
	}
	if got := capture.LastContext().Event; got != "sms_received" {
		t.Fatalf("event=%q, want sms_received", got)
	}
}

func webhookConfigForTest(url, template string) config.WebhookConfig {
	return config.WebhookConfig{
		Enabled:      true,
		URLs:         []string{url},
		TimeoutMs:    5000,
		RetryMax:     0,
		TextTemplate: template,
	}
}
