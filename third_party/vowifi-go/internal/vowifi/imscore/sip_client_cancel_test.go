package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func TestInviteContextCancellationSendsRelatedCANCELAndWaitsFor487(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("INVITE", "cancel-invite", "UDP")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(ctx, request)
		result <- err
	}()

	_ = waitTransactionWrite(t, outbound)
	transport.DeliverResponse(mustTransactionResponse(t, request, 180))
	cancel()
	cancelRequest := waitForTransactionMethod(t, outbound, "CANCEL")
	assertCancelMatchesInvite(t, request, cancelRequest)
	transport.DeliverResponse(mustTransactionResponse(t, request, 487))
	assertTransactionACK(t, request, waitForTransactionMethod(t, outbound, "ACK"))
	transport.DeliverResponse(mustTransactionResponse(t, cancelRequest, 200))
	if err := waitTransactionResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip error = %v", err)
	}
}

func TestDetachedCANCELUsesClientTransactionAndConsumesResponse(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	invite := transactionRequestWithTransport("INVITE", "detached-cancel", "TCP")
	cancelRequest := strings.Replace(invite, "INVITE ", "CANCEL ", 1)
	cancelRequest = strings.Replace(cancelRequest, "CSeq: 1 INVITE", "CSeq: 1 CANCEL", 1)
	if err := transport.Send(cancelRequest); err != nil {
		t.Fatal(err)
	}
	_ = waitTransactionWrite(t, outbound)
	assertTransactionCount(t, transport, 1)
	transport.DeliverResponse(mustTransactionResponse(t, cancelRequest, 200))
	waitTransactionCount(t, transport, 0)
	select {
	case response := <-transport.Responses():
		t.Fatalf("CANCEL response was unmatched: %+v", response)
	case <-time.After(15 * time.Millisecond):
	}
}

func TestConnectionFailureTerminatesActiveClientTransaction(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("OPTIONS", "closed-stream", "TCP")
	result := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(context.Background(), request)
		result <- err
	}()
	_ = waitTransactionWrite(t, outbound)
	transport.terminateClientTransactions(fmt.Errorf("stream EOF: %w", sip.ErrTransactionTransport))
	if err := waitTransactionResult(t, result); !errors.Is(err, sip.ErrTransactionTransport) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	assertTransactionCount(t, transport, 0)
}

func TestBuildClientInviteCancelRequestWrapsBuilderError(t *testing.T) {
	_, err := buildClientInviteCancelRequest(nil)
	if err == nil || !strings.HasPrefix(err.Error(), "client INVITE 不能构造 CANCEL: ") {
		t.Fatalf("buildClientInviteCancelRequest error = %v", err)
	}
}

func TestClientInviteHandleRetainsTransactionForCancel(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	service.transport.timers = newTimedTestTransport().timers
	t.Cleanup(service.StopCurrent)
	outbound := recordTransactionWrites(service.transport)
	request := transactionRequestWithTransport("INVITE", "handle-cancel", "TCP")
	handle := &imscoreInviteHandle{id: "handle-cancel"}
	if err := service.StartClientInviteRaw(handle, request); err != nil {
		t.Fatal(err)
	}
	_ = waitTransactionWrite(t, outbound)
	service.transport.DeliverResponse(mustTransactionResponse(t, request, 180))
	cancelResult := make(chan error, 1)
	go func() { cancelResult <- service.CancelClientInviteRaw(handle) }()
	cancelRequest := waitForTransactionMethod(t, outbound, "CANCEL")
	service.transport.DeliverResponse(mustTransactionResponse(t, cancelRequest, 200))
	if err := waitTransactionResult(t, cancelResult); err != nil {
		t.Fatal(err)
	}
	service.transport.DeliverResponse(mustTransactionResponse(t, request, 487))
	_ = waitForTransactionMethod(t, outbound, "ACK")
	waitInviteHandleDone(t, handle)
	if err := service.CancelClientInviteRaw(handle); err == nil || !strings.Contains(err.Error(), "已结束") {
		t.Fatalf("second CancelClientInvite error = %v", err)
	}
}

func waitInviteHandleDone(t *testing.T, handle *imscoreInviteHandle) {
	t.Helper()
	deadline := time.Now().Add(transactionTestWait)
	for time.Now().Before(deadline) {
		handle.mu.Lock()
		done := handle.done
		handle.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("client INVITE handle did not reach done")
}

func waitForTransactionMethod(t *testing.T, outbound <-chan string, method string) string {
	t.Helper()
	deadline := time.After(transactionTestWait)
	for {
		select {
		case request := <-outbound:
			if strings.EqualFold(sipRequestMethod(request), method) {
				return request
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", method)
			return ""
		}
	}
}

func assertCancelMatchesInvite(t *testing.T, invite, cancel string) {
	t.Helper()
	for _, name := range []string{"Via", "From", "To", "Call-ID"} {
		if rawSIPHeaderValue(cancel, name) != rawSIPHeaderValue(invite, name) {
			t.Fatalf("CANCEL %s = %q, want %q", name,
				rawSIPHeaderValue(cancel, name), rawSIPHeaderValue(invite, name))
		}
	}
	if rawSIPHeaderValue(cancel, "CSeq") != "1 CANCEL" {
		t.Fatalf("CANCEL CSeq = %q", rawSIPHeaderValue(cancel, "CSeq"))
	}
}

func transactionRequestWithTransport(method, callID, transport string) string {
	return method + " sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/" + transport + " 127.0.0.1:5060;branch=z9hG4bK-" + callID + "\r\n" +
		"From: <sip:user@ims.example>;tag=local\r\nTo: <sip:peer@ims.example>\r\n" +
		"Call-ID: " + callID + "\r\nCSeq: 1 " + method + "\r\nContent-Length: 0\r\n\r\n"
}

func mustTransactionResponse(t *testing.T, request string, status int) *sipResponse {
	t.Helper()
	reason := map[int]string{180: "Ringing", 200: "OK", 486: "Busy Here", 487: "Request Terminated"}[status]
	if reason == "" {
		reason = "Response"
	}
	raw := fmt.Sprintf("SIP/2.0 %d %s\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=remote\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
		status, reason, rawSIPHeaderValue(request, "Via"), rawSIPHeaderValue(request, "From"),
		rawSIPHeaderValue(request, "To"), rawSIPHeaderValue(request, "Call-ID"), rawSIPHeaderValue(request, "CSeq"))
	message, err := parseSIPMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := message.(*sip.Response)
	if !ok {
		t.Fatalf("parsed response type = %T", message)
	}
	return newSIPResponse(parsed)
}
