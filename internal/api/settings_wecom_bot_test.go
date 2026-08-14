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

func TestWeComQRHandlersPersistCredentialsWithoutLeakingSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate":
			_, _ = w.Write([]byte(`{"data":{"scode":"scan-code","auth_url":"` + provider.URL + `/scan"}}`))
		case "/query":
			_, _ = w.Write([]byte(`{"data":{"status":"success","bot_info":{"botid":"bot-1","secret":"private-secret"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{WeComBot: config.WeComBotConfig{WebSocketURL: defaultWeComBotWebSocketURL}}
	server := &Server{
		fullCfg: cfg, configPath: configPath,
		wecomQR: notify.NewWeComQRService(notify.WeComQROptions{
			HTTPClient: provider.Client(), GenerateURL: provider.URL + "/generate",
			QueryURL: provider.URL + "/query", PageURL: provider.URL + "/open?scode=",
		}),
	}

	startRecorder := httptest.NewRecorder()
	startContext, _ := gin.CreateTestContext(startRecorder)
	startContext.Request = httptest.NewRequest(http.MethodPost, "/api/settings/notifications/wecom-bot/qr/start", nil)
	server.handleStartWeComQR(startContext)
	var started weComQRResponse
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	statusRecorder := httptest.NewRecorder()
	statusContext, _ := gin.CreateTestContext(statusRecorder)
	statusContext.Request = httptest.NewRequest(http.MethodGet, "/api/settings/notifications/wecom-bot/qr/status?session_id="+started.SessionID, nil)
	server.handleWeComQRStatus(statusContext)
	if statusRecorder.Code != http.StatusOK || strings.Contains(statusRecorder.Body.String(), "private-secret") {
		t.Fatalf("status = %d, body = %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	var status weComQRResponse
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Applied || status.BotID != "bot-1" || !server.fullCfg.WeComBot.Enabled {
		t.Fatalf("status = %+v, config = %+v", status, server.fullCfg.WeComBot)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(raw), "private-secret") {
		t.Fatalf("config = %s, %v", raw, err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v", info.Mode().Perm())
	}
}

func TestStartWeComQRHandlerExposesManualFallbackOnProviderFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer provider.Close()
	server := &Server{wecomQR: notify.NewWeComQRService(notify.WeComQROptions{
		HTTPClient: provider.Client(), GenerateURL: provider.URL, QueryURL: provider.URL,
	})}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/settings/notifications/wecom-bot/qr/start", nil)
	server.handleStartWeComQR(context)
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"manual_setup_available":true`) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestWeComBotSettingsMaskSecretAndPreserveWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const webhookURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{configPath: configPath, fullCfg: &config.Config{
		WeComBot: config.WeComBotConfig{
			Enabled: true, BotID: "bot-1", Secret: "bot-secret", WebSocketURL: defaultWeComBotWebSocketURL,
		},
		WeCom: config.WeComConfig{Enabled: true, URLs: []string{webhookURL}, PayloadTemplate: config.DefaultWeComPayloadTemplate},
	}}

	getRecorder := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getRecorder)
	getContext.Request = httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil)
	server.handleGetNotificationSettings(getContext)
	if strings.Contains(getRecorder.Body.String(), "bot-secret") || !strings.Contains(getRecorder.Body.String(), notificationSecretMask) {
		t.Fatalf("GET body = %s", getRecorder.Body.String())
	}
	body := `{"wecom_bot":{"enabled":true,"bot_id":"bot-1","secret":"********","websocket_url":"wss://openws.work.weixin.qq.com","allowed_user_ids":[" user-1 ","user-1"],"allowed_group_ids":["group-1"]}}`
	putRecorder := httptest.NewRecorder()
	putContext, _ := gin.CreateTestContext(putRecorder)
	putContext.Request = httptest.NewRequest(http.MethodPut, "/api/settings/notifications", strings.NewReader(body))
	putContext.Request.Header.Set("Content-Type", "application/json")
	server.handleUpdateNotificationSettings(putContext)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", putRecorder.Code, putRecorder.Body.String())
	}
	if server.fullCfg.WeComBot.Secret != "bot-secret" || len(server.fullCfg.WeComBot.AllowedUserIDs) != 1 {
		t.Fatalf("wecom bot config = %+v", server.fullCfg.WeComBot)
	}
	if len(server.fullCfg.WeCom.URLs) != 1 || server.fullCfg.WeCom.URLs[0] != webhookURL {
		t.Fatalf("webhook config = %+v", server.fullCfg.WeCom)
	}
}

func TestBuildWeComBotConfigRejectsInsecureRemoteWebSocket(t *testing.T) {
	_, err := buildWeComBotConfig(&weComBotNotificationSettings{
		Enabled: true, BotID: "bot", Secret: "secret", WebSocketURL: "ws://example.com/socket",
	}, config.WeComBotConfig{})
	if err == nil || !strings.Contains(err.Error(), "WSS") {
		t.Fatalf("error = %v", err)
	}
}
