package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yibaiba/hideck/internal/config"
)

func TestWeixinChannelRejectsNotificationWithoutContextToken(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
	}))
	t.Cleanup(provider.Close)

	channel, _ := newWeixinDeliveryTestChannel(t, provider.URL, nil)
	err := channel.Send("server alert")
	if err == nil || !strings.Contains(err.Error(), "没有可用会话上下文") || requests != 0 {
		t.Fatalf("Send() error = %v, requests = %d", err, requests)
	}
}

func TestWeixinChannelClearsStaleContextWithoutRetry(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		message := decodeWeixinSendMessage(t, r)
		if message["context_token"] != "stale-context" {
			t.Fatalf("context_token = %#v", message["context_token"])
		}
		_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"prepare failed"}`))
	}))
	t.Cleanup(provider.Close)

	channel, store := newWeixinDeliveryTestChannel(t, provider.URL, map[string]string{"user-1": "stale-context"})
	err := channel.Send("server alert")
	if err == nil || !strings.Contains(err.Error(), "会话上下文已失效") || requests != 1 {
		t.Fatalf("Send() error = %v, requests = %d", err, requests)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Weixin.ContextTokens["user-1"]; exists {
		t.Fatalf("stale context token was retained: %#v", state.Weixin.ContextTokens)
	}
}

func TestWeixinChannelDoesNotClearUnrelatedSendFailure(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"media busy"}`))
	}))
	t.Cleanup(provider.Close)

	channel, store := newWeixinDeliveryTestChannel(t, provider.URL, map[string]string{"user-1": "active-context"})
	err := channel.Send("server alert")
	if err == nil || !strings.Contains(err.Error(), "media busy") || requests != 1 {
		t.Fatalf("Send() error = %v, requests = %d", err, requests)
	}
	state, loadErr := store.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if state.Weixin.ContextTokens["user-1"] != "active-context" {
		t.Fatalf("context token was cleared: %#v", state.Weixin.ContextTokens)
	}
}

func TestWeixinChannelDoesNotClearNewContextAfterStaleResponse(t *testing.T) {
	var channel *WeixinChannel
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		message := decodeWeixinSendMessage(t, r)
		if requests == 2 {
			if message["context_token"] != "fresh-context" {
				t.Fatalf("retry context_token = %#v", message["context_token"])
			}
			_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
			return
		}
		allowed, _, err := channel.authorizeMessage(weixinAuthorizationRequest{
			Kind: "direct", ChatID: "user-1", Sender: "user-1", ContextToken: "fresh-context",
		})
		if err != nil || !allowed {
			t.Errorf("authorizeMessage() = allowed:%v error:%v", allowed, err)
		}
		_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"prepare failed"}`))
	}))
	t.Cleanup(provider.Close)

	var store *FileRuntimeStateStore
	channel, store = newWeixinDeliveryTestChannel(
		t, provider.URL, map[string]string{"user-1": "stale-context"},
	)
	channel.config.AllowedUserIDs = []string{"user-1"}
	if err := channel.Send("server alert"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Weixin.ContextTokens["user-1"] != "fresh-context" {
		t.Fatalf("fresh context token was cleared: %#v", state.Weixin.ContextTokens)
	}
}

func newWeixinDeliveryTestChannel(
	t *testing.T,
	baseURL string,
	contextTokens map[string]string,
) (*WeixinChannel, *FileRuntimeStateStore) {
	t.Helper()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{
		AccountID: "bot-1", Token: "token-1", BaseURL: baseURL,
		DefaultTarget: "user-1", ContextTokens: contextTokens,
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true}, StateStore: store,
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	return channel, store
}

func decodeWeixinSendMessage(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	var body struct {
		Message map[string]any `json:"msg"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Fatalf("decode send body: %v", err)
	}
	return body.Message
}
