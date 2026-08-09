package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

var _ imsendpoint.Endpoint = (*Service)(nil)

func TestLegacyClientInviteAPICompletesRealTransaction(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	outbound := recordTransactionWrites(service.transport)
	request := mustClientInviteRequest(t, "legacy-invite")
	events := make(chan string, 4)
	result := make(chan legacyClientInviteOutcome, 1)
	go func() {
		value, err := service.StartClientInvite(context.Background(), imsendpoint.ClientInviteOptions{
			Request: request, Contact: request.Contact(),
			OnStarted: func(handle imsendpoint.InviteHandle) error {
				events <- "started:" + handle.InviteID()
				return nil
			},
			OnResponse: func(response *sip.Response) error {
				events <- fmt.Sprintf("response:%d", response.StatusCode)
				return nil
			},
		})
		result <- legacyClientInviteOutcome{result: value, err: err}
	}()

	written := waitTransactionWrite(t, outbound)
	assertClientInviteEvent(t, events, "started:legacy-invite")
	service.transport.DeliverResponse(mustTransactionResponse(t, written, 180))
	assertClientInviteEvent(t, events, "response:180")
	service.transport.DeliverResponse(mustTransactionResponse(t, written, 200))
	assertClientInviteEvent(t, events, "response:200")
	outcome := <-result
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if outcome.result == nil || outcome.result.Response == nil || outcome.result.Response.StatusCode != 200 {
		t.Fatalf("result = %+v", outcome.result)
	}
	if outcome.result.InviteHandle.InviteID() != "legacy-invite" || outcome.result.Dialog == nil {
		t.Fatalf("handles = invite:%v dialog:%v", outcome.result.InviteHandle, outcome.result.Dialog)
	}
	request.CSeq().SeqNo = 99
	handle := outcome.result.InviteHandle.(*imscoreInviteHandle)
	if handle.initialRequest.CSeq().SeqNo != 1 {
		t.Fatalf("stored INVITE was mutated: CSeq=%d", handle.initialRequest.CSeq().SeqNo)
	}
}

func TestLegacyCancelAPIUsesRelatedClientTransaction(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	outbound := recordTransactionWrites(service.transport)
	request := mustClientInviteRequest(t, "legacy-cancel")
	started := make(chan imsendpoint.InviteHandle, 1)
	inviteResult := make(chan legacyClientInviteOutcome, 1)
	go func() {
		value, err := service.StartClientInvite(context.Background(), imsendpoint.ClientInviteOptions{
			Request: request, Contact: request.Contact(),
			OnStarted: func(handle imsendpoint.InviteHandle) error {
				started <- handle
				return nil
			},
		})
		inviteResult <- legacyClientInviteOutcome{result: value, err: err}
	}()

	writtenInvite := waitTransactionWrite(t, outbound)
	handle := <-started
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenInvite, 180))
	cancelResult := make(chan error, 1)
	go func() {
		cancelResult <- service.CancelClientInvite(
			context.Background(), handle, imsendpoint.ClientInviteCancelOptions{Reason: "user"},
		)
	}()
	writtenCancel := waitForTransactionMethod(t, outbound, "CANCEL")
	assertCancelMatchesInvite(t, writtenInvite, writtenCancel)
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenInvite, 487))
	assertTransactionACK(t, writtenInvite, waitForTransactionMethod(t, outbound, "ACK"))
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenCancel, 200))
	if err := <-cancelResult; err != nil {
		t.Fatal(err)
	}
	outcome := <-inviteResult
	if outcome.result == nil || outcome.result.Response == nil || outcome.result.Response.StatusCode != 487 {
		t.Fatalf("INVITE result = %+v", outcome.result)
	}
	if outcome.err == nil || !strings.Contains(outcome.err.Error(), "487") {
		t.Fatalf("INVITE error = %v", outcome.err)
	}
}

func TestLegacyClientInviteContextCancellationRetainsFinalResponse(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	outbound := recordTransactionWrites(service.transport)
	request := mustClientInviteRequest(t, "legacy-context-cancel")
	ctx, cancel := context.WithCancel(context.Background())
	responses := make(chan int, 2)
	result := make(chan legacyClientInviteOutcome, 1)
	go func() {
		value, err := service.StartClientInvite(ctx, imsendpoint.ClientInviteOptions{
			Request: request, Contact: request.Contact(),
			OnResponse: func(response *sip.Response) error {
				responses <- response.StatusCode
				return nil
			},
		})
		result <- legacyClientInviteOutcome{result: value, err: err}
	}()

	writtenInvite := waitTransactionWrite(t, outbound)
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenInvite, 180))
	if status := <-responses; status != 180 {
		t.Fatalf("provisional status = %d", status)
	}
	cancel()
	writtenCancel := waitForTransactionMethod(t, outbound, "CANCEL")
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenInvite, 487))
	assertTransactionACK(t, writtenInvite, waitForTransactionMethod(t, outbound, "ACK"))
	service.transport.DeliverResponse(mustTransactionResponse(t, writtenCancel, 200))
	outcome := <-result
	if !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("StartClientInvite error = %v", outcome.err)
	}
	if outcome.result == nil || outcome.result.Response == nil || outcome.result.Response.StatusCode != 487 {
		t.Fatalf("result = %+v", outcome.result)
	}
	if status := <-responses; status != 487 {
		t.Fatalf("final callback status = %d", status)
	}
}

func TestLegacyClientInviteRequiresRegisteredService(t *testing.T) {
	service, err := New(&IMSConfig{Registrar: "ims.example"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartClientInvite(context.Background(), imsendpoint.ClientInviteOptions{
		Request: mustClientInviteRequest(t, "not-registered"),
		Contact: mustClientInviteRequest(t, "contact").Contact(),
	})
	if err == nil || !strings.Contains(err.Error(), "注册成功") {
		t.Fatalf("StartClientInvite error = %v", err)
	}
}

func TestLegacyClientInviteOnStartedFailureCleansTransaction(t *testing.T) {
	service := newRegisteredClientInviteService(t)
	service.transport.SetSendFn(func(string) error { return nil })
	want := errors.New("started failed")
	result, err := service.StartClientInvite(context.Background(), imsendpoint.ClientInviteOptions{
		Request:   mustClientInviteRequest(t, "started-error"),
		Contact:   mustClientInviteRequest(t, "contact").Contact(),
		OnStarted: func(imsendpoint.InviteHandle) error { return want },
	})
	if !errors.Is(err, want) || result == nil || result.InviteHandle == nil {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	assertTransactionCount(t, service.transport, 0)
}

type legacyClientInviteOutcome struct {
	result *imsendpoint.ClientInviteResult
	err    error
}

func newRegisteredClientInviteService(t *testing.T) *Service {
	t.Helper()
	service, err := New(&IMSConfig{Registrar: "ims.example"})
	if err != nil {
		t.Fatal(err)
	}
	service.regState = regRegistered
	service.regStatus.Store(registrationRegistered)
	t.Cleanup(func() { _ = service.transport.Close() })
	return service
}

func mustClientInviteRequest(t *testing.T, callID string) *sip.Request {
	t.Helper()
	raw := transactionRequestWithTransport("INVITE", callID, "TCP")
	raw = strings.Replace(raw, "Content-Length: 0", "Contact: <sip:user@127.0.0.1:5060>\r\nContent-Length: 0", 1)
	message, err := parseSIPMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	request, ok := message.(*sip.Request)
	if !ok {
		t.Fatalf("message type = %T", message)
	}
	return request
}

func assertClientInviteEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	if got := <-events; got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
}
