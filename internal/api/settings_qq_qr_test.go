package api

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
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

func TestQQQRHandlersApplyCredentialsWithoutLeakingSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := newQQQRProvider(t)
	defer provider.Close()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, []byte("server:\n  port: 7575\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	stateStore := notify.NewFileRuntimeStateStore(filepath.Join(directory, "notification-state.json"))
	welcome := &qqRegistrationHelpCapture{}
	manager, err := notify.NewManagerWithOptions(cfg, nil, notify.ManagerOptions{
		StateStore: stateStore,
		QQChannelFactory: func(config.QQConfig) (notify.Channel, error) {
			return welcome, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	server := &Server{
		fullCfg: cfg, configPath: configPath, notifyMgr: manager,
		qqQR: notify.NewQQQRService(notify.QQQROptions{
			HTTPClient: provider.Client(), BaseURL: provider.URL, QRBaseURL: provider.URL,
		}),
	}

	start := requestQQQRStart(t, server)
	statusRecorder := successfulQRStatus(t, requestQRStatusConcurrently(
		"/api/settings/notifications/qq/qr/status?session_id="+start.SessionID,
		server.handleQQQRStatus,
	))
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	responseBody := statusRecorder.Body.String()
	for _, sensitive := range []string{"client-secret", "bot_encrypt_secret", `"key"`} {
		if strings.Contains(responseBody, sensitive) {
			t.Fatalf("response leaked %q: %s", sensitive, responseBody)
		}
	}
	var response qqQRResponse
	if err := json.Unmarshal(statusRecorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != notify.QQQRConfirmed || !response.Applied ||
		response.AppID != "app-1" || response.UserOpenID != "openid-1" {
		t.Fatalf("status response = %+v", response)
	}
	assertQQQRState(t, stateStore)
	assertQQQRConfig(t, configPath, cfg)
	if welcome.target != "openid-1" || !strings.Contains(welcome.message, "/help") ||
		!strings.Contains(welcome.message, "当前没有已配置设备") {
		t.Fatalf("registration help target=%q message=%q", welcome.target, welcome.message)
	}
}

type qqRegistrationHelpCapture struct {
	target  string
	message string
}

func (c *qqRegistrationHelpCapture) Name() string                                  { return "qq" }
func (c *qqRegistrationHelpCapture) Send(string) error                             { return nil }
func (c *qqRegistrationHelpCapture) RegisterCommand(string, notify.CommandHandler) {}
func (c *qqRegistrationHelpCapture) Start() error                                  { return nil }
func (c *qqRegistrationHelpCapture) Close() error                                  { return nil }
func (c *qqRegistrationHelpCapture) SendRegistrationHelp(target, message string) error {
	c.target, c.message = target, message
	return nil
}

func newQQQRProvider(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	var key []byte
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lite/create_bind_task":
			var request struct {
				Key string `json:"key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode create request: %v", err)
				return
			}
			decoded, err := base64.StdEncoding.DecodeString(request.Key)
			if err != nil {
				t.Errorf("decode create key: %v", err)
				return
			}
			mu.Lock()
			key = append([]byte(nil), decoded...)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"task_id":"task-1"}}`))
		case "/lite/poll_bind_result":
			mu.Lock()
			currentKey := append([]byte(nil), key...)
			mu.Unlock()
			secret := encryptQQQRAPISecret(t, "client-secret", currentKey)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retcode": 0,
				"data": map[string]any{
					"status": 2, "bot_appid": "app-1",
					"bot_encrypt_secret": secret, "user_openid": "openid-1",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func requestQQQRStart(t *testing.T, server *Server) qqQRResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/api/settings/notifications/qq/qr/start", nil)
	server.handleStartQQQR(requestContext)
	if recorder.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response qqQRResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func encryptQQQRAPISecret(t *testing.T, secret string, key []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := []byte("0123456789ab")
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, []byte(secret), nil)...))
}

func assertQQQRState(t *testing.T, store notify.RuntimeStateStore) {
	t.Helper()
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.QQ.AdminOpenID != "openid-1" || state.QQ.DefaultTarget != "openid-1" ||
		len(state.QQ.AllowedDirect) != 1 || state.QQ.AllowedDirect[0] != "openid-1" {
		t.Fatalf("QQ runtime state = %+v", state.QQ)
	}
}

func assertQQQRConfig(t *testing.T, path string, cfg *config.Config) {
	t.Helper()
	if !cfg.QQ.Enabled || cfg.QQ.AppID != "app-1" || cfg.QQ.AppSecret != "client-secret" ||
		cfg.QQ.DirectIDs != "openid-1" {
		t.Fatalf("QQ config = %+v", cfg.QQ)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"enabled: true", "app_id: app-1", "app_secret: client-secret", "direct_ids: openid-1"} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("config missing %q: %s", expected, raw)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v", info.Mode().Perm())
	}
}

func TestCancelQQQRHandlerReturnsNotFound(t *testing.T) {
	server := &Server{qqQR: notify.NewQQQRService(notify.QQQROptions{})}
	recorder := httptest.NewRecorder()
	requestContext, _ := gin.CreateTestContext(recorder)
	requestContext.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"session_id":"missing"}`))
	requestContext.Request.Header.Set("Content-Type", "application/json")
	server.handleCancelQQQR(requestContext)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
