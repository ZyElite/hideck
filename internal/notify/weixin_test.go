package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
)

func TestWeixinChannelBindsFirstDirectMessageAndExecutesHelp(t *testing.T) {
	var mu sync.Mutex
	updateCalls := 0
	sentBodies := make(chan map[string]any, 2)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getupdates":
			mu.Lock()
			updateCalls++
			call := updateCalls
			mu.Unlock()
			if call > 1 {
				time.Sleep(10 * time.Millisecond)
				_, _ = w.Write([]byte(`{"ret":0}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ret": 0, "get_updates_buf": "cursor-2",
				"msgs": []any{map[string]any{
					"from_user_id": "user-1", "to_user_id": "bot-1", "message_id": "message-1",
					"message_type": 1, "context_token": "context-1",
					"item_list": []any{map[string]any{"type": 1, "text_item": map[string]string{"text": "/help"}}},
				}},
			})
		case "/ilink/bot/sendmessage":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode send body: %v", err)
			}
			sentBodies <- body
			_, _ = w.Write([]byte(`{"ret":0,"errcode":0}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{AccountID: "bot-1", Token: "token-1", BaseURL: provider.URL}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true, BaseURL: provider.URL}, StateStore: store, HTTPClient: provider.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	channel.RegisterCommand("help", func(_ CommandContext, _ []string) string {
		return "可用设备（1）\n- modem-01  主卡\n\n命令帮助"
	})
	startResult := make(chan error, 1)
	go func() { startResult <- channel.Start() }()

	var sent map[string]any
	select {
	case sent = <-sentBodies:
	case <-time.After(2 * time.Second):
		t.Fatal("help response was not sent")
	}
	waitUntil(t, 2*time.Second, func() bool {
		state, err := store.Load()
		return err == nil && state.Weixin.SyncBuffer == "cursor-2"
	})
	if err := channel.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-startResult; err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	message := sent["msg"].(map[string]any)
	if message["to_user_id"] != "user-1" || message["context_token"] != "context-1" {
		t.Fatalf("sent message = %#v", message)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Weixin.DefaultTarget != "user-1" || loaded.Weixin.SyncBuffer != "cursor-2" || loaded.Weixin.ContextTokens["user-1"] != "context-1" {
		t.Fatalf("saved state = %+v", loaded.Weixin)
	}
}

func TestWeixinChannelGroupCannotClaimFirstBinding(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/sendmessage" {
			t.Error("unauthorized group triggered a reply")
		}
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{AccountID: "bot-1", Token: "token-1", BaseURL: provider.URL}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true, BaseURL: provider.URL}, StateStore: store, HTTPClient: provider.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	message := weixinMessage{FromUserID: "user-1", ToUserID: "group-1", RoomID: "group-1", MessageType: 1}
	message.Items = textWeixinItems("/help")
	if err := channel.processMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Weixin.DefaultTarget != "" || len(loaded.Weixin.AllowedUsers) != 0 {
		t.Fatalf("group claimed binding: %+v", loaded.Weixin)
	}
}

func TestWeixinChannelUsesPersistedCursorAfterRestart(t *testing.T) {
	cursorSeen := make(chan string, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Cursor string `json:"get_updates_buf"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		select {
		case cursorSeen <- body.Cursor:
		default:
		}
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer provider.Close()
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.Weixin = WeixinRuntimeState{
		AccountID: "bot-1", Token: "token-1", BaseURL: provider.URL, SyncBuffer: "saved-cursor",
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	channel, err := NewWeixinChannel(WeixinChannelOptions{
		Config: config.WeixinConfig{Enabled: true}, StateStore: store, HTTPClient: provider.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	startResult := make(chan error, 1)
	go func() { startResult <- channel.Start() }()
	select {
	case cursor := <-cursorSeen:
		if cursor != "saved-cursor" {
			t.Fatalf("cursor = %q", cursor)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("getupdates was not called")
	}
	_ = channel.Close()
	if err := <-startResult; err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatal(err)
	}
}

func textWeixinItems(text string) []weixinMessageItem {
	item := weixinMessageItem{Type: weixinItemText}
	item.TextItem.Text = text
	return []weixinMessageItem{item}
}
