package eventhost

import (
	"context"
	"reflect"
	"testing"
	"time"
)

type externalEvent struct{}

func (externalEvent) Type() string     { return "External" }
func (externalEvent) DeviceID() string { return "device-external" }

var _ Event = externalEvent{}

func assertEventPrefix(t *testing.T, value any, names ...string) {
	t.Helper()
	typeOf := reflect.TypeOf(value)
	if typeOf.NumField() < len(names) {
		t.Fatalf("%s has %d fields, want at least %d", typeOf, typeOf.NumField(), len(names))
	}
	for index, name := range names {
		if got := typeOf.Field(index).Name; got != name {
			t.Fatalf("%s field %d = %s, want %s", typeOf, index, got, name)
		}
	}
}

func TestRecoveredEventTypePrefixes(t *testing.T) {
	assertEventPrefix(t, Generic{}, "EventType", "DevID")
	assertEventPrefix(t, SMSReceived{}, "DevID", "Sender", "Content", "Time")
	assertEventPrefix(t, SMSSent{}, "DevID", "TargetURI", "Content", "Time", "TotalParts")
	assertEventPrefix(t, LocalNumberLearned{}, "DevID", "IMSI", "Number", "Source", "Time")
	assertEventPrefix(t, LogNotify{}, "DevID", "Message")

	timeType := reflect.TypeOf(time.Time{})
	if reflect.TypeOf(SMSReceived{}).Field(3).Type != timeType ||
		reflect.TypeOf(LocalNumberLearned{}).Field(4).Type != timeType {
		t.Fatal("recovered event timestamp fields do not use time.Time")
	}
}

func TestRecoveredEventMethods(t *testing.T) {
	events := []struct {
		event    Event
		typeName string
		deviceID string
	}{
		{SMSReceived{DevID: "d1"}, "SMSReceived", "d1"},
		{SMSSent{DevID: "d2"}, "SMSSent", "d2"},
		{LocalNumberLearned{DevID: "d3"}, "LocalNumberLearned", "d3"},
		{LogNotify{DevID: "d4"}, "LogNotify", "d4"},
		{Generic{EventType: "Custom", DevID: "d5"}, "Custom", "d5"},
	}
	for _, test := range events {
		if test.event.Type() != test.typeName || test.event.DeviceID() != test.deviceID {
			t.Fatalf("event %T = type %q device %q", test.event, test.event.Type(), test.event.DeviceID())
		}
	}
	if got := (Generic{TypeName: "Current"}).Type(); got != "Current" {
		t.Fatalf("current generic fallback = %q", got)
	}
	if got := (Generic{}).Type(); got != "" {
		t.Fatalf("zero generic type = %q, want empty", got)
	}
}

type externalDispatcher struct{ event Event }

func (dispatcher *externalDispatcher) Dispatch(_ context.Context, event Event) {
	dispatcher.event = event
}

var _ Dispatcher = (*externalDispatcher)(nil)

func TestEventInterfaceIsNotPackageSealed(t *testing.T) {
	dispatcher := &externalDispatcher{}
	dispatcher.Dispatch(context.Background(), externalEvent{})
	if dispatcher.event.Type() != "External" || dispatcher.event.DeviceID() != "device-external" {
		t.Fatalf("external event = %#v", dispatcher.event)
	}
}
