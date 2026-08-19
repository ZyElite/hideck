package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
)

func TestUpdateNotificationSettingsPersistsWeixinConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{configPath: configPath, fullCfg: &config.Config{
		Weixin: config.WeixinConfig{CDNBaseURL: "https://cdn.example.com"},
	}}
	body := `{"weixin":{"enabled":true,"base_url":"https://ilink.example.com/","allowed_user_ids":[" user-1 ","user-1"],"allowed_group_ids":["group-1"]}}`
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/settings/notifications", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")

	server.handleUpdateNotificationSettings(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !server.fullCfg.Weixin.Enabled || server.fullCfg.Weixin.BaseURL != "https://ilink.example.com" ||
		server.fullCfg.Weixin.CDNBaseURL != "https://cdn.example.com" ||
		!reflect.DeepEqual(server.fullCfg.Weixin.AllowedUserIDs, []string{"user-1"}) {
		t.Fatalf("weixin config = %+v", server.fullCfg.Weixin)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "allowed_group_ids:") || !strings.Contains(string(raw), "group-1") {
		t.Fatalf("persisted config = %s", raw)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v", info.Mode().Perm())
	}
}

func TestWeixinSettingsExposeRuntimeBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := notify.NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "notification-state.json"))
	manager, err := notify.NewManagerWithOptions(&config.Config{}, nil, notify.ManagerOptions{StateStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if err := manager.SaveRuntimeState(notify.RuntimeState{
		Weixin: notify.WeixinRuntimeState{
			UserID: "qr-user", AllowedUsers: []string{"qr-user"}, DefaultTarget: "qr-user",
		},
		WeComBot: notify.WeComBotRuntimeState{AllowedUsers: []string{"wecom-user"}, DefaultTarget: "wecom-user"},
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		fullCfg: &config.Config{
			Weixin:   config.WeixinConfig{Enabled: true, BaseURL: "https://ilink.example.com"},
			WeComBot: config.WeComBotConfig{Enabled: true, BotID: "bot-1"},
		},
		notifyMgr: manager,
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil)
	server.handleGetNotificationSettings(context)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response notificationSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Weixin.AllowedUserIDs, []string{"qr-user"}) {
		t.Fatalf("weixin allowed users = %#v", response.Weixin.AllowedUserIDs)
	}
	if !reflect.DeepEqual(response.WeComBot.AllowedUserIDs, []string{"wecom-user"}) {
		t.Fatalf("wecom allowed users = %#v", response.WeComBot.AllowedUserIDs)
	}
}

func TestWeComSettingsBackfillConfigFromRuntimeBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := notify.NewFileRuntimeStateStore(filepath.Join(directory, "notification-state.json"))
	manager, err := notify.NewManagerWithOptions(&config.Config{}, nil, notify.ManagerOptions{StateStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if err := manager.SaveRuntimeState(notify.RuntimeState{
		WeComBot: notify.WeComBotRuntimeState{AllowedUsers: []string{"5894387400451573601"}, DefaultTarget: "5894387400451573601"},
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		configPath: configPath,
		fullCfg:    &config.Config{WeComBot: config.WeComBotConfig{Enabled: true, BotID: "bot-1"}},
		notifyMgr:  manager,
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil)
	server.handleGetNotificationSettings(context)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(server.fullCfg.WeComBot.AllowedUserIDs, []string{"5894387400451573601"}) {
		t.Fatalf("backfilled config = %#v", server.fullCfg.WeComBot.AllowedUserIDs)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(raw), "5894387400451573601") {
		t.Fatalf("config = %s, %v", raw, err)
	}
}

func TestBuildWeixinConfigNormalizesManualSettings(t *testing.T) {
	current := config.WeixinConfig{CDNBaseURL: "https://cdn.example.com"}
	cfg, err := buildWeixinConfig(&weixinNotificationSettings{
		Enabled: true, BaseURL: " https://ilink.example.com/ ",
		AllowedUserIDs: []string{" user-1 ", "user-1"}, AllowedGroupIDs: []string{"group-1"},
	}, current)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.BaseURL != "https://ilink.example.com" || cfg.CDNBaseURL != current.CDNBaseURL ||
		!reflect.DeepEqual(cfg.AllowedUserIDs, []string{"user-1"}) ||
		!reflect.DeepEqual(cfg.AllowedGroupIDs, []string{"group-1"}) {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestBuildWeixinConfigPreservesExistingWhenFieldOmitted(t *testing.T) {
	current := config.WeixinConfig{Enabled: true, BaseURL: "https://ilink.example.com"}
	cfg, err := buildWeixinConfig(nil, current)
	if err != nil || !reflect.DeepEqual(cfg, current) {
		t.Fatalf("config = %+v, error = %v", cfg, err)
	}
}

func TestBuildWeixinConfigRejectsInsecureRemoteURL(t *testing.T) {
	_, err := buildWeixinConfig(&weixinNotificationSettings{BaseURL: "http://ilink.example.com"}, config.WeixinConfig{})
	if err == nil {
		t.Fatal("expected HTTPS validation error")
	}
}
