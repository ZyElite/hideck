package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/notify"
)

func TestConcurrentQRAndManualNotificationUpdatesPreserveBothChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{WeComBot: config.WeComBotConfig{WebSocketURL: defaultWeComBotWebSocketURL}}
	server := &Server{configPath: configPath, fullCfg: cfg}
	start := make(chan struct{})
	warning := make(chan string, 1)
	response := make(chan *httptest.ResponseRecorder, 1)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		warning <- server.applyWeComQRCredentials(notify.WeComQRCredentials{BotID: "wecom-bot", Secret: "wecom-secret"})
	}()
	go func() {
		defer wait.Done()
		<-start
		response <- requestConcurrentQQSettings(server)
	}()
	close(start)
	wait.Wait()

	if result := <-warning; result != "" {
		t.Fatalf("QR apply warning = %q", result)
	}
	if recorder := <-response; recorder.Code != http.StatusOK {
		t.Fatalf("manual save = %d, %s", recorder.Code, recorder.Body.String())
	}
	assertConcurrentNotificationConfig(t, configPath, cfg)
}

func requestConcurrentQQSettings(server *Server) *httptest.ResponseRecorder {
	body := `{"qq":{"enabled":true,"app_id":"qq-app","app_secret":"qq-secret","direct_ids":"qq-user"}}`
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/settings/notifications", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	server.handleUpdateNotificationSettings(context)
	return recorder
}

func assertConcurrentNotificationConfig(t *testing.T, path string, cfg *config.Config) {
	t.Helper()
	if cfg.QQ.AppID != "qq-app" || cfg.WeComBot.BotID != "wecom-bot" {
		t.Fatalf("runtime config: QQ=%+v WeComBot=%+v", cfg.QQ, cfg.WeComBot)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"app_id: qq-app", "bot_id: wecom-bot", "secret: wecom-secret"} {
		if !strings.Contains(string(data), value) {
			t.Fatalf("config missing %q: %s", value, data)
		}
	}
}
