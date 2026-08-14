package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQQQRServiceCompletesAndDecryptsCredentials(t *testing.T) {
	var mu sync.Mutex
	var key []byte
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case qqQRCreatePath:
			var request struct {
				Key string `json:"key"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			decoded, err := base64.StdEncoding.DecodeString(request.Key)
			if err != nil {
				t.Errorf("decode key: %v", err)
				return
			}
			mu.Lock()
			key = append([]byte(nil), decoded...)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"task_id":"task-1"}}`))
		case qqQRPollPath:
			mu.Lock()
			currentKey := append([]byte(nil), key...)
			mu.Unlock()
			encrypted := encryptQQQRSecretForTest(t, "client-secret", currentKey)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"retcode": 0, "data": map[string]any{
					"status": 2, "bot_appid": "app-1", "bot_encrypt_secret": encrypted, "user_openid": "openid-1",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service := NewQQQRService(QQQROptions{HTTPClient: provider.Client(), BaseURL: provider.URL, QRBaseURL: provider.URL})

	started, err := service.Start(context.Background())
	if err != nil || started.Status != QQQRWait || started.TaskID != "task-1" {
		t.Fatalf("Start() = %+v, %v", started, err)
	}
	parsed, err := url.Parse(started.QRURL)
	if err != nil || parsed.Query().Get("source") != "vohive" || parsed.Query().Get("task_id") != "task-1" {
		t.Fatalf("QR URL = %q, %v", started.QRURL, err)
	}
	mu.Lock()
	keyLength := len(key)
	mu.Unlock()
	if keyLength != 32 {
		t.Fatalf("AES key length = %d", keyLength)
	}
	confirmed, err := service.Status(context.Background(), started.SessionID)
	if err != nil || confirmed.Status != QQQRConfirmed {
		t.Fatalf("Status() = %+v, %v", confirmed, err)
	}
	if confirmed.Credentials.AppID != "app-1" || confirmed.Credentials.ClientSecret != "client-secret" ||
		confirmed.Credentials.UserOpenID != "openid-1" {
		t.Fatalf("credentials = %+v", confirmed.Credentials)
	}
	repeated, err := service.Status(context.Background(), started.SessionID)
	if err != nil || repeated.Status != QQQRConfirmed || repeated.Credentials.ClientSecret != "client-secret" {
		t.Fatalf("repeated confirmed Status() = %+v, %v", repeated, err)
	}
	if err := service.MarkApplied(started.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	applied, err := service.Status(context.Background(), started.SessionID)
	if err != nil || !applied.Applied || applied.Credentials.ClientSecret != "" {
		t.Fatalf("applied = %+v, %v", applied, err)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrQQQRSessionNotFound) {
		t.Fatalf("terminal Status() error = %v", err)
	}
}

func TestQQQRServiceRefreshesAtMostThreeTimes(t *testing.T) {
	var mu sync.Mutex
	createCalls := 0
	now := time.Unix(100, 0)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case qqQRCreatePath:
			mu.Lock()
			createCalls++
			taskID := createCalls
			mu.Unlock()
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"task_id":"task-` + strconv.Itoa(taskID) + `"}}`))
		case qqQRPollPath:
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"status":3}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	service := NewQQQRService(QQQROptions{
		HTTPClient: provider.Client(), BaseURL: provider.URL, QRBaseURL: provider.URL, Now: func() time.Time { return now },
	})
	started, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var view QQQRView
	for index := range qqQRMaxRefreshes + 1 {
		if index == 0 {
			now = now.Add(qqQRSessionTTL / 2)
		}
		view, err = service.Status(context.Background(), started.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 && !view.ExpiresAt.Equal(now.Add(qqQRSessionTTL)) {
			t.Fatalf("refreshed expiry = %s", view.ExpiresAt)
		}
	}
	mu.Lock()
	calls := createCalls
	mu.Unlock()
	if view.Status != QQQRExpired || calls != qqQRMaxRefreshes+1 {
		t.Fatalf("view = %+v, create calls = %d", view, calls)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrQQQRSessionNotFound) {
		t.Fatalf("expired Status() error = %v", err)
	}
}

func TestQQQRServiceCancelRemovesSession(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"retcode":0,"data":{"task_id":"task-1"}}`))
	}))
	defer provider.Close()
	service := NewQQQRService(QQQROptions{HTTPClient: provider.Client(), BaseURL: provider.URL, QRBaseURL: provider.URL})
	started, err := service.Start(context.Background())
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
	if len(session.key) != 0 {
		t.Fatalf("cancelled AES key length = %d", len(session.key))
	}
	session.mu.Unlock()
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrQQQRSessionNotFound) {
		t.Fatalf("Status() error = %v", err)
	}
}

func TestDecryptQQQRSecretRejectsTamperedCiphertext(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	encrypted := encryptQQQRSecretForTest(t, "secret", key)
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	_, err = decryptQQQRSecret(base64.StdEncoding.EncodeToString(raw), key)
	if err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("error = %v", err)
	}
}

func encryptQQQRSecretForTest(t *testing.T, secret string, key []byte) string {
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
	sealed := gcm.Seal(nil, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

func TestQQQRServiceExpiresByDeadline(t *testing.T) {
	now := time.Unix(100, 0)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"retcode":0,"data":{"task_id":"task-1"}}`))
	}))
	defer provider.Close()
	service := NewQQQRService(QQQROptions{
		HTTPClient: provider.Client(), BaseURL: provider.URL, QRBaseURL: provider.URL, Now: func() time.Time { return now },
	})
	started, err := service.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(qqQRSessionTTL)
	view, err := service.Status(context.Background(), started.SessionID)
	if err != nil || view.Status != QQQRExpired {
		t.Fatalf("Status() = %+v, %v", view, err)
	}
	if _, err := service.Status(context.Background(), started.SessionID); !errors.Is(err, ErrQQQRSessionNotFound) {
		t.Fatalf("expired Status() error = %v", err)
	}
}
