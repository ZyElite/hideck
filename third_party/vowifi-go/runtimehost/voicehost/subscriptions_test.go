package voicehost

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

const subscriptionTestTimeout = time.Second

func TestIncomingLegacyAndSubscriberCoexistInAnyRegistrationOrder(t *testing.T) {
	for _, legacyFirst := range []bool{true, false} {
		legacyFirst := legacyFirst
		t.Run(map[bool]string{true: "legacy-first", false: "subscriber-first"}[legacyFirst], func(t *testing.T) {
			gateway, agent := NewGateway(), &fakeIncomingAgent{}
			legacyCalls, subscribedCalls := make(chan IncomingCall, 2), make(chan IncomingCall, 2)
			registerLegacy := func() { gateway.SetIncomingCallHandler(func(call IncomingCall) { legacyCalls <- call }) }
			registerSubscriber := func() func() {
				return gateway.SubscribeIncomingCalls(func(call IncomingCall) { subscribedCalls <- call })
			}
			var unsubscribe func()
			if legacyFirst {
				registerLegacy()
				unsubscribe = registerSubscriber()
			} else {
				unsubscribe = registerSubscriber()
				registerLegacy()
			}
			defer unsubscribe()
			if err := gateway.SetAgent("dev-1", agent); err != nil {
				t.Fatal(err)
			}
			call := IncomingCall{DeviceID: "dev-1", CallID: "call-1"}
			agent.handler(call)
			agent.handler(call)
			assertIncomingCall(t, legacyCalls, "call-1")
			assertIncomingCall(t, subscribedCalls, "call-1")
			assertNoIncomingCall(t, legacyCalls)
			assertNoIncomingCall(t, subscribedCalls)
		})
	}
}

func TestSlowIncomingHandlerDoesNotBlockOtherSubscriber(t *testing.T) {
	gateway, agent := NewGateway(), &fakeIncomingAgent{}
	releaseSlow := make(chan struct{})
	slowStarted := make(chan struct{})
	gateway.SetIncomingCallHandler(func(IncomingCall) {
		close(slowStarted)
		<-releaseSlow
	})
	fastCalls := make(chan IncomingCall, 1)
	unsubscribe := gateway.SubscribeIncomingCalls(func(call IncomingCall) { fastCalls <- call })
	defer unsubscribe()
	if err := gateway.SetAgent("dev-1", agent); err != nil {
		t.Fatal(err)
	}
	agent.handler(IncomingCall{DeviceID: "dev-1", CallID: "call-slow"})
	awaitSignal(t, slowStarted, "slow handler start")
	assertIncomingCall(t, fastCalls, "call-slow")
	close(releaseSlow)
}

func TestCallEventSubscriberCoexistsWithSlowLegacyDispatcher(t *testing.T) {
	gateway := NewGateway()
	releaseSlow := make(chan struct{})
	slowStarted := make(chan struct{})
	dispatcher := callEventDispatcherFunc(func(context.Context, eventhost.Event) {
		close(slowStarted)
		<-releaseSlow
	})
	received := make(chan CallEvent, 1)
	unsubscribe := gateway.SubscribeCallEvents(func(event CallEvent) { received <- event })
	defer unsubscribe()
	adapter := eventDispatcherAdapter{dispatch: dispatcher, gateway: gateway}
	started := time.Now()
	adapter.Dispatch(context.Background(), events.EventCallEnded{
		DevID: "dev-1", CallID: "call-1", Reason: "remote_bye", EndedAt: started,
	})
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("slow legacy dispatcher blocked event publication")
	}
	awaitSignal(t, slowStarted, "slow dispatcher start")
	select {
	case event := <-received:
		if event.CallID != "call-1" || event.Reason != "remote_bye" || event.Time != started {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(subscriptionTestTimeout):
		t.Fatal("call event subscriber did not receive event")
	}
	close(releaseSlow)
}

type callEventDispatcherFunc func(context.Context, eventhost.Event)

func (dispatch callEventDispatcherFunc) Dispatch(ctx context.Context, event eventhost.Event) {
	dispatch(ctx, event)
}

func assertIncomingCall(t *testing.T, calls <-chan IncomingCall, callID string) {
	t.Helper()
	select {
	case call := <-calls:
		if call.CallID != callID {
			t.Fatalf("call id = %q, want %q", call.CallID, callID)
		}
	case <-time.After(subscriptionTestTimeout):
		t.Fatalf("timed out waiting for call %q", callID)
	}
}

func assertNoIncomingCall(t *testing.T, calls <-chan IncomingCall) {
	t.Helper()
	select {
	case call := <-calls:
		t.Fatalf("unexpected duplicate call: %+v", call)
	case <-time.After(50 * time.Millisecond):
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(subscriptionTestTimeout):
		t.Fatalf("timed out waiting for %s", label)
	}
}
