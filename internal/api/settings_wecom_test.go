package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
)

func TestGetNotificationSettingsMasksWeComURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secretURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret"
	server := &Server{fullCfg: &config.Config{WeCom: config.WeComConfig{
		Enabled:         true,
		URLs:            []string{secretURL},
		PayloadTemplate: config.DefaultWeComPayloadTemplate,
	}}}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/settings/notifications", nil)
	server.handleGetNotificationSettings(context)

	if strings.Contains(recorder.Body.String(), secretURL) || strings.Contains(recorder.Body.String(), "key=secret") {
		t.Fatalf("response leaks WeCom URL: %s", recorder.Body.String())
	}
	var response notificationSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.WeCom.URLs) != 1 || response.WeCom.URLs[0] != notificationSecretMask {
		t.Fatalf("masked URLs = %#v", response.WeCom.URLs)
	}
}

func TestUpdateNotificationSettingsPreservesMaskedWeComURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secretURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=secret"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		configPath: configPath,
		fullCfg: &config.Config{WeCom: config.WeComConfig{
			Enabled:         true,
			URLs:            []string{secretURL},
			PayloadTemplate: config.DefaultWeComPayloadTemplate,
		}},
	}
	body := `{"wecom":{"enabled":true,"urls":["********"],"payload_template":` + quoteJSON(t, config.DefaultWeComPayloadTemplate) + `}}`
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/settings/notifications", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	server.handleUpdateNotificationSettings(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(server.fullCfg.WeCom.URLs) != 1 || server.fullCfg.WeCom.URLs[0] != secretURL {
		t.Fatalf("runtime URLs = %#v", server.fullCfg.WeCom.URLs)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "key=secret") {
		t.Fatalf("persisted config did not preserve URL: %s", raw)
	}
}

func TestWeComNotificationTestUsesStoredMaskedURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer provider.Close()
	secretURL := provider.URL + "/send?key=test-secret"
	server := &Server{fullCfg: &config.Config{WeCom: config.WeComConfig{
		Enabled:         true,
		URLs:            []string{secretURL},
		PayloadTemplate: config.DefaultWeComPayloadTemplate,
	}}}
	body := `{"enabled":true,"urls":["********"],"payload_template":` + quoteJSON(t, config.DefaultWeComPayloadTemplate) + `}`
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/settings/notifications/wecom/test", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	server.handleTestWeComNotification(context)

	if recorder.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d, calls = %d, body = %s", recorder.Code, calls, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "test-secret") {
		t.Fatalf("response leaks WeCom URL: %s", recorder.Body.String())
	}
}

func TestResolveWeComURLsRejectsOrphanMask(t *testing.T) {
	if _, err := resolveWeComURLs([]string{notificationSecretMask}, nil); err == nil {
		t.Fatal("orphan secret mask was accepted")
	}
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
