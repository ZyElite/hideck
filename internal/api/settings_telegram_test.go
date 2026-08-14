package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
)

func TestTelegramSettingsMaskTokenAndExposeRuntimeBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := newTelegramSettingsManager(t, 123456)
	server := &Server{fullCfg: &config.Config{Telegram: config.TelegramConfig{
		Enabled: true, BotToken: "private-token", AdminID: 123456,
	}}, notifyMgr: manager}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil)
	server.handleGetNotificationSettings(context)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "private-token") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response notificationSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Telegram.BotToken != notificationSecretMask || response.Telegram.BoundChatID != 123456 {
		t.Fatalf("telegram response = %+v", response.Telegram)
	}
}

func TestTelegramSettingsPreserveMaskedTokenAndAllowAutomaticBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := writeTelegramSettingsConfig(t)
	server := &Server{configPath: configPath, fullCfg: &config.Config{Telegram: config.TelegramConfig{
		Enabled: true, BotToken: "private-token", AdminID: 42,
	}}}
	body := `{"telegram":{"enabled":true,"bot_token":"********","admin_id":42,"chat_id":0}}`
	recorder := requestTelegramSettingsUpdate(server, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if server.fullCfg.Telegram.BotToken != "private-token" || server.fullCfg.Telegram.ChatID != 0 {
		t.Fatalf("telegram config = %+v", server.fullCfg.Telegram)
	}
	data, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(data), "bot_token: private-token") {
		t.Fatalf("config = %s, error = %v", data, err)
	}
}

func TestTelegramSettingsRequireAuthorizedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{configPath: writeTelegramSettingsConfig(t), fullCfg: &config.Config{}}
	body := `{"telegram":{"enabled":true,"bot_token":"new-token","admin_id":0,"chat_id":0}}`
	recorder := requestTelegramSettingsUpdate(server, body)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "管理员 ID 或通知 Chat ID") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestTelegramIdentityChangeClearsRuntimeBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := newTelegramSettingsManager(t, 42)
	server := &Server{
		configPath: writeTelegramSettingsConfig(t), notifyMgr: manager,
		fullCfg: &config.Config{Telegram: config.TelegramConfig{
			Enabled: true, BotToken: "private-token", AdminID: 42,
		}},
	}
	body := `{"telegram":{"enabled":false,"bot_token":"********","admin_id":99,"chat_id":0}}`
	recorder := requestTelegramSettingsUpdate(server, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	state, err := manager.LoadRuntimeState()
	if err != nil || state.Telegram.DefaultTarget != 0 {
		t.Fatalf("state = %+v, error = %v", state, err)
	}
}

func TestTelegramIdentityChangeRevokesOldChannelWhenReplacementFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	botAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/botold-token/getMe") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id": 1, "is_bot": true, "first_name": "old", "username": "old_bot",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "invalid token"})
	}))
	t.Cleanup(botAPI.Close)
	telegramConfig := config.TelegramConfig{
		Enabled: true, BotToken: "old-token", AdminID: 42,
		BaseURL: botAPI.URL + "/bot%s/%s",
	}
	manager := newConfiguredTelegramSettingsManager(t, telegramConfig, 42)
	server := &Server{
		configPath: writeTelegramSettingsConfig(t), notifyMgr: manager,
		fullCfg: &config.Config{Telegram: telegramConfig},
	}
	body := `{"telegram":{"enabled":true,"bot_token":"bad-token","admin_id":99,"chat_id":0,"base_url":"` +
		botAPI.URL + `/bot%s/%s"}}`
	recorder := requestTelegramSettingsUpdate(server, body)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"applied":false`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for _, name := range manager.GetChannelNames() {
		if name == "telegram" {
			t.Fatalf("old Telegram channel remained active: %v", manager.GetChannelNames())
		}
	}
	state, err := manager.LoadRuntimeState()
	if err != nil || state.Telegram.DefaultTarget != 0 {
		t.Fatalf("state = %+v, error = %v", state, err)
	}
}

func TestTelegramIdentityChangeRestoresBindingWhenConfigWriteFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := newTelegramSettingsManager(t, 42)
	server := &Server{
		configPath: t.TempDir(), notifyMgr: manager,
		fullCfg: &config.Config{Telegram: config.TelegramConfig{
			Enabled: true, BotToken: "old-token", AdminID: 42,
		}},
	}
	body := `{"telegram":{"enabled":false,"bot_token":"********","admin_id":99,"chat_id":0}}`
	recorder := requestTelegramSettingsUpdate(server, body)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	state, err := manager.LoadRuntimeState()
	if err != nil || state.Telegram.DefaultTarget != 42 {
		t.Fatalf("state = %+v, error = %v", state, err)
	}
}

func newTelegramSettingsManager(t *testing.T, target int64) *notify.Manager {
	t.Helper()
	store := notify.NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "notification-state.json"))
	manager, err := notify.NewManagerWithOptions(&config.Config{}, nil, notify.ManagerOptions{StateStore: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveRuntimeState(notify.RuntimeState{
		Telegram: notify.TelegramRuntimeState{DefaultTarget: target},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func newConfiguredTelegramSettingsManager(
	t *testing.T,
	telegramConfig config.TelegramConfig,
	target int64,
) *notify.Manager {
	t.Helper()
	store := notify.NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "notification-state.json"))
	if err := store.Save(notify.RuntimeState{
		Telegram: notify.TelegramRuntimeState{DefaultTarget: target},
	}); err != nil {
		t.Fatal(err)
	}
	manager, err := notify.NewManagerWithOptions(
		&config.Config{Telegram: telegramConfig}, nil, notify.ManagerOptions{StateStore: store},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func writeTelegramSettingsConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func requestTelegramSettingsUpdate(server *Server, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/settings/notifications", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	server.handleUpdateNotificationSettings(context)
	return recorder
}
