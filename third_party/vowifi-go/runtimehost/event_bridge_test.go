package runtimehost

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

type captureRuntimeDispatcher struct {
	events []eventhost.Event
}

func (d *captureRuntimeDispatcher) Dispatch(_ context.Context, event eventhost.Event) {
	d.events = append(d.events, event)
}

func TestIMSEventBridgeDispatchesInboundSMS(t *testing.T) {
	dispatcher := &captureRuntimeDispatcher{}
	bridge := &imsEventBridge{dispatcher: dispatcher}
	receivedAt := time.Now()
	bridge.OnIMSEvent(&events.EventSMSReceived{
		DevID: "wwan0", Sender: "+447700900123", TargetURI: "sip:user@ims.example",
		Content: "hello", Time: receivedAt,
	})

	if len(dispatcher.events) != 1 {
		t.Fatalf("dispatched events = %d, want 1", len(dispatcher.events))
	}
	received, ok := dispatcher.events[0].(eventhost.SMSReceived)
	if !ok {
		t.Fatalf("event type = %T, want SMSReceived", dispatcher.events[0])
	}
	if received.DevID != "wwan0" || received.Sender != "+447700900123" || received.Content != "hello" || !received.Time.Equal(receivedAt) {
		t.Fatalf("received event = %+v", received)
	}
}

func TestIMSEventBridgePreservesUnknownEventType(t *testing.T) {
	dispatcher := &captureRuntimeDispatcher{}
	bridge := &imsEventBridge{dispatcher: dispatcher}
	bridge.OnIMSEvent(&events.EventSMSDeliveryUpdated{DevID: "wwan0", MessageID: "msg-1", State: "delivered"})

	generic, ok := dispatcher.events[0].(eventhost.Generic)
	if !ok || generic.DevID != "wwan0" || generic.TypeName != "SMSDeliveryUpdated" {
		t.Fatalf("generic event = %#v", dispatcher.events[0])
	}
}
