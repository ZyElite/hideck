package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWeComQRServiceConfirmsHermesCompatibleScan(t *testing.T) {
	var generateSource string
	var querySCode string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate":
			generateSource = r.URL.Query().Get("source")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"scode": "scan-code", "auth_url": "http://127.0.0.1/scan"},
			})
		case "/query":
			querySCode = r.URL.Query().Get("scode")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"status": "success", "bot_info": map[string]any{"botid": "bot-1", "secret": "private-secret"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service := NewWeComQRService(WeComQROptions{
		HTTPClient: provider.Client(), GenerateURL: provider.URL + "/generate",
		QueryURL: provider.URL + "/query", PageURL: provider.URL + "/open?scode=",
	})

	started, err := service.Start(context.Background())
	if err != nil || started.Status != WeComQRWait || started.QRURL == "" {
		t.Fatalf("Start() = %+v, %v", started, err)
	}
	confirmed, err := service.Status(context.Background(), started.SessionID)
	if err != nil || confirmed.Status != WeComQRConfirmed {
		t.Fatalf("Status() = %+v, %v", confirmed, err)
	}
	if generateSource != "hermes" || querySCode != "scan-code" {
		t.Fatalf("source = %q, scode = %q", generateSource, querySCode)
	}
	if confirmed.Credentials.BotID != "bot-1" || confirmed.Credentials.Secret != "private-secret" {
		t.Fatalf("credentials = %+v", confirmed.Credentials)
	}
	repeated, err := service.Status(context.Background(), started.SessionID)
	if err != nil || repeated.Status != WeComQRConfirmed || repeated.Credentials.Secret != "private-secret" {
		t.Fatalf("repeated confirmed Status() = %+v, %v", repeated, err)
	}
	if err := service.MarkApplied(started.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	applied, err := service.Status(context.Background(), started.SessionID)
	if err != nil || !applied.Applied || applied.Credentials.Secret != "" {
		t.Fatalf("applied = %+v, %v", applied, err)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrWeComQRSessionNotFound) {
		t.Fatalf("terminal Status() error = %v", err)
	}
}

func TestWeComQRServiceReportsIncompleteCredentials(t *testing.T) {
	provider := newWeComQRTestServer(t, `{"data":{"status":"success","bot_info":{"botid":"bot-1"}}}`)
	defer provider.Close()
	service := newWeComQRTestService(provider)
	started, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Status(context.Background(), started.SessionID)
	if err != nil || view.Status != WeComQRError || !strings.Contains(view.Error, "手工填写") {
		t.Fatalf("Status() = %+v, %v", view, err)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrWeComQRSessionNotFound) {
		t.Fatalf("error Status() = %v", err)
	}
}

func TestWeComQRServiceExpiresAndCancelsSessions(t *testing.T) {
	now := time.Unix(100, 0)
	provider := newWeComQRTestServer(t, `{"data":{"status":"pending"}}`)
	defer provider.Close()
	service := newWeComQRTestServiceWithNow(provider, func() time.Time { return now })
	started, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(weComQRSessionTTL)
	view, err := service.Status(context.Background(), started.SessionID)
	if err != nil || view.Status != WeComQRExpired {
		t.Fatalf("Status() = %+v, %v", view, err)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrWeComQRSessionNotFound) {
		t.Fatalf("expired Status() error = %v", err)
	}

	started, err = service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.session(started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(started.SessionID); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	if session.scode != "" {
		t.Fatalf("cancelled scode = %q", session.scode)
	}
	session.mu.Unlock()
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrWeComQRSessionNotFound) {
		t.Fatalf("Status() error = %v", err)
	}
}

func newWeComQRTestServer(t *testing.T, queryResponse string) *httptest.Server {
	t.Helper()
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate":
			_, _ = w.Write([]byte(`{"data":{"scode":"scan-code","auth_url":"` + provider.URL + `/scan"}}`))
		case "/query":
			_, _ = w.Write([]byte(queryResponse))
		default:
			http.NotFound(w, r)
		}
	}))
	return provider
}

func newWeComQRTestService(provider *httptest.Server) *WeComQRService {
	return newWeComQRTestServiceWithNow(provider, nil)
}

func newWeComQRTestServiceWithNow(provider *httptest.Server, now func() time.Time) *WeComQRService {
	return NewWeComQRService(WeComQROptions{
		HTTPClient: provider.Client(), GenerateURL: provider.URL + "/generate",
		QueryURL: provider.URL + "/query", PageURL: provider.URL + "/open?scode=", Now: now,
	})
}
