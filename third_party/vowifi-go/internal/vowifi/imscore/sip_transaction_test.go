package imscore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSIPTransactionsRouteConcurrentResponsesByFullKey(t *testing.T) {
	transport := newSIPTransport()
	requests := make(chan string, 2)
	transport.SetSendFn(func(request string) error {
		requests <- request
		return nil
	})
	type result struct {
		callID string
		status int
		err    error
	}
	results := make(chan result, 2)
	var started sync.WaitGroup
	started.Add(2)
	for _, callID := range []string{"register-call", "subscribe-call"} {
		callID := callID
		go func() {
			started.Done()
			method := "REGISTER"
			if strings.HasPrefix(callID, "subscribe") {
				method = "SUBSCRIBE"
			}
			response, err := transport.RoundTrip(context.Background(), transactionRequest(method, callID))
			status := 0
			if response != nil {
				status = response.StatusCode
			}
			results <- result{callID: callID, status: status, err: err}
		}()
	}
	started.Wait()
	first, second := <-requests, <-requests
	transport.DeliverResponse(transactionResponse(second, transactionStatus(second)))
	transport.DeliverResponse(transactionResponse(first, transactionStatus(first)))

	seen := make(map[string]int)
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("RoundTrip(%s): %v", got.callID, got.err)
		}
		seen[got.callID] = got.status
	}
	if seen["register-call"] != 200 || seen["subscribe-call"] != 202 {
		t.Fatalf("routed statuses = %+v", seen)
	}
}

func transactionStatus(request string) int {
	if strings.HasPrefix(request, "SUBSCRIBE ") {
		return 202
	}
	return 200
}

func TestSIPTransactionRegistersBeforeSynchronousSend(t *testing.T) {
	transport := newSIPTransport()
	transport.SetSendFn(func(request string) error {
		transport.DeliverResponse(transactionResponse(request, 200))
		return nil
	})
	response, err := transport.RoundTrip(context.Background(), transactionRequest("MESSAGE", "message-call"))
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestSIPTransactionDeliversProvisionalBeforeFinalResponse(t *testing.T) {
	transport := newSIPTransport()
	request := transactionRequest("INVITE", "reliable-provisional-call")
	transport.SetSendFn(func(string) error {
		transport.DeliverResponse(transactionResponse(request, 183))
		transport.DeliverResponse(transactionResponse(request, 200))
		return nil
	})
	var provisional []int
	response, err := transport.roundTripWithProvisional(context.Background(), request, func(value *sipResponse) error {
		provisional = append(provisional, value.StatusCode)
		return nil
	})
	if err != nil {
		t.Fatalf("roundTripWithProvisional: %v", err)
	}
	if response.StatusCode != 200 || len(provisional) != 1 || provisional[0] != 183 {
		t.Fatalf("final=%d provisional=%v", response.StatusCode, provisional)
	}
}

func TestSIPTransactionTimeoutRemovesWaiter(t *testing.T) {
	transport := newSIPTransport()
	transport.SetSendFn(func(string) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := transport.RoundTrip(ctx, transactionRequest("MESSAGE", "timeout-call"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RoundTrip error = %v", err)
	}
	transport.mu.Lock()
	waiters := len(transport.waiters)
	transport.mu.Unlock()
	if waiters != 0 {
		t.Fatalf("waiters after timeout = %d", waiters)
	}
}

func transactionRequest(method, callID string) string {
	return method + " sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/TCP 127.0.0.1:5060;branch=z9hG4bK-" + callID + "\r\n" +
		"From: <sip:user@ims.example>;tag=local\r\nTo: <sip:peer@ims.example>\r\n" +
		"Call-ID: " + callID + "\r\nCSeq: 1 " + method + "\r\nContent-Length: 0\r\n\r\n"
}

func transactionResponse(request string, status int) *sipResponse {
	return &sipResponse{
		StatusCode: status,
		CallID:     rawSIPHeaderValue(request, "Call-ID"),
		CSeq:       rawSIPHeaderValue(request, "CSeq"),
		Headers:    map[string]string{"Via": rawSIPHeaderValue(request, "Via")},
	}
}
