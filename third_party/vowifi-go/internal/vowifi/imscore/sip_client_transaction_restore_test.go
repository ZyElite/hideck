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

const transactionTestWait = 500 * time.Millisecond

func TestUDPNonInviteTransactionRetransmitsAndRetainsFinal(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("MESSAGE", "udp-message", "UDP")
	result := make(chan error, 1)
	go func() {
		response, err := transport.RoundTrip(context.Background(), request)
		if err == nil && response.StatusCode != 200 {
			err = fmt.Errorf("status = %d", response.StatusCode)
		}
		result <- err
	}()

	if got := waitTransactionWrite(t, outbound); got != request {
		t.Fatalf("initial request changed: %q", got)
	}
	if got := waitTransactionWrite(t, outbound); got != request {
		t.Fatalf("retransmission changed: %q", got)
	}
	response := mustTransactionResponse(t, request, 200)
	transport.DeliverResponse(response)
	if err := waitTransactionResult(t, result); err != nil {
		t.Fatal(err)
	}
	assertTransactionCount(t, transport, 1)
	transport.DeliverResponse(response)
	assertNoTransactionWrite(t, outbound, 15*time.Millisecond)
	waitTransactionCount(t, transport, 0)
}

func TestReliableClientTransactionDoesNotRetransmit(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("OPTIONS", "tcp-options", "TCP")
	result := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(context.Background(), request)
		result <- err
	}()

	_ = waitTransactionWrite(t, outbound)
	assertNoTransactionWrite(t, outbound, 30*time.Millisecond)
	if err := waitTransactionResult(t, result); !errors.Is(err, sip.ErrTransactionTimeout) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	assertTransactionCount(t, transport, 0)
}

func TestUDPInviteFinalGeneratesAndRetransmitsTransactionACK(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("INVITE", "udp-invite", "UDP")
	result := make(chan error, 1)
	go func() {
		response, err := transport.RoundTrip(context.Background(), request)
		if err == nil && response.StatusCode != 486 {
			err = fmt.Errorf("status = %d", response.StatusCode)
		}
		result <- err
	}()

	_ = waitTransactionWrite(t, outbound)
	_ = waitTransactionWrite(t, outbound)
	transport.DeliverResponse(mustTransactionResponse(t, request, 180))
	assertNoTransactionWrite(t, outbound, 30*time.Millisecond)
	final := mustTransactionResponse(t, request, 486)
	transport.DeliverResponse(final)
	ack := waitTransactionWrite(t, outbound)
	assertTransactionACK(t, request, ack)
	if err := waitTransactionResult(t, result); err != nil {
		t.Fatal(err)
	}
	assertTransactionCount(t, transport, 1)
	transport.DeliverResponse(final)
	assertTransactionACK(t, request, waitTransactionWrite(t, outbound))
	waitTransactionCount(t, transport, 0)
}

func TestInviteAcceptedStateRoutesFinalRetransmissionCallback(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("INVITE", "accepted-invite", "TCP")
	retransmitted := make(chan int, 1)
	result := make(chan error, 1)
	go func() {
		response, err := transport.roundTripWithCallbacks(context.Background(), request, sipTransactionCallbacks{
			onFinalRetransmit: func(response *sipResponse) error {
				retransmitted <- response.StatusCode
				return nil
			},
		})
		if err == nil && response.StatusCode != 200 {
			err = fmt.Errorf("status = %d", response.StatusCode)
		}
		result <- err
	}()

	_ = waitTransactionWrite(t, outbound)
	final := mustTransactionResponse(t, request, 200)
	transport.DeliverResponse(final)
	if err := waitTransactionResult(t, result); err != nil {
		t.Fatal(err)
	}
	transport.DeliverResponse(final)
	select {
	case status := <-retransmitted:
		if status != 200 {
			t.Fatalf("retransmitted status = %d", status)
		}
	case <-time.After(transactionTestWait):
		t.Fatal("final retransmission callback was not called")
	}
	assertNoTransactionWrite(t, outbound, 15*time.Millisecond)
	waitTransactionCount(t, transport, 0)
}

func TestClientTransactionRetainsSenderAcrossTransportSwitch(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	original := recordTransactionWrites(transport)
	replacement := make(chan string, 8)
	request := transactionRequestWithTransport("INVITE", "sender-snapshot", "UDP")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := transport.RoundTrip(ctx, request)
		result <- err
	}()

	_ = waitTransactionWrite(t, original)
	transport.SetSendFn(func(request string) error {
		replacement <- request
		return nil
	})
	if got := waitTransactionWrite(t, original); got != request {
		t.Fatalf("retransmitted request changed: %q", got)
	}
	transport.DeliverResponse(mustTransactionResponse(t, request, 180))
	cancel()
	cancelRequest := waitForTransactionMethod(t, original, "CANCEL")
	transport.DeliverResponse(mustTransactionResponse(t, request, 487))
	assertTransactionACK(t, request, waitForTransactionMethod(t, original, "ACK"))
	transport.DeliverResponse(mustTransactionResponse(t, cancelRequest, 200))
	if err := waitTransactionResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	assertNoTransactionWrite(t, replacement, 15*time.Millisecond)
}

func assertTransactionACK(t *testing.T, invite, ack string) {
	t.Helper()
	if sipRequestMethod(ack) != "ACK" {
		t.Fatalf("transaction output is not ACK: %q", ack)
	}
	for _, name := range []string{"Via", "Call-ID"} {
		if rawSIPHeaderValue(ack, name) != rawSIPHeaderValue(invite, name) {
			t.Fatalf("ACK %s = %q", name, rawSIPHeaderValue(ack, name))
		}
	}
	inviteCSeq := strings.Fields(rawSIPHeaderValue(invite, "CSeq"))[0]
	if rawSIPHeaderValue(ack, "CSeq") != inviteCSeq+" ACK" {
		t.Fatalf("ACK CSeq = %q", rawSIPHeaderValue(ack, "CSeq"))
	}
	if !strings.Contains(rawSIPHeaderValue(ack, "To"), ";tag=remote") {
		t.Fatalf("ACK To = %q", rawSIPHeaderValue(ack, "To"))
	}
}

func newTimedTestTransport() *sipTransport {
	transport := newSIPTransport()
	transport.timers = sipTransactionTimers{
		t1: 10 * time.Millisecond, t2: 20 * time.Millisecond,
		bf: 50 * time.Millisecond, d: 40 * time.Millisecond,
		k: 30 * time.Millisecond, m: 40 * time.Millisecond,
	}
	return transport
}

func recordTransactionWrites(transport *sipTransport) <-chan string {
	outbound := make(chan string, 32)
	transport.SetSendFn(func(request string) error {
		outbound <- request
		return nil
	})
	return outbound
}

func waitTransactionWrite(t *testing.T, outbound <-chan string) string {
	t.Helper()
	select {
	case request := <-outbound:
		return request
	case <-time.After(transactionTestWait):
		t.Fatal("timed out waiting for SIP transaction write")
		return ""
	}
}

func assertNoTransactionWrite(t *testing.T, outbound <-chan string, wait time.Duration) {
	t.Helper()
	select {
	case request := <-outbound:
		t.Fatalf("unexpected SIP transaction write: %q", request)
	case <-time.After(wait):
	}
}

func waitTransactionResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(transactionTestWait):
		t.Fatal("timed out waiting for SIP transaction result")
		return nil
	}
}

func waitTransactionCount(t *testing.T, transport *sipTransport, want int) {
	t.Helper()
	deadline := time.Now().Add(transactionTestWait)
	for time.Now().Before(deadline) {
		transport.mu.Lock()
		got := len(transport.waiters)
		transport.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	assertTransactionCount(t, transport, want)
}

func assertTransactionCount(t *testing.T, transport *sipTransport, want int) {
	t.Helper()
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if got := len(transport.waiters); got != want {
		t.Fatalf("active transactions = %d, want %d", got, want)
	}
}
