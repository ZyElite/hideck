package events

import "testing"

func TestEventTypes(t *testing.T) {
	cases := []struct {
		ev   Event
		typ  string
		dev  string
	}{
		{&EventSMSReceived{DevID: "d1"}, "SMSReceived", "d1"},
		{&EventSMSSent{DevID: "d1"}, "SMSSent", "d1"},
		{&EventSMSSendAccepted{DevID: "d1"}, "SMSSendAccepted", "d1"},
		{&EventSMSDeliveryUpdated{DevID: "d1"}, "SMSDeliveryUpdated", "d1"},
		{&EventSMSDeliveryCompleted{DevID: "d1"}, "SMSDeliveryCompleted", "d1"},
		{&EventSMSDeliveryFailed{DevID: "d1"}, "SMSDeliveryFailed", "d1"},
		{&EventLocalNumberLearned{DevID: "d1"}, "LocalNumberLearned", "d1"},
		{&EventLogNotify{DevID: "d1"}, "LogNotify", "d1"},
		{&EventUSSDResult{DevID: "d1"}, "USSDResult", "d1"},
		{&EventIncomingCall{DevID: "d1"}, "IncomingCall", "d1"},
		{&EventCallRinging{DevID: "d1"}, "CallRinging", "d1"},
		{&EventCallAnswered{DevID: "d1"}, "CallAnswered", "d1"},
		{&EventCallEnded{DevID: "d1"}, "CallEnded", "d1"},
		{&EventCallFailed{DevID: "d1"}, "CallFailed", "d1"},
		{&EventCallCanceled{DevID: "d1"}, "CallCanceled", "d1"},
		{&EventCallMediaUpdated{DevID: "d1"}, "CallMediaUpdated", "d1"},
	}
	for _, tc := range cases {
		if got := tc.ev.Type(); got != tc.typ {
			t.Errorf("%T.Type() = %q, want %q", tc.ev, got, tc.typ)
		}
		if got := tc.ev.DeviceID(); got != tc.dev {
			t.Errorf("%T.DeviceID() = %q, want %q", tc.ev, got, tc.dev)
		}
	}
}
