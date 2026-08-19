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

func TestWeixinQRHandlersPersistConfirmedCredentialsWithoutLeakingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = w.Write([]byte(`{"qrcode":"qr-token","qrcode_img_content":"weixin://scan"}`))
		case "/ilink/bot/get_qrcode_status":
			_, _ = w.Write([]byte(`{"status":"confirmed","ilink_bot_id":"bot-1","bot_token":"private-token","ilink_user_id":"user-1"}`))
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
	cfg := &config.Config{Weixin: config.WeixinConfig{BaseURL: provider.URL}}
	stateStore := notify.NewFileRuntimeStateStore(filepath.Join(directory, "notification-state.json"))
	manager, err := notify.NewManagerWithOptions(cfg, nil, notify.ManagerOptions{StateStore: stateStore})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		fullCfg: cfg, configPath: configPath, notifyMgr: manager,
		weixinQR: notify.NewWeixinQRService(notify.WeixinQROptions{HTTPClient: provider.Client()}),
	}

	startRecorder := httptest.NewRecorder()
	startContext, _ := gin.CreateTestContext(startRecorder)
	startContext.Request = httptest.NewRequest(http.MethodPost, "/api/settings/notifications/weixin/qr/start", nil)
	server.handleStartWeixinQR(startContext)
	var startResponse weixinQRResponse
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &startResponse); err != nil {
		t.Fatal(err)
	}
	statusRecorder := successfulQRStatus(t, requestQRStatusConcurrently(
		"/api/settings/notifications/weixin/qr/status?session_id="+startResponse.SessionID,
		server.handleWeixinQRStatus,
	))
	if statusRecorder.Code != http.StatusOK || strings.Contains(statusRecorder.Body.String(), "private-token") {
		t.Fatalf("status = %d, body = %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	var statusResponse weixinQRResponse
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &statusResponse); err != nil {
		t.Fatal(err)
	}
	if statusResponse.Status != notify.WeixinQRConfirmed || !statusResponse.Applied || statusResponse.BotAccountID != "bot-1" {
		t.Fatalf("status response = %+v", statusResponse)
	}
	state, err := stateStore.Load()
	if err != nil || state.Weixin.Token != "private-token" || state.Weixin.AccountID != "bot-1" ||
		state.Weixin.UserID != "user-1" || state.Weixin.DefaultTarget != "user-1" ||
		len(state.Weixin.AllowedUsers) != 1 || state.Weixin.AllowedUsers[0] != "user-1" {
		t.Fatalf("state = %+v, %v", state, err)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(configData), "enabled: true") {
		t.Fatalf("config = %s, %v", configData, err)
	}
}

func TestCancelWeixinQRHandlerReturnsNotFound(t *testing.T) {
	server := &Server{weixinQR: notify.NewWeixinQRService(notify.WeixinQROptions{})}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"session_id":"missing"}`))
	context.Request.Header.Set("Content-Type", "application/json")
	server.handleCancelWeixinQR(context)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
