package imscore

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

const testVoiceEventSubscription = "voice_ims_dispatch"

type eventOwnedVoiceHandler struct {
	calls atomic.Int32
}

func (h *eventOwnedVoiceHandler) HandleInboundVoiceRequest(
	InboundVoiceRequest,
) (InboundVoiceResult, error) {
	h.calls.Add(1)
	return InboundVoiceResult{Handled: true, StatusCode: 200}, nil
}

func (*eventOwnedVoiceHandler) OwnsInboundVoiceMethod(method string) bool {
	return strings.EqualFold(strings.TrimSpace(method), "INVITE")
}

func (*eventOwnedVoiceHandler) InboundVoiceEventSubscription() string {
	return testVoiceEventSubscription
}

func TestEventOwnedVoiceRequestFallsBackWhenEventWasNotDelivered(t *testing.T) {
	service := mustEventTestService(t)
	handler := &eventOwnedVoiceHandler{}
	service.SetVoiceRequestHandler(handler)
	result, err := service.handleInboundSIPTransaction(
		context.Background(), testInboundVoiceInvite(), func(string) error { return nil }, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if handler.calls.Load() != 1 || !strings.HasPrefix(result.response, "SIP/2.0 200 ") {
		t.Fatalf("fallback calls=%d response=%q", handler.calls.Load(), result.response)
	}
}

func TestEventOwnedVoiceRequestSkipsSyncHandlerOnlyAfterNamedDelivery(t *testing.T) {
	service := mustEventTestService(t)
	handler := &eventOwnedVoiceHandler{}
	service.SetVoiceRequestHandler(handler)
	receipt := imsEventPublishReceipt{subscriptions: map[string]int{testVoiceEventSubscription: 1}}
	result, err := service.handleInboundSIPDispatch(context.Background(), inboundSIPDispatch{
		raw: testInboundVoiceInvite(), reply: func(string) error { return nil }, events: receipt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handler.calls.Load() != 0 || result.response != "" {
		t.Fatalf("event-owned calls=%d response=%q", handler.calls.Load(), result.response)
	}
}

func TestIMSEventPublishReceiptNamesAcceptedSubscription(t *testing.T) {
	bus := newIMSEventBus("receipt-device")
	delivered := make(chan struct{}, 1)
	unsubscribe := bus.subscribe(imsendpoint.EventSubscription{
		Name: testVoiceEventSubscription,
	}, func(imsendpoint.Event) { delivered <- struct{}{} })
	t.Cleanup(unsubscribe)
	receipt := bus.publishWithReceipt(imsendpoint.Event{Kind: "request", Method: "INVITE"})
	if !receipt.enqueuedFor(testVoiceEventSubscription) || receipt.enqueued != 1 {
		t.Fatalf("publish receipt = %+v", receipt)
	}
	<-delivered
}

func testInboundVoiceInvite() string {
	return "INVITE sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-voice-owner\r\n" +
		"From: <sip:caller@ims.example>;tag=caller\r\n" +
		"To: <sip:user@ims.example>\r\n" +
		"Call-ID: voice-owner-call\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Type: application/sdp\r\n" +
		"Content-Length: 3\r\n\r\nv=0"
}
