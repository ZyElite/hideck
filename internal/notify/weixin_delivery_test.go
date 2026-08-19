package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/yibaiba/hideck/internal/config"
)

func TestWeixinChannelSendsNotificationWithoutContextToken(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		message := decodeWeixinSendMessage(t, r)
		if _, exists := message["context_token"]; exists {
			t.Fatalf("tokenless notification included context_token: %#v", message)
		}
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
	}))
	t.Cleanup(provider.Close)

	channel, _ := newWeixinDeliveryTestChannel(t, provider.URL, nil)
	if err := channel.Send("server alert"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestWeixinChannelRetriesStaleContextWithoutToken(t *testing.T) {
	var mu sync.Mutex
	contexts := make([]string, 0, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		message := decodeWeixinSendMessage(t, r)
		contextToken, _ := message["context_token"].(string)
		mu.Lock()
		contexts = append(contexts, contextToken)
		attempt := len(contexts)
		mu.Unlock()
		if attempt == 1 {
			_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"prepare failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
	}))
	t.Cleanup(provider.Close)

	channel, store := newWeixinDeliveryTestChannel(t, provider.URL, map[string]string{"user-1": "stale-context"})
	if err := channel.Send("server alert"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	gotContexts := append([]string(nil), contexts...)
	mu.Unlock()
	if len(gotContexts) != 2 || gotContexts[0] != "stale-context" || gotContexts[1] != "" {
		t.Fatalf("send contexts = %#v", gotContexts)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Weixin.ContextTokens["user-1"]; exists {
		t.Fatalf("stale context token was retained: %#v", state.Weixin.ContextTokens)
	}
}

func TestWeixinChannelSurfacesTokenlessRetryFailure(t *testing.T) {
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"prepare failed"}`))
	}))
	t.Cleanup(provider.Close)

	channel, _ := newWeixinDeliveryTestChannel(t, provider.URL, map[string]string{"user-1": "stale-context"})
	err := channel.Send("server alert")
	if err == nil || !strings.Contains(err.Error(), "主动通知重试也失败") || requests != 2 {
		t.Fatalf("Send() error = %v, requests = %d", err, requests)
	}
}

func TestWeixinChannelDoesNotClearNewContextAfterFallback(t *testing.T) {
	var channel *WeixinChannel
	requests := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"ret":-2,"errcode":0,"errmsg":"prepare failed"}`))
			return
		}
		allowed, _, err := channel.authorizeMessage(weixinAuthorizationRequest{
			Kind: "direct", ChatID: "user-1", Sender: "user-1", ContextToken: "fresh-context",
		})
		if err != nil || !allowed {
			t.Errorf("authorizeMessage() = allowed:%v error:%v", allowed, err)
		}
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
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
