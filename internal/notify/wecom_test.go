package notify

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yibaiba/hideck/internal/config"
)

func TestRenderWeComPayloadEscapesTemplateValues(t *testing.T) {
	payload, err := renderWeComPayload(
		`{"msgtype":"text","text":{"content":{{message}},"number":{{number}}}}`,
		NotificationContext{
			Text:   "quote: \"\nline",
			Number: "+447386",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"msgtype":"text","text":{"content":"quote: \"\nline","number":"+447386"}}`
	if got := string(payload); got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestRenderWeComPayloadRejectsInvalidTemplate(t *testing.T) {
	templates := []string{
		`{"text":{{unknown}}}`,
		`[]`,
		`{"msgtype":"text"`,
		`{"text":"{{message}}"}`,
	}
	for _, template := range templates {
		t.Run(template, func(t *testing.T) {
			if _, err := renderWeComPayload(template, NotificationContext{}); err == nil {
				t.Fatalf("template %q was accepted", template)
			}
		})
	}
}

func TestValidateWeComResponse(t *testing.T) {
	if err := validateWeComResponse(http.StatusOK, []byte(`{"errcode":0,"errmsg":"ok"}`)); err != nil {
		t.Fatalf("successful response = %v", err)
	}
	tests := []struct {
		status int
		body   string
	}{
		{http.StatusBadGateway, `{"errcode":0}`},
		{http.StatusOK, `{"errcode":40058,"errmsg":"invalid"}`},
		{http.StatusOK, `{}`},
		{http.StatusOK, `not-json`},
	}
	for _, test := range tests {
		if err := validateWeComResponse(test.status, []byte(test.body)); err == nil {
			t.Fatalf("validateWeComResponse(%d, %s) returned nil", test.status, test.body)
		}
	}
}

func TestWeComChannelSendsContextToEveryDestination(t *testing.T) {
	var calls atomic.Int32
	receivedPayloads := make(chan map[string]any, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var received map[string]any
		if err := json.Unmarshal(body, &received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		receivedPayloads <- received
		calls.Add(1)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	})
	first := httptest.NewServer(handler)
	defer first.Close()
	second := httptest.NewServer(handler)
	defer second.Close()

	channel, err := NewWeComChannel(config.WeComConfig{
		Enabled: true,
		URLs:    []string{first.URL, second.URL},
		PayloadTemplate: `{
  "msgtype":"text",
  "text":{"message":{{message}},"number":{{number}},"content":{{content}},"device":{{device_label}}}
}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = channel.SendWithContext(NotificationContext{
		Event:      "sms_received",
		Text:       "收到新短信",
		DeviceID:   "wwan0",
		DeviceName: "主卡",
		Number:     "10086",
		Content:    "余额提醒",
		Timestamp:  time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	received := <-receivedPayloads
	text, ok := received["text"].(map[string]any)
	if !ok {
		t.Fatalf("text payload = %#v", received["text"])
	}
	if text["message"] != "收到新短信" || text["number"] != "10086" ||
		text["content"] != "余额提醒" || text["device"] != "主卡 (wwan0)" {
		t.Fatalf("text payload = %#v", text)
	}
}

func TestWeComChannelRejectsProviderErrorWithoutLeakingURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40058,"errmsg":"invalid"}`))
	}))
	defer server.Close()

	const secret = "top-secret-key"
	channel, err := NewWeComChannel(config.WeComConfig{
		Enabled:         true,
		URLs:            []string{server.URL + "/send?key=" + secret},
		PayloadTemplate: config.DefaultWeComPayloadTemplate,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = channel.Send("test")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error leaks destination: %v", err)
	}
}

func TestNewWeComChannelValidatesConfiguration(t *testing.T) {
	tests := []config.WeComConfig{
		{Enabled: true, PayloadTemplate: config.DefaultWeComPayloadTemplate},
		{Enabled: true, URLs: []string{"ftp://example.com/hook"}, PayloadTemplate: config.DefaultWeComPayloadTemplate},
		{Enabled: true, URLs: []string{"https://example.com/hook"}, PayloadTemplate: `[]`},
	}
	for _, cfg := range tests {
		if _, err := NewWeComChannel(cfg); err == nil {
			t.Fatalf("configuration was accepted: %#v", cfg)
		}
	}
}

func TestRedactWeComTransportErrorRemovesRequestURL(t *testing.T) {
	inner := errors.New("dial tcp: connection refused")
	err := redactWeComTransportError(&url.Error{
		Op:  http.MethodPost,
		URL: "https://example.com/send?key=top-secret",
		Err: inner,
	})
	if !errors.Is(err, inner) {
		t.Fatalf("wrapped error = %v", err)
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("error leaks URL: %v", err)
	}
}
