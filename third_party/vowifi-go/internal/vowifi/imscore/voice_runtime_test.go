package imscore

import (
	"strings"
	"testing"
)

type recordingVoiceHandler struct {
	request InboundVoiceRequest
	result  InboundVoiceResult
}

func (h *recordingVoiceHandler) HandleInboundVoiceRequest(request InboundVoiceRequest) (InboundVoiceResult, error) {
	h.request = request
	return h.result, nil
}

func TestInboundVoiceBYERoutesToHandler(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handler := &recordingVoiceHandler{result: InboundVoiceResult{Handled: true, StatusCode: 200}}
	service.SetVoiceRequestHandler(handler)
	raw := "BYE sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP server.example;branch=z9hG4bKbye\r\n" +
		"From: <sip:peer@ims.example>;tag=remote\r\n" +
		"To: <sip:user@ims.example>;tag=local\r\n" +
		"Call-ID: voice-call-1\r\nCSeq: 2 BYE\r\nContent-Length: 0\r\n\r\n"
	result, err := service.handleInboundSIP(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if handler.request.Method != "BYE" || handler.request.CallID != "voice-call-1" {
		t.Fatalf("routed request = %+v", handler.request)
	}
	if !strings.HasPrefix(result.response, "SIP/2.0 200 OK") {
		t.Fatalf("response = %q", result.response)
	}
}

func TestInboundVoiceACKDoesNotGenerateResponse(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	service.SetVoiceRequestHandler(&recordingVoiceHandler{result: InboundVoiceResult{Handled: true}})
	raw := "ACK sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP server.example;branch=z9hG4bKack\r\n" +
		"Call-ID: voice-call-1\r\nCSeq: 1 ACK\r\nContent-Length: 0\r\n\r\n"
	result, err := service.handleInboundSIP(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.response != "" {
		t.Fatalf("ACK generated a response: %q", result.response)
	}
}
