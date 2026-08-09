package events

import (
	"reflect"
	"testing"
)

func TestEventTypes(t *testing.T) {
	cases := []struct {
		ev  Event
		typ string
		dev string
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

func TestEventValuesImplementEvent(t *testing.T) {
	values := []Event{
		EventSMSReceived{}, EventSMSSent{}, EventSMSSendAccepted{},
		EventSMSDeliveryUpdated{}, EventSMSDeliveryCompleted{}, EventSMSDeliveryFailed{},
		EventLocalNumberLearned{}, EventLogNotify{}, EventUSSDResult{}, EventIncomingCall{},
		EventCallRinging{}, EventCallAnswered{}, EventCallEnded{}, EventCallFailed{},
		EventCallCanceled{}, EventCallMediaUpdated{},
	}
	for _, event := range values {
		if event.Type() == "" {
			t.Fatalf("%T returned an empty type", event)
		}
	}
}

func TestRecoveredEventFieldOrder(t *testing.T) {
	tests := []struct {
		value any
		want  []string
	}{
		{EventSMSReceived{}, []string{"DevID", "Sender", "Content", "Time"}},
		{EventSMSSent{}, []string{"DevID", "TargetURI", "Content", "Time", "TotalParts"}},
		{EventSMSSendAccepted{}, []string{"DevID", "MessageID", "TargetURI", "Content", "PartsTotal", "AcceptedAt", "ExpiresHint"}},
		{EventSMSDeliveryUpdated{}, []string{"DevID", "MessageID", "PartNo", "PartsTotal", "State", "SIPCode", "RPCause", "UpdatedAt", "Completed", "FailureText"}},
		{EventSMSDeliveryCompleted{}, []string{"DevID", "MessageID", "PartsTotal", "CompletedAt"}},
		{EventSMSDeliveryFailed{}, []string{"DevID", "TargetURI", "Reason", "SIPCode", "RecommendCSFallback"}},
		{EventLocalNumberLearned{}, []string{"DevID", "IMSI", "Number", "Source", "Time"}},
		{EventLogNotify{}, []string{"DevID", "Message"}},
		{EventUSSDResult{}, []string{"DevID", "SessionID", "Command", "Text", "Status", "Time"}},
		{EventIncomingCall{}, []string{"DevID", "CallID", "Caller", "Callee", "ReceivedAt"}},
		{EventCallRinging{}, []string{"DevID", "CallID", "Time"}},
		{EventCallAnswered{}, []string{"DevID", "CallID", "AnsweredAt"}},
		{EventCallEnded{}, []string{"DevID", "CallID", "Reason", "EndedAt"}},
		{EventCallFailed{}, []string{"DevID", "CallID", "Reason", "Time"}},
		{EventCallCanceled{}, []string{"DevID", "CallID", "Reason", "Time"}},
		{EventCallMediaUpdated{}, []string{"DevID", "CallID", "Direction", "State", "Time"}},
	}
	for _, test := range tests {
		t.Run(reflect.TypeOf(test.value).Name(), func(t *testing.T) {
			typeOf := reflect.TypeOf(test.value)
			if typeOf.NumField() < len(test.want) {
				t.Fatalf("field count = %d, want at least %d", typeOf.NumField(), len(test.want))
			}
			for index, want := range test.want {
				if got := typeOf.Field(index).Name; got != want {
					t.Fatalf("field %d = %q, want %q", index, got, want)
				}
			}
		})
	}
}
