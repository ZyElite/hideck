package imscore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSIPTransactionRetainsLateFinalWhenCallbackApproves(t *testing.T) {
	transport := newSIPTransport()
	t.Cleanup(func() { _ = transport.Close() })
	transport.SetSendFn(func(string) error { return nil })
	request := transactionRequest("MESSAGE", "late-final-call")
	probeTimeout := errors.New("probe timeout")
	finals := make(chan int, 1)
	callbacks := sipTransactionCallbacks{
		onLateFinal: func(response *sipResponse) error {
			finals <- response.StatusCode
			return nil
		},
		retainFinalAfterContext: func(cause error) bool { return errors.Is(cause, probeTimeout) },
		lateFinalRetention:      100 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Millisecond, probeTimeout)
	defer cancel()
	_, err := transport.roundTripWithCallbacks(ctx, request, callbacks)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("round trip error = %v", err)
	}
	assertTransactionCount(t, transport, 1)

	transport.DeliverResponse(transactionResponse(request, 202))
	select {
	case status := <-finals:
		if status != 202 {
			t.Fatalf("late final status = %d", status)
		}
	case <-time.After(transactionTestWait):
		t.Fatal("late final callback was not called")
	}
	waitTransactionCount(t, transport, 0)
}

func TestSIPTransactionDoesNotRetainCallerCancellation(t *testing.T) {
	transport := newSIPTransport()
	t.Cleanup(func() { _ = transport.Close() })
	transport.SetSendFn(func(string) error { return nil })
	probeTimeout := errors.New("probe timeout")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := transport.roundTripWithCallbacks(
		ctx,
		transactionRequest("MESSAGE", "caller-cancel-call"),
		sipTransactionCallbacks{
			onLateFinal: func(*sipResponse) error { return nil },
			retainFinalAfterContext: func(cause error) bool {
				return errors.Is(cause, probeTimeout)
			},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("round trip error = %v", err)
	}
	assertTransactionCount(t, transport, 0)
}

func TestSIPTransactionLateFinalWindowAndCloseCleanWaiter(t *testing.T) {
	for _, test := range []struct {
		name  string
		close bool
	}{
		{name: "window"},
		{name: "close", close: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := newSIPTransport()
			transport.SetSendFn(func(string) error { return nil })
			probeTimeout := errors.New("probe timeout")
			ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Millisecond, probeTimeout)
			defer cancel()
			_, err := transport.roundTripWithCallbacks(
				ctx,
				transactionRequest("MESSAGE", "late-cleanup-"+test.name),
				sipTransactionCallbacks{
					onLateFinal: func(*sipResponse) error { return nil },
					retainFinalAfterContext: func(cause error) bool {
						return errors.Is(cause, probeTimeout)
					},
					lateFinalRetention: 20 * time.Millisecond,
				},
			)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("round trip error = %v", err)
			}
			assertTransactionCount(t, transport, 1)
			if test.close {
				_ = transport.Close()
			}
			waitTransactionCount(t, transport, 0)
			_ = transport.Close()
		})
	}
}

func TestServiceStopCleansRetainedLateFinalTransaction(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	service.transport.SetSendFn(func(string) error { return nil })
	probeTimeout := errors.New("probe timeout")
	ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Millisecond, probeTimeout)
	defer cancel()
	_, err = service.transport.roundTripWithCallbacks(
		ctx,
		transactionRequest("MESSAGE", "service-stop-late-final"),
		sipTransactionCallbacks{
			onLateFinal: func(*sipResponse) error { return nil },
			retainFinalAfterContext: func(cause error) bool {
				return errors.Is(cause, probeTimeout)
			},
			lateFinalRetention: time.Second,
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("round trip error = %v", err)
	}
	assertTransactionCount(t, service.transport, 1)
	service.StopCurrent()
	waitTransactionCount(t, service.transport, 0)
}
