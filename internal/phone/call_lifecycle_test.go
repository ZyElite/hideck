package phone

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

func TestIncomingTerminalClassificationsAndDeduplication(t *testing.T) {
	tests := []struct {
		name       string
		callID     string
		status     string
		answer     bool
		userReject bool
	}{
		{name: "remote cancel is missed", callID: "missed-1", status: StatusMissed},
		{name: "answered call is completed", callID: "completed-1", status: StatusCompleted, answer: true},
		{name: "user reject is rejected", callID: "rejected-1", status: StatusRejected, userReject: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
			notifications := make(chan capturedNotification, 2)
			service := newPhoneTestService(t, gateway, store, time.Second)
			service.notifier = captureNotifier{notifications: notifications}
			gateway.emitIncoming(voicehost.IncomingCall{
				DeviceID: "dev-1", CallID: test.callID, Caller: "+15550001",
				OfferSDP: testPlainSDP, ReceivedAt: time.Now(),
			})
			if test.answer {
				gateway.emitEvent(voicehost.CallEvent{
					Type: "CallAnswered", DeviceID: "dev-1", CallID: test.callID, Time: time.Now(),
				})
			}
			if test.userReject {
				addStubMedia(t, service, "media-1", "admin", "lease-1")
				gateway.rejectEmits = true
				if err := service.Reject(ControlRequest{
					Owner: "admin", CallID: test.callID, MediaID: "media-1", Lease: "lease-1",
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				gateway.emitEvent(voicehost.CallEvent{
					Type: "CallCanceled", DeviceID: "dev-1", CallID: test.callID,
					Reason: "remote_cancel", Time: time.Now(),
				})
			}
			waitForRecordStatus(t, store, test.callID, test.status)
			gateway.emitEvent(voicehost.CallEvent{
				Type: "CallCanceled", DeviceID: "dev-1", CallID: test.callID,
				Reason: "duplicate", Time: time.Now(),
			})
			select {
			case notification := <-notifications:
				if notification.status != test.status {
					t.Fatalf("notification status = %q, want %q", notification.status, test.status)
				}
			case <-time.After(time.Second):
				t.Fatal("terminal notification was not delivered")
			}
			select {
			case duplicate := <-notifications:
				t.Fatalf("duplicate terminal notification: %+v", duplicate)
			case <-time.After(30 * time.Millisecond):
			}
		})
	}
}

func TestBusyIncomingCallIsRecordedOnce(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	service := newPhoneTestService(t, gateway, store, time.Second)
	first := voicehost.IncomingCall{DeviceID: "dev-1", CallID: "active-1", ReceivedAt: time.Now()}
	busy := voicehost.IncomingCall{
		DeviceID: "dev-1", CallID: "busy-1", Caller: "+15550002", ReceivedAt: time.Now(),
	}
	gateway.emitIncoming(first)
	gateway.emitIncoming(busy)
	waitForRecordStatus(t, store, busy.CallID, StatusBusy)
	gateway.emitEvent(voicehost.CallEvent{
		Type: "CallBusy", DeviceID: busy.DeviceID, CallID: busy.CallID, Caller: busy.Caller, Time: time.Now(),
	})
	if record := store.record(busy.CallID); record.Status != StatusBusy || record.EndReason != "device_busy" {
		t.Fatalf("busy record = %+v", record)
	}
	select {
	case rejected := <-gateway.rejectCalls:
		if rejected.CallID != busy.CallID || rejected.StatusCode != 486 {
			t.Fatalf("busy rejection = %+v", rejected)
		}
	case <-time.After(time.Second):
		t.Fatal("busy call was not rejected")
	}
	_ = service
}

func TestOutboundEventsEmittedBeforeBeginReturnsAreReplayed(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	gateway.beginEvents = []voicehost.CallEvent{{
		Type: "CallFailed", DeviceID: "dev-1", CallID: "outbound-1", Reason: "busy", Time: time.Now(),
	}}
	service := newPhoneTestService(t, gateway, store, time.Second)
	addStubMedia(t, service, "media-1", "admin", "lease-1")
	call, err := service.StartCall(StartCallRequest{
		Owner: "admin", DeviceID: "dev-1", Callee: "888", MediaID: "media-1", Lease: "lease-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if call.CallID != "outbound-1" {
		t.Fatalf("returned Call-ID = %q", call.CallID)
	}
	waitForRecordStatus(t, store, call.CallID, StatusFailed)
}

func TestControlLeaseProtectsDTMFAndHangup(t *testing.T) {
	gateway, store := newFakeVoiceGateway(), newMemoryCallStore()
	service := newPhoneTestService(t, gateway, store, time.Second)
	call := &activeCall{
		view:   CallView{CallID: "call-1", DeviceID: "dev-1", Status: StatusConnected},
		record: CallRecord{CallID: "call-1", DeviceID: "dev-1", Status: StatusConnected},
		owner:  "admin", lease: "lease-1", terminalDone: make(chan struct{}),
	}
	service.mu.Lock()
	service.calls[call.view.CallID] = call
	service.deviceCalls[call.view.DeviceID] = call.view.CallID
	service.mu.Unlock()
	if err := service.DTMF("admin", call.view.CallID, "wrong", "5"); err == nil {
		t.Fatal("DTMF accepted a foreign lease")
	}
	if err := service.Hangup(context.Background(), "admin", call.view.CallID, "wrong"); err == nil {
		t.Fatal("hangup accepted a foreign lease")
	}
	if err := service.DTMF("admin", call.view.CallID, "lease-1", "5"); err != nil {
		t.Fatal(err)
	}
	if value := <-gateway.dtmfCalls; value != "call-1:5" {
		t.Fatalf("DTMF call = %q", value)
	}
	if active := service.Active("other"); len(active) != 1 || !active[0].ReadOnly {
		t.Fatalf("foreign active view = %+v", active)
	}
	if active := service.Active("lease-1"); len(active) != 1 || active[0].ReadOnly {
		t.Fatalf("owner active view = %+v", active)
	}
}

const testPlainSDP = "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=phone\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 40000 RTP/AVP 0\r\n"
