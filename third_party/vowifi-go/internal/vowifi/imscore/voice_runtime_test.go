package imscore

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

func TestRegisteredSIPDialogProfileUsesNegotiatedIdentityAndBinding(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	service := &Service{
		cfg: &IMSConfig{
			Domain: "ims.mnc010.mcc234.3gppnetwork.org", LocalIP: net.ParseIP("2001:db8::10"),
			IMEI: "860349050445311", UserAgent: "test-agent",
			RegisterTemplate: IMSRegisterTemplate{
				AccessType: "wlan1", ContactOrder: []string{"access_type", "sip_instance", "audio"},
			},
		},
		regState: regRegistered, registrationTCP: client, registrationTCPProtected: true,
		protectedClientPort: 50309, protectedServerPort: 48554,
		regSession: &registerSession{
			contactUser: "binding-uuid", cseq: 3,
			publicID: "sip:+447840844894@o2.co.uk", serviceRoute: "<sip:pcscf.example;lr>",
			security: &securityAgreement{verifyHeader: "ipsec-3gpp;alg=hmac-sha-1-96"},
		},
	}

	profile, err := service.RegisteredSIPDialogProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.LocalURI != "sip:+447840844894@o2.co.uk" || profile.InitialCSeq != 6 {
		t.Fatalf("registered identity/CSeq = %q/%d", profile.LocalURI, profile.InitialCSeq)
	}
	if profile.LocalAddress != "[2001:db8::10]:50309" || profile.ContactURI != "sip:binding-uuid@[2001:db8::10]:48554" {
		t.Fatalf("registered addresses = local %q contact %q", profile.LocalAddress, profile.ContactURI)
	}
	wantContact := `<sip:binding-uuid@[2001:db8::10]:48554>;+g.3gpp.accesstype="wlan1";+sip.instance="<urn:gsma:imei:86034905-044531-1>";audio`
	if profile.ContactHeader != wantContact {
		t.Fatalf("Contact = %q, want %q", profile.ContactHeader, wantContact)
	}
	nextProfile, err := service.RegisteredSIPDialogProfile()
	if err != nil {
		t.Fatal(err)
	}
	if nextProfile.InitialCSeq != 7 {
		t.Fatalf("next registered CSeq = %d, want 7", nextProfile.InitialCSeq)
	}
}

type recordingVoiceHandler struct {
	request InboundVoiceRequest
	result  InboundVoiceResult
}

type respondingVoiceHandler struct {
	request InboundVoiceRequest
}

func (h *respondingVoiceHandler) HandleInboundVoiceRequest(request InboundVoiceRequest) (InboundVoiceResult, error) {
	h.request = request
	if err := request.Responder.Respond(InboundVoiceResponse{StatusCode: 180}); err != nil {
		return InboundVoiceResult{Handled: true}, err
	}
	return InboundVoiceResult{Handled: true}, nil
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

func TestInboundVoiceResponderRetainsReplyPathForFinalSDPResponse(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handler := &respondingVoiceHandler{}
	service.SetVoiceRequestHandler(handler)
	raw := inboundVoiceInvite("voice-call-retained")
	var mu sync.Mutex
	var responses []string
	if err := service.dispatchInboundSIP(raw, func(value string) error {
		mu.Lock()
		responses = append(responses, value)
		mu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	answer := []byte("v=0\r\nc=IN IP4 192.0.2.2\r\nm=audio 40000 RTP/AVP 0\r\n")
	if err := handler.request.Responder.Respond(InboundVoiceResponse{
		StatusCode: 200, ContentType: "application/sdp", Body: answer,
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(responses) != 2 || !strings.HasPrefix(responses[0], "SIP/2.0 180") || !strings.HasPrefix(responses[1], "SIP/2.0 200") {
		t.Fatalf("responses = %#v", responses)
	}
	if responseToTag(responses[0]) != responseToTag(responses[1]) {
		t.Fatalf("To tags differ: %q / %q", responses[0], responses[1])
	}
	if !strings.Contains(responses[1], "Content-Type: application/sdp") || !strings.HasSuffix(responses[1], string(answer)) {
		t.Fatalf("final response omitted SDP: %q", responses[1])
	}
	if err := handler.request.Responder.Respond(InboundVoiceResponse{StatusCode: 486}); err == nil {
		t.Fatal("second final response unexpectedly succeeded")
	}
}

func inboundVoiceInvite(callID string) string {
	body := "v=0\r\nc=IN IP4 192.0.2.1\r\nm=audio 30000 RTP/AVP 0\r\n"
	return fmt.Sprintf("INVITE sip:user@ims.example SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP server.example;branch=z9hG4bKinvite\r\n"+
		"From: <sip:peer@ims.example>;tag=remote\r\n"+
		"To: <sip:user@ims.example>\r\n"+
		"Call-ID: %s\r\nCSeq: 1 INVITE\r\n"+
		"Content-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", callID, len(body), body)
}

func responseToTag(response string) string {
	to := rawSIPHeaderValue(response, "To")
	_, tag, _ := strings.Cut(strings.ToLower(to), ";tag=")
	return tag
}
