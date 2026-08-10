package imscore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceTransactionCallbacksExposeAcceptedRetransmission(t *testing.T) {
	service, err := New(&IMSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	service.transport.timers = newTimedTestTransport().timers
	t.Cleanup(service.StopCurrent)
	outbound := recordTransactionWrites(service.transport)
	request := transactionRequestWithTransport("INVITE", "public-callback", "TCP")
	retransmitted := make(chan int, 1)
	result := make(chan error, 1)
	go func() {
		response, roundTripErr := service.RoundTripSIPWithCallbacks(
			context.Background(), request, SIPTransactionCallbacks{
				OnFinalRetransmission: func(response SIPResponse) error {
					retransmitted <- response.StatusCode
					return nil
				},
			},
		)
		if roundTripErr == nil && response.StatusCode != 200 {
			t.Errorf("status = %d", response.StatusCode)
		}
		result <- roundTripErr
	}()
	_ = waitTransactionWrite(t, outbound)
	final := mustTransactionResponse(t, request, 200)
	service.transport.DeliverResponse(final)
	if err := waitTransactionResult(t, result); err != nil {
		t.Fatal(err)
	}
	service.transport.DeliverResponse(final)
	select {
	case status := <-retransmitted:
		if status != 200 {
			t.Fatalf("retransmitted status = %d", status)
		}
	case <-time.After(transactionTestWait):
		t.Fatal("timed out waiting for final retransmission callback")
	}
}

func TestCanceledInviteSurfacesLateAcceptedResponse(t *testing.T) {
	transport := newTimedTestTransport()
	t.Cleanup(func() { _ = transport.Close() })
	outbound := recordTransactionWrites(transport)
	request := transactionRequestWithTransport("INVITE", "late-accepted", "TCP")
	ctx, cancel := context.WithCancel(context.Background())
	lateFinal := make(chan int, 1)
	result := make(chan error, 1)
	go func() {
		_, err := transport.roundTripWithCallbacks(ctx, request, sipTransactionCallbacks{
			onFinalRetransmit: func(response *sipResponse) error {
				lateFinal <- response.StatusCode
				return nil
			},
		})
		result <- err
	}()
	_ = waitTransactionWrite(t, outbound)
	transport.DeliverResponse(mustTransactionResponse(t, request, 180))
	cancel()
	cancelRequest := waitForTransactionMethod(t, outbound, "CANCEL")
	transport.DeliverResponse(mustTransactionResponse(t, request, 200))
	if status := <-lateFinal; status != 200 {
		t.Fatalf("late final status = %d", status)
	}
	transport.DeliverResponse(mustTransactionResponse(t, cancelRequest, 200))
	if err := waitTransactionResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip error = %v", err)
	}
}
