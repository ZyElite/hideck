package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWeixinQRServiceScansAndConfirms(t *testing.T) {
	var mu sync.Mutex
	statusCalls := 0
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("iLink-App-Id") != "bot" || r.Header.Get("iLink-App-ClientVersion") != "131584" {
			t.Errorf("iLink headers = %v", r.Header)
		}
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_ = json.NewEncoder(w).Encode(map[string]string{"qrcode": "qr-token", "qrcode_img_content": "weixin://qr"})
		case "/ilink/bot/get_qrcode_status":
			mu.Lock()
			statusCalls++
			call := statusCalls
			mu.Unlock()
			if call == 1 {
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "scaned"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "confirmed", "ilink_bot_id": "bot-1", "bot_token": "secret-token",
				"baseurl": provider.URL, "ilink_user_id": "user-1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	service := NewWeixinQRService(WeixinQROptions{HTTPClient: provider.Client()})
	started, err := service.Start(context.Background(), provider.URL)
	if err != nil || started.Status != WeixinQRWait || started.QRURL != "weixin://qr" {
		t.Fatalf("Start() = %+v, %v", started, err)
	}
	scanned, err := service.Status(context.Background(), started.SessionID)
	if err != nil || scanned.Status != WeixinQRScanned {
		t.Fatalf("first Status() = %+v, %v", scanned, err)
	}
	confirmed, err := service.Status(context.Background(), started.SessionID)
	if err != nil || confirmed.Status != WeixinQRConfirmed {
		t.Fatalf("second Status() = %+v, %v", confirmed, err)
	}
	if confirmed.Credentials.AccountID != "bot-1" || confirmed.Credentials.Token != "secret-token" {
		t.Fatalf("credentials = %+v", confirmed.Credentials)
	}
}

func TestWeixinQRServiceRefreshesAtMostThreeTimes(t *testing.T) {
	qrCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			qrCalls++
			_ = json.NewEncoder(w).Encode(map[string]string{"qrcode": fmt.Sprintf("qr-%d", qrCalls)})
		case "/ilink/bot/get_qrcode_status":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "expired"})
		}
	}))
	defer provider.Close()
	service := NewWeixinQRService(WeixinQROptions{HTTPClient: provider.Client(), Now: func() time.Time {
		return time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	}})
	view, err := service.Start(context.Background(), provider.URL)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		view, err = service.Status(context.Background(), view.SessionID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if view.Status != WeixinQRExpired || qrCalls != 4 {
		t.Fatalf("view = %+v, qrCalls = %d", view, qrCalls)
	}
}

func TestWeixinQRServiceCancelRemovesSession(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"qrcode":"qr-token"}`))
	}))
	defer provider.Close()
	service := NewWeixinQRService(WeixinQROptions{HTTPClient: provider.Client()})
	view, err := service.Start(context.Background(), provider.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(view.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Status(context.Background(), view.SessionID); !errors.Is(err, ErrWeixinQRSessionNotFound) {
		t.Fatalf("Status() error = %v", err)
	}
}

func TestNormalizeWeixinBaseURLRejectsRemoteHTTP(t *testing.T) {
	if _, err := normalizeWeixinBaseURL("http://example.com"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("normalize error = %v", err)
	}
}
