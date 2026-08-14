package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yibaiba/hideck/internal/config"
	"github.com/yibaiba/hideck/internal/phone"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
	"github.com/pion/webrtc/v4"
)

func TestPhoneRoutesEnforceAuthenticationAndControlLease(t *testing.T) {
	gateway := &phoneRouteGatewayStub{}
	service, err := phone.NewService(phone.ServiceOptions{
		Gateway: gateway, WebRTCUDPAddress: "127.0.0.1:0", RecoveryGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Errorf("close phone service: %v", err)
		}
	})
	server := &Server{
		auth:  config.WebConfig{Username: "admin", Password: "secret"},
		phone: service, shutdownCh: make(chan struct{}),
	}
	router := gin.New()
	api := router.Group("/api")
	api.Use(server.authMiddleware())
	server.registerPhoneRoutes(api)

	unauthorized := performPhoneRequest(router, http.MethodGet, "/api/phone/history", "", "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	token := testSessionToken(t, "secret", time.Now().Add(time.Hour))
	offer, closePeer := browserPhoneOffer(t)
	defer closePeer()
	mediaResponse := performPhoneRequest(router, http.MethodPost, "/api/phone/media", token, "", map[string]string{"sdp": offer})
	if mediaResponse.Code != http.StatusCreated {
		t.Fatalf("media status=%d body=%s", mediaResponse.Code, mediaResponse.Body.String())
	}
	var media struct {
		MediaID string `json:"media_id"`
		Lease   string `json:"lease"`
	}
	if err := json.Unmarshal(mediaResponse.Body.Bytes(), &media); err != nil {
		t.Fatal(err)
	}
	callResponse := performPhoneRequest(router, http.MethodPost, "/api/phone/calls", token, media.Lease, map[string]string{
		"device_id": "dev-1", "callee": "888", "media_id": media.MediaID,
	})
	if callResponse.Code != http.StatusAccepted {
		t.Fatalf("call status=%d body=%s", callResponse.Code, callResponse.Body.String())
	}
	foreign := performPhoneRequest(router, http.MethodPost, "/api/phone/calls/call-api-1/dtmf", token, "foreign", map[string]string{"digit": "5"})
	if foreign.Code != http.StatusForbidden {
		t.Fatalf("foreign lease status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	gateway.emit(voicehost.CallEvent{
		Type: "CallAnswered", DeviceID: "dev-1", CallID: "call-api-1", Time: time.Now(),
	})
	dtmf := performPhoneRequest(router, http.MethodPost, "/api/phone/calls/call-api-1/dtmf", token, media.Lease, map[string]string{"digit": "5"})
	if dtmf.Code != http.StatusNoContent || gateway.dtmf != "call-api-1:5" {
		t.Fatalf("DTMF status=%d forwarded=%q body=%s", dtmf.Code, gateway.dtmf, dtmf.Body.String())
	}
	hangup := performPhoneRequest(router, http.MethodDelete, "/api/phone/calls/call-api-1", token, media.Lease, nil)
	if hangup.Code != http.StatusNoContent {
		t.Fatalf("hangup status=%d body=%s", hangup.Code, hangup.Body.String())
	}
}

type phoneRouteGatewayStub struct {
	incoming func(voicehost.IncomingCall)
	events   func(voicehost.CallEvent)
	dtmf     string
}

func (g *phoneRouteGatewayStub) SubscribeIncomingCalls(handler func(voicehost.IncomingCall)) func() {
	g.incoming = handler
	return func() {}
}

func (g *phoneRouteGatewayStub) SubscribeCallEvents(handler func(voicehost.CallEvent)) func() {
	g.events = handler
	return func() {}
}

func (g *phoneRouteGatewayStub) BeginCall(_ context.Context, _ voicehost.BeginCallRequest) (voicehost.CallSnapshot, error) {
	return voicehost.CallSnapshot{CallID: "call-api-1", DeviceID: "dev-1"}, nil
}

func (g *phoneRouteGatewayStub) ActiveCall(string) *voicehost.CallSnapshot {
	return &voicehost.CallSnapshot{CallID: "call-api-1", DeviceID: "dev-1", ClientSDP: apiPhonePlainSDP}
}

func (g *phoneRouteGatewayStub) AnswerIncomingCall(_ context.Context, request voicehost.AnswerRequest) (voicehost.AnswerResult, error) {
	return voicehost.AnswerResult{CallID: request.CallID}, nil
}

func (g *phoneRouteGatewayStub) RejectIncomingCall(voicehost.RejectRequest) error { return nil }

func (g *phoneRouteGatewayStub) HangupCall(_ context.Context, deviceID, callID string) error {
	g.emit(voicehost.CallEvent{
		Type: "CallEnded", DeviceID: deviceID, CallID: callID, Reason: "local_hangup", Time: time.Now(),
	})
	return nil
}

func (g *phoneRouteGatewayStub) SendCallDTMF(_, callID, digit string) error {
	g.dtmf = callID + ":" + digit
	return nil
}

func (g *phoneRouteGatewayStub) StartCallCapture(_, _, _ string) error { return nil }

func (g *phoneRouteGatewayStub) emit(event voicehost.CallEvent) {
	if g.events != nil {
		g.events(event)
	}
}

func performPhoneRequest(router http.Handler, method, path, token, lease string, body interface{}) *httptest.ResponseRecorder {
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	request := httptest.NewRequest(method, path, &payload)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if lease != "" {
		request.Header.Set("X-Phone-Lease", lease)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func browserPhoneOffer(t *testing.T) (string, func()) {
	t.Helper()
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		_ = peer.Close()
		t.Fatal(err)
	}
	gathering := webrtc.GatheringCompletePromise(peer)
	offer, err := peer.CreateOffer(nil)
	if err == nil {
		err = peer.SetLocalDescription(offer)
	}
	if err != nil {
		_ = peer.Close()
		t.Fatal(err)
	}
	select {
	case <-gathering:
	case <-time.After(time.Second):
		_ = peer.Close()
		t.Fatal("ICE gathering timed out")
	}
	return peer.LocalDescription().SDP, func() { _ = peer.Close() }
}

const apiPhonePlainSDP = "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=phone\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 40000 RTP/AVP 0\r\n"
