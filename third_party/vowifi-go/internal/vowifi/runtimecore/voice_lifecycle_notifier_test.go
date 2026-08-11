package runtimecore

import (
	"context"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

type notifierObserver struct {
	events []RuntimeEvent[*SessionResult]
}

func (o *notifierObserver) OnRuntimeEvent(
	_ context.Context,
	event RuntimeEvent[*SessionResult],
) {
	o.events = append(o.events, event)
}

func TestIMSRegisteredNotifierWaitsForSessionService(t *testing.T) {
	observer := &notifierObserver{}
	hookCalls := 0
	notifier := &imsRegisteredNotifier{
		ctx: context.Background(), events: observer,
		hooks:  RuntimeHostHooks{OnIMSRegistered: func(context.Context) { hookCalls++ }},
		device: "wwan1", traceID: "trace-1",
	}

	notifier.OnIMSRegistered()
	if len(observer.events) != 0 || hookCalls != 0 {
		t.Fatalf("registration published before session: events=%d hooks=%d", len(observer.events), hookCalls)
	}

	service := &imscore.Service{}
	session := &SessionResult{DeviceID: "wwan1", IMSService: service}
	notifier.SetSession(session)
	if len(observer.events) != 1 || hookCalls != 1 {
		t.Fatalf("registration publication: events=%d hooks=%d", len(observer.events), hookCalls)
	}
	event := observer.events[0]
	if event.Kind != "ims_registered" || event.Handle != session || event.Service != service {
		t.Fatalf("registration event missing live session service: %+v", event)
	}
}

func TestIMSRegisteredNotifierPublishesImmediatelyWithSession(t *testing.T) {
	observer := &notifierObserver{}
	service := &imscore.Service{}
	session := &SessionResult{DeviceID: "wwan1", IMSService: service}
	notifier := &imsRegisteredNotifier{
		ctx: context.Background(), events: observer, device: "wwan1", traceID: "trace-2",
	}
	notifier.SetSession(session)
	notifier.OnIMSRegistered()

	if len(observer.events) != 1 || observer.events[0].Service != service {
		t.Fatalf("registration events = %+v", observer.events)
	}
}
