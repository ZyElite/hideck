// Package eventhost defines the runtime events published by the IMS host and
// the dispatcher surface the vohive host wires into.
//
// Reconstructed from the decompiled engine/runtimehost/eventhost.
package eventhost

import (
	"context"
	"time"
)

// Event is a runtime event published by the IMS host.
type Event interface {
	// Type returns the event type string.
	Type() string
	// DeviceID returns the device the event belongs to.
	DeviceID() string
	event()
}

// Generic is a generic runtime event.
type Generic struct {
	DevID    string
	TypeName string
}

// SMSReceived is published when an SMS is received.
type SMSReceived struct {
	DevID     string
	Sender    string
	TargetURI string
	Content   string
	Time      time.Time
}

// SMSSent is published when an SMS is sent.
type SMSSent struct {
	DevID      string
	TargetURI  string
	Content    string
	Time       time.Time
	TotalParts int
}

// LocalNumberLearned is published when the local phone number is learned.
type LocalNumberLearned struct {
	DevID  string
	IMSI   string
	Number string
	Source string
}

// LogNotify is a log notification event.
type LogNotify struct {
	Message string
}

func (SMSReceived) event()        {}
func (SMSSent) event()            {}
func (LocalNumberLearned) event() {}
func (LogNotify) event()          {}
func (Generic) event()            {}

// Type returns "SMSReceived".
func (e SMSReceived) Type() string { return "SMSReceived" }

// DeviceID returns the device ID.
func (e SMSReceived) DeviceID() string { return e.DevID }

// Type returns "SMSSent".
func (e SMSSent) Type() string { return "SMSSent" }

// DeviceID returns the device ID.
func (e SMSSent) DeviceID() string { return e.DevID }

// Type returns "LocalNumberLearned".
func (e LocalNumberLearned) Type() string { return "LocalNumberLearned" }

// DeviceID returns the device ID.
func (e LocalNumberLearned) DeviceID() string { return e.DevID }

// Type returns "LogNotify".
func (e LogNotify) Type() string { return "LogNotify" }

// DeviceID returns an empty device ID for log events.
func (e LogNotify) DeviceID() string { return "" }

// Type returns the generic event type.
func (e Generic) Type() string {
	if e.TypeName != "" {
		return e.TypeName
	}
	return "Generic"
}

// DeviceID returns the device ID.
func (e Generic) DeviceID() string { return e.DevID }

// Dispatcher dispatches runtime events.
type Dispatcher interface {
	Dispatch(ctx context.Context, e Event)
}
