package ussi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEncodeDecodeXML(t *testing.T) {
	payload := &XMLPayload{Language: "en", Text: "hello", Request: &struct{}{}}
	body, err := EncodeXML(payload)
	if err != nil {
		t.Fatalf("EncodeXML: %v", err)
	}
	decoded, err := DecodeXML(body)
	if err != nil {
		t.Fatalf("DecodeXML: %v", err)
	}
	if decoded.Text != "hello" || decoded.Request == nil {
		t.Fatalf("decoded payload = %#v", decoded)
	}
}

func TestDecodeXMLRejectsUnrelatedRoot(t *testing.T) {
	if _, err := DecodeXML([]byte("<message>hello</message>")); err == nil {
		t.Fatal("DecodeXML accepted an unrelated root")
	}
}

func TestLooksLikeMenu(t *testing.T) {
	if !LooksLikeMenu("1. Balance\n2. Top-up") {
		t.Error("menu should be detected")
	}
	if LooksLikeMenu("Your balance is $10") {
		t.Error("plain message should not be a menu")
	}
}

func TestBuildMultipartBody(t *testing.T) {
	body := BuildMultipartBody([]byte("v=0\r\n"), []byte("<ussd-data/>"))
	part := ExtractFromMultipart(body, ContentType)
	if string(part) != "<ussd-data/>" {
		t.Fatalf("extracted part = %q", part)
	}
}

func TestServiceNetworkLifecycle(t *testing.T) {
	transport := &scriptedTransport{}
	transport.roundTrip = func(_ context.Context, request string) (Response, error) {
		switch requestMethod(request) {
		case "INVITE":
			return ussiResponse(requestBodyXML(t, "1. Balance\n2. Top-up", false)), nil
		case "INFO":
			return ussiResponse(requestBodyXML(t, "Your balance is 10", true)), nil
		default:
			return Response{}, errors.New("unexpected request")
		}
	}
	svc := configuredService(t, transport)
	result, err := svc.Send("*100#")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Done || result.Message != "1. Balance\n2. Top-up" {
		t.Fatalf("initial result = %#v", result)
	}
	continued, err := svc.Continue(result.SessionID, "1")
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if !continued.Done || continued.Message != "Your balance is 10" {
		t.Fatalf("continued result = %#v", continued)
	}
	if svc.ActiveSessionID() != "" {
		t.Fatal("completed session remained active")
	}
	requests := transport.Requests()
	if len(requests) != 3 || requestMethod(requests[0]) != "INVITE" || requestMethod(requests[1]) != "ACK" || requestMethod(requests[2]) != "INFO" {
		t.Fatalf("SIP sequence = %v", requestMethods(requests))
	}
	if !strings.Contains(requests[0], "Route: <sip:pcscf.ims.example;lr>") {
		t.Fatalf("initial INVITE missing service route:\n%s", requests[0])
	}
}

func TestServiceCancelSendsBYE(t *testing.T) {
	transport := &scriptedTransport{}
	transport.roundTrip = func(_ context.Context, request string) (Response, error) {
		if requestMethod(request) == "INVITE" {
			return ussiResponse(requestBodyXML(t, "1. Continue", false)), nil
		}
		return Response{StatusCode: 200, Reason: "OK"}, nil
	}
	svc := configuredService(t, transport)
	result, err := svc.Send("*100#")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel(result.SessionID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	methods := requestMethods(transport.Requests())
	if methods[len(methods)-1] != "BYE" {
		t.Fatalf("last method = %q, sequence = %v", methods[len(methods)-1], methods)
	}
}

func TestServiceWaitsForInboundINFO(t *testing.T) {
	transport := &scriptedTransport{}
	svc := configuredService(t, transport)
	transport.roundTrip = func(_ context.Context, request string) (Response, error) {
		if requestMethod(request) != "INVITE" {
			return Response{}, errors.New("unexpected request")
		}
		callID := headerValue(request, "Call-ID")
		go func() {
			_, _ = svc.HandleInbound(InboundRequest{
				Method: "INFO", CallID: callID, ContentType: ContentType,
				Body: requestBodyXML(t, "Network result", true),
			})
		}()
		return ussiResponse(nil), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := svc.SendContext(ctx, "*100#")
	if err != nil {
		t.Fatalf("SendContext: %v", err)
	}
	if !result.Done || result.Message != "Network result" {
		t.Fatalf("result = %#v", result)
	}
}

func TestServiceRejectsSIPFailure(t *testing.T) {
	transport := &scriptedTransport{roundTrip: func(context.Context, string) (Response, error) {
		return Response{StatusCode: 503, Reason: "Service Unavailable"}, nil
	}}
	svc := configuredService(t, transport)
	if _, err := svc.Send("*100#"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("Send error = %v", err)
	}
	if svc.ActiveSessionID() != "" {
		t.Fatal("rejected session remained active")
	}
}

func TestStopWakesBlockedSend(t *testing.T) {
	started := make(chan struct{})
	transport := &scriptedTransport{roundTrip: func(context.Context, string) (Response, error) {
		close(started)
		return ussiResponse(nil), nil
	}}
	svc := configuredService(t, transport)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Send("*100#")
		done <- err
	}()
	<-started
	svc.Stop()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Fatalf("Send error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Send was not released")
	}
}

func configuredService(t *testing.T, transport Transport) *Service {
	t.Helper()
	svc, err := NewServiceWithConfig(Config{
		Transport: transport, LocalURI: "sip:user@ims.example",
		ContactURI: "sip:user@192.0.2.10:5060;transport=udp",
		Domain:     "ims.example", LocalAddress: "192.0.2.10:5060",
		SIPTransport: "udp", ServiceRoute: "<sip:pcscf.ims.example;lr>",
		UserAgent: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func requestBodyXML(t *testing.T, text string, done bool) []byte {
	t.Helper()
	payload := &XMLPayload{Language: "en", Text: text}
	if done {
		payload.Notify = &struct{}{}
	} else {
		payload.Request = &struct{}{}
	}
	body, err := EncodeXML(payload)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func ussiResponse(body []byte) Response {
	headers := map[string]string{
		"To": "<sip:ussi@ims.example>;tag=remote", "Contact": "<sip:ussi@server.example>",
		"Record-Route": "<sip:edge.ims.example;lr>",
	}
	if len(body) > 0 {
		headers["Content-Type"] = ContentType
	}
	return Response{StatusCode: 200, Reason: "OK", Headers: headers, Body: body}
}

type scriptedTransport struct {
	mu        sync.Mutex
	roundTrip func(context.Context, string) (Response, error)
	requests  []string
}

func (t *scriptedTransport) RoundTrip(ctx context.Context, request string) (Response, error) {
	t.record(request)
	return t.roundTrip(ctx, request)
}

func (t *scriptedTransport) Send(_ context.Context, request string) error {
	t.record(request)
	return nil
}

func (t *scriptedTransport) record(request string) {
	t.mu.Lock()
	t.requests = append(t.requests, request)
	t.mu.Unlock()
}

func (t *scriptedTransport) Requests() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.requests...)
}

func requestMethod(request string) string {
	fields := strings.Fields(request)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func requestMethods(requests []string) []string {
	methods := make([]string, 0, len(requests))
	for _, request := range requests {
		methods = append(methods, requestMethod(request))
	}
	return methods
}

func headerValue(request, name string) string {
	for _, line := range strings.Split(request, "\r\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
