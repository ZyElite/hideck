package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

type apiVoiceCall struct{ id string }

func (c apiVoiceCall) CallID() string            { return c.id }
func (c apiVoiceCall) MediaErrors() <-chan error { return nil }

type apiVoiceAgent struct {
	callee      string
	hangupID    string
	dialCalls   int
	hangupCalls int
}

func (a *apiVoiceAgent) DialContext(_ context.Context, callee string) (interface{}, error) {
	a.callee = callee
	a.dialCalls++
	return apiVoiceCall{id: "api-call-1"}, nil
}

func (a *apiVoiceAgent) HangupContext(_ context.Context, callID string) error {
	a.hangupID = callID
	a.hangupCalls++
	return nil
}

func (a *apiVoiceAgent) Ready() bool  { return true }
func (a *apiVoiceAgent) Start() error { return nil }
func (a *apiVoiceAgent) Stop() error  { return nil }

func TestDeviceVoWiFiCallRequiresAuthentication(t *testing.T) {
	router, _ := newVoiceCallTestRouter(t)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/wwan0/vowifi/calls", strings.NewReader(`{"callee":"+447942985429","hold_seconds":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s, want 401", recorder.Code, recorder.Body.String())
	}
}

func TestDeviceVoWiFiCallUsesRegisteredGatewayAndHangsUp(t *testing.T) {
	router, agent := newVoiceCallTestRouter(t)
	recorder := httptest.NewRecorder()
	req := authenticatedVoiceCallRequest(t, `{"callee":"+447942985429","hold_seconds":1}`)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Success    bool  `json:"success"`
		DurationMs int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.DurationMs != 1000 {
		t.Fatalf("response=%+v", body)
	}
	if agent.callee != "+447942985429" || agent.dialCalls != 1 || agent.hangupCalls != 1 || agent.hangupID != "api-call-1" {
		t.Fatalf("agent=%+v", agent)
	}
}

func TestDeviceVoWiFiCallRejectsExcessiveHoldBeforeDial(t *testing.T) {
	router, agent := newVoiceCallTestRouter(t)
	recorder := httptest.NewRecorder()
	req := authenticatedVoiceCallRequest(t, `{"callee":"+447942985429","hold_seconds":61}`)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || agent.dialCalls != 0 {
		t.Fatalf("code=%d body=%s dial_calls=%d", recorder.Code, recorder.Body.String(), agent.dialCalls)
	}
}

func newVoiceCallTestRouter(t *testing.T) (http.Handler, *apiVoiceAgent) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gateway := voicehost.NewGateway()
	agent := &apiVoiceAgent{}
	if err := gateway.SetAgent("wwan0", agent); err != nil {
		t.Fatalf("SetAgent: %v", err)
	}
	server := &Server{
		auth:          config.WebConfig{Username: "admin", Password: "secret"},
		voiceGW:       gateway,
		loginAttempts: make(map[string]loginAttempt),
	}
	return server.newRouter(), agent
}

func authenticatedVoiceCallRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/devices/wwan0/vowifi/calls", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testSessionToken(t, "secret", time.Now().Add(time.Hour)))
	return req
}
