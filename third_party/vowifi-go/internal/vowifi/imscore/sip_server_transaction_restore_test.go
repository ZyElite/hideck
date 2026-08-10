package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

type serverTransactionVoiceHandler struct {
	mu       sync.Mutex
	requests []InboundVoiceRequest
	handle   func(InboundVoiceRequest) (InboundVoiceResult, error)
}

func (handler *serverTransactionVoiceHandler) HandleInboundVoiceRequest(
	request InboundVoiceRequest,
) (InboundVoiceResult, error) {
	handler.mu.Lock()
	handler.requests = append(handler.requests, request)
	handler.mu.Unlock()
	return handler.handle(request)
}

func (handler *serverTransactionVoiceHandler) snapshot() []InboundVoiceRequest {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	return append([]InboundVoiceRequest(nil), handler.requests...)
}

type serverResponseRecorder struct {
	mu        sync.Mutex
	responses []string
}

func (recorder *serverResponseRecorder) reply(response string) error {
	recorder.mu.Lock()
	recorder.responses = append(recorder.responses, response)
	recorder.mu.Unlock()
	return nil
}

func (recorder *serverResponseRecorder) snapshot() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]string(nil), recorder.responses...)
}

func TestServerNonInviteUsesHandleAndMemoizesAfterTimerJ(t *testing.T) {
	service := newServerTransactionTestService(t)
	var captured InboundVoiceRequest
	handler := &serverTransactionVoiceHandler{handle: func(request InboundVoiceRequest) (InboundVoiceResult, error) {
		captured = request
		return InboundVoiceResult{Handled: true}, nil
	}}
	service.SetVoiceRequestHandler(handler)
	recorder := &serverResponseRecorder{}
	request := serverTestRequest("UPDATE", "noninvite-call", "noninvite-branch")

	if err := service.dispatchInboundSIP(request, recorder.reply); err != nil {
		t.Fatal(err)
	}
	if captured.InboundRequest == nil {
		t.Fatal("inbound request handle was not routed to production handler")
	}
	if err := service.RespondInboundRequest(t.Context(), service.DeviceID(), captured.InboundRequest, imsendpoint.InboundResponseOptions{
		Code: 202, Reason: "Accepted",
	}); err != nil {
		t.Fatal(err)
	}
	waitForServerCondition(t, func() bool { return len(recorder.snapshot()) == 1 })
	waitForServerCondition(t, func() bool { return service.serverTransactionCount() == 0 })
	if err := service.dispatchInboundSIP(request, recorder.reply); err != nil {
		t.Fatal(err)
	}
	responses := recorder.snapshot()
	if len(handler.snapshot()) != 1 || len(responses) != 2 {
		t.Fatalf("handler calls=%d responses=%d", len(handler.snapshot()), len(responses))
	}
	if !strings.HasPrefix(responses[1], "SIP/2.0 202 Accepted") {
		t.Fatalf("memoized response = %q", responses[1])
	}
}

func TestInboundPRACKRespondsBeforeVoiceDispatchAndPublishesEvent(t *testing.T) {
	service := newServerTransactionTestService(t)
	handler := &serverTransactionVoiceHandler{handle: func(
		InboundVoiceRequest,
	) (InboundVoiceResult, error) {
		return InboundVoiceResult{Handled: true}, errors.New("voice handler must not own IMS PRACK")
	}}
	service.SetVoiceRequestHandler(handler)
	events := make(chan imsendpoint.Event, 1)
	unsubscribe := service.Subscribe(imsendpoint.EventSubscription{
		Name: "test_inbound_prack", QueueSize: 1, Workers: 1,
		Match: func(event imsendpoint.Event) bool {
			return event.Kind == "request" && event.Method == "PRACK"
		},
	}, func(event imsendpoint.Event) { events <- event })
	t.Cleanup(unsubscribe)

	recorder := &serverResponseRecorder{}
	request := serverTestRequest("PRACK", "prack-call", "prack-branch")
	if err := service.dispatchInboundSIP(request, recorder.reply); err != nil {
		t.Fatal(err)
	}
	responses := recorder.snapshot()
	if len(responses) != 1 || !strings.HasPrefix(responses[0], "SIP/2.0 200 OK") {
		t.Fatalf("PRACK responses = %q", responses)
	}
	if len(handler.snapshot()) != 0 {
		t.Fatal("IMS PRACK reached the generic voice handler")
	}
	select {
	case event := <-events:
		if !event.ResponseAcknowledged || event.InboundRequest == nil {
			t.Fatalf("PRACK event = %+v", event)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("IMS PRACK event was not published")
	}
}

func TestRejectedInviteRetransmitsAndACKStopsTransaction(t *testing.T) {
	service := newServerTransactionTestService(t)
	var invite InboundVoiceRequest
	handler := &serverTransactionVoiceHandler{handle: func(request InboundVoiceRequest) (InboundVoiceResult, error) {
		invite = request
		return InboundVoiceResult{Handled: true}, nil
	}}
	service.SetVoiceRequestHandler(handler)
	recorder := &serverResponseRecorder{}
	request := serverTestRequest("INVITE", "reject-call", "reject-branch")
	if err := service.dispatchInboundSIP(request, recorder.reply); err != nil {
		t.Fatal(err)
	}
	if err := service.RejectServerInvite(t.Context(), service.DeviceID(), invite.ServerInvite, imsendpoint.ServerInviteRejectOptions{
		Code: 486, Reason: "Busy Here",
	}); err != nil {
		t.Fatal(err)
	}
	waitForServerCondition(t, func() bool { return len(recorder.snapshot()) >= 2 })
	if len(handler.snapshot()) != 1 {
		t.Fatalf("INVITE handler calls=%d, want 1", len(handler.snapshot()))
	}
	ack := serverTestRequest("ACK", "reject-call", "reject-branch")
	if err := service.dispatchInboundSIP(ack, recorder.reply); err != nil {
		t.Fatal(err)
	}
	waitForServerCondition(t, func() bool { return service.serverTransactionCount() == 0 })
	count := len(recorder.snapshot())
	time.Sleep(3 * service.serverTimers.t1)
	if len(recorder.snapshot()) != count {
		t.Fatal("486 retransmission continued after matching ACK")
	}
}

func TestCancelNotifiesMatchingInviteTransaction(t *testing.T) {
	service := newServerTransactionTestService(t)
	canceled := make(chan *sip.Request, 1)
	handler := &serverTransactionVoiceHandler{handle: func(request InboundVoiceRequest) (InboundVoiceResult, error) {
		if request.Method == "INVITE" {
			handle := request.ServerInvite.(*imscoreServerInviteHandle)
			if !handle.tx.OnCancel(func(cancel *sip.Request) { canceled <- cancel }) {
				return InboundVoiceResult{Handled: true}, fmt.Errorf("register OnCancel")
			}
			return InboundVoiceResult{Handled: true}, nil
		}
		return InboundVoiceResult{Handled: true, StatusCode: 200}, nil
	}}
	service.SetVoiceRequestHandler(handler)
	recorder := &serverResponseRecorder{}
	if err := service.dispatchInboundSIP(
		serverTestRequest("INVITE", "cancel-call", "cancel-branch"), recorder.reply,
	); err != nil {
		t.Fatal(err)
	}
	if err := service.dispatchInboundSIP(
		serverTestRequest("CANCEL", "cancel-call", "cancel-branch"), recorder.reply,
	); err != nil {
		t.Fatal(err)
	}
	select {
	case cancel := <-canceled:
		if !cancel.IsCancel() {
			t.Fatalf("OnCancel request method=%s", cancel.Method)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("matching INVITE transaction did not receive CANCEL")
	}
}

func TestAcceptedInviteUsesLegacyAPIAndOnlyReplaysOnRequest(t *testing.T) {
	service := newServerTransactionTestService(t)
	var invite InboundVoiceRequest
	handler := &serverTransactionVoiceHandler{handle: func(request InboundVoiceRequest) (InboundVoiceResult, error) {
		if request.Method == "INVITE" {
			invite = request
		}
		return InboundVoiceResult{Handled: true}, nil
	}}
	service.SetVoiceRequestHandler(handler)
	recorder := &serverResponseRecorder{}
	raw := serverTestRequest("INVITE", "answer-call", "answer-branch")
	if err := service.dispatchInboundSIP(raw, recorder.reply); err != nil {
		t.Fatal(err)
	}
	request := mustServerTestRequest(t, raw)
	response := buildInboundResponseFromRequest(request, 200, "OK", nil, nil)
	var contactURI sip.Uri
	if err := sip.ParseUri("sip:user@192.0.2.10:5060", &contactURI); err != nil {
		t.Fatal(err)
	}
	dialog, err := service.AnswerServerInvite(t.Context(), service.DeviceID(), invite.ServerInvite, imsendpoint.ServerInviteAnswerOptions{
		Response: response, Contact: &sip.ContactHeader{Address: contactURI},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDialogID, dialogIDErr := sip.DialogIDFromResponse(response)
	if dialogIDErr != nil {
		t.Fatal(dialogIDErr)
	}
	if dialog.DialogID() != wantDialogID {
		t.Fatalf("dialog ID=%q", dialog.DialogID())
	}
	waitForServerCondition(t, func() bool { return len(recorder.snapshot()) == 1 })
	time.Sleep(3 * service.serverTimers.t1)
	if len(recorder.snapshot()) != 1 {
		t.Fatal("2xx response was retransmitted without a duplicate INVITE")
	}
	if err := service.dispatchInboundSIP(raw, recorder.reply); err != nil {
		t.Fatal(err)
	}
	if len(recorder.snapshot()) != 2 || len(handler.snapshot()) != 1 {
		t.Fatalf("duplicate INVITE responses=%d handler calls=%d", len(recorder.snapshot()), len(handler.snapshot()))
	}
	ack := serverTestRequest("ACK", "answer-call", "answer-branch")
	if err := service.dispatchInboundSIP(ack, recorder.reply); err != nil {
		t.Fatal(err)
	}
	requests := handler.snapshot()
	if len(requests) != 2 || requests[1].Method != "ACK" {
		t.Fatalf("2xx ACK was not routed to voice handler: %+v", requests)
	}
}

func TestInviteAutomaticallySendsTrying(t *testing.T) {
	service := newServerTransactionTestService(t)
	service.serverTimers.trying = 5 * time.Millisecond
	handler := &serverTransactionVoiceHandler{handle: func(InboundVoiceRequest) (InboundVoiceResult, error) {
		return InboundVoiceResult{Handled: true}, nil
	}}
	service.SetVoiceRequestHandler(handler)
	recorder := &serverResponseRecorder{}
	if err := service.dispatchInboundSIP(
		serverTestRequest("INVITE", "trying-call", "trying-branch"), recorder.reply,
	); err != nil {
		t.Fatal(err)
	}
	waitForServerCondition(t, func() bool { return len(recorder.snapshot()) == 1 })
	if !strings.HasPrefix(recorder.snapshot()[0], "SIP/2.0 100 Trying") {
		t.Fatalf("automatic response = %q", recorder.snapshot()[0])
	}
}

func TestRejectedInviteTimerHReportsTimeout(t *testing.T) {
	service := newServerTransactionTestService(t)
	service.serverTimers.h = 25 * time.Millisecond
	var invite InboundVoiceRequest
	handler := &serverTransactionVoiceHandler{handle: func(request InboundVoiceRequest) (InboundVoiceResult, error) {
		invite = request
		return InboundVoiceResult{Handled: true}, nil
	}}
	service.SetVoiceRequestHandler(handler)
	if err := service.dispatchInboundSIP(
		serverTestRequest("INVITE", "timeout-call", "timeout-branch"), func(string) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	handle := invite.ServerInvite.(*imscoreServerInviteHandle)
	if err := service.RejectServerInvite(t.Context(), service.DeviceID(), handle, imsendpoint.ServerInviteRejectOptions{Code: 486}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.tx.Done():
		if !errors.Is(handle.tx.Err(), sip.ErrTransactionTimeout) {
			t.Fatalf("Timer H error=%v", handle.tx.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Timer H did not terminate rejected INVITE")
	}
}

func newServerTransactionTestService(t *testing.T) *Service {
	t.Helper()
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	service.serverTimers = serverTransactionTimers{
		t1: 8 * time.Millisecond, t2: 16 * time.Millisecond,
		h: 160 * time.Millisecond, i: 12 * time.Millisecond,
		j: 20 * time.Millisecond, l: 160 * time.Millisecond,
		trying: 500 * time.Millisecond,
	}
	t.Cleanup(service.StopCurrent)
	return service
}

func (s *Service) serverTransactionCount() int {
	s.serverTxMu.Lock()
	defer s.serverTxMu.Unlock()
	return len(s.serverTx)
}

func serverTestRequest(method, callID, branch string) string {
	return fmt.Sprintf("%s sip:user@ims.example SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP server.example;branch=z9hG4bK%s\r\n"+
		"From: <sip:peer@ims.example>;tag=remote\r\n"+
		"To: <sip:user@ims.example>\r\n"+
		"Call-ID: %s\r\nCSeq: 1 %s\r\nContent-Length: 0\r\n\r\n",
		method, branch, callID, method)
}

func mustServerTestRequest(t *testing.T, raw string) *sip.Request {
	t.Helper()
	message, err := parseSIPMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	request, ok := message.(*sip.Request)
	if !ok {
		t.Fatal("test message is not a SIP request")
	}
	return request
}

func waitForServerCondition(t *testing.T, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for server transaction condition")
		case <-ticker.C:
		}
	}
}
