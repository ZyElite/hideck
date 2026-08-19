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

func TestFeishuQRHandlersPersistConfirmedCredentialsWithoutLeakingSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.PostForm.Get("action") {
		case "init":
			_, _ = w.Write([]byte(`{"supported_auth_methods":["client_secret"]}`))
		case "begin":
			_, _ = w.Write([]byte(`{"device_code":"device-1","verification_uri_complete":"https://accounts.feishu.cn/verify"}`))
		case "poll":
			_, _ = w.Write([]byte(`{"client_id":"cli_app","client_secret":"private-secret"}`))
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
	cfg := &config.Config{}
	manager, err := notify.NewManagerWithOptions(cfg, nil, notify.ManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		fullCfg: cfg, configPath: configPath, notifyMgr: manager,
		feishuQR: notify.NewFeishuQRService(notify.FeishuQROptions{HTTPClient: provider.Client(), AccountsURL: provider.URL}),
	}

	startRecorder := httptest.NewRecorder()
	startContext, _ := gin.CreateTestContext(startRecorder)
	startContext.Request = httptest.NewRequest(http.MethodPost, "/api/settings/notifications/feishu/qr/start", nil)
	server.handleStartFeishuQR(startContext)
	var started feishuQRResponse
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	statusRecorder := successfulQRStatus(t, requestQRStatusConcurrently(
		"/api/settings/notifications/feishu/qr/status?session_id="+started.SessionID,
		server.handleFeishuQRStatus,
	))
	if statusRecorder.Code != http.StatusOK || strings.Contains(statusRecorder.Body.String(), "private-secret") {
		t.Fatalf("status = %d, body = %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	var statusResponse feishuQRResponse
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &statusResponse); err != nil {
		t.Fatal(err)
	}
	if statusResponse.Status != notify.FeishuQRConfirmed || !statusResponse.Applied || statusResponse.AppID != "cli_app" {
		t.Fatalf("status response = %+v", statusResponse)
	}
	if cfg.Feishu.AppID != "cli_app" || cfg.Feishu.AppSecret != "private-secret" || !cfg.Feishu.Enabled {
		t.Fatalf("config = %+v", cfg.Feishu)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil || !strings.Contains(string(configData), "cli_app") {
		t.Fatalf("config file = %s, %v", configData, err)
	}
}
