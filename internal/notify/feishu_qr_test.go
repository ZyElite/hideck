package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFeishuQRServiceConfirmsHermesCompatibleScan(t *testing.T) {
	var actions []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != feishuQRRegistrationPath {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		action := form.Get("action")
		actions = append(actions, action)
		switch action {
		case "init":
			_ = json.NewEncoder(w).Encode(map[string]any{"supported_auth_methods": []string{"client_secret"}})
		case "begin":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "device-1", "verification_uri_complete": "https://accounts.feishu.cn/verify",
				"expire_in": 300,
			})
		case "poll":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"client_id": "cli_app", "client_secret": "private-secret",
				"user_info": map[string]string{"open_id": "ou_user"},
			})
		default:
			http.Error(w, "bad action", http.StatusBadRequest)
		}
	}))
	defer provider.Close()
	service := NewFeishuQRService(FeishuQROptions{HTTPClient: provider.Client(), AccountsURL: provider.URL})

	started, err := service.Start(context.Background())
	if err != nil || started.Status != FeishuQRWait || !strings.Contains(started.QRURL, "from=hideck") {
		t.Fatalf("Start() = %+v, %v", started, err)
	}
	confirmed, err := service.Status(context.Background(), started.SessionID)
	if err != nil || confirmed.Status != FeishuQRConfirmed {
		t.Fatalf("Status() = %+v, %v", confirmed, err)
	}
	if confirmed.Credentials.AppID != "cli_app" || confirmed.Credentials.AppSecret != "private-secret" ||
		confirmed.Credentials.OpenID != "ou_user" {
		t.Fatalf("credentials = %+v", confirmed.Credentials)
	}
	if strings.Join(actions, ",") != "init,begin,poll" {
		t.Fatalf("actions = %v", actions)
	}
	if err := service.MarkApplied(started.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	applied, err := service.Status(context.Background(), started.SessionID)
	if err != nil || !applied.Applied || applied.Credentials.AppSecret != "" {
		t.Fatalf("applied = %+v, %v", applied, err)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrFeishuQRSessionNotFound) {
		t.Fatalf("terminal Status() error = %v", err)
	}
}

func TestFeishuQRServiceKeepsWaitingWhileAuthorizationPending(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		switch form.Get("action") {
		case "init":
			_ = json.NewEncoder(w).Encode(map[string]any{"supported_auth_methods": []string{"client_secret"}})
		case "begin":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "device-1", "verification_uri_complete": "https://accounts.feishu.cn/verify",
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
		}
	}))
	defer provider.Close()
	service := NewFeishuQRService(FeishuQROptions{HTTPClient: provider.Client(), AccountsURL: provider.URL})
	started, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Status(context.Background(), started.SessionID)
	if err != nil || view.Status != FeishuQRWait {
		t.Fatalf("pending Status() = %+v, %v", view, err)
	}
}
