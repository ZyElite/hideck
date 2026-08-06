// Package events defines the internal IMS event types published on the
// imscore/voice event bus. Each event carries a DeviceID and a Type string.
//
// Reconstructed from the decompiled internal/vowifi/events.
package events

import "time"

// Event is the common interface implemented by all event types.
type Event interface {
	// Type returns the event type string (e.g. "SMSReceived").
	Type() string
	// DeviceID returns the device the event belongs to.
	DeviceID() string
}

// EventSMSReceived is published when an SMS is received.
type EventSMSReceived struct {
	DevID string
	Sender    string
	TargetURI string
	Content   string
	Time      time.Time
}

// EventSMSSent is published when an SMS is sent.
type EventSMSSent struct {
	DevID      string
	TargetURI  string
	Content    string
	Time       time.Time
	TotalParts int
}

// EventSMSSendAccepted is published when an SMS send is accepted.
type EventSMSSendAccepted struct {
	DevID string
	TargetURI string
	Content   string
	Time      time.Time
}

// EventSMSDeliveryUpdated is published when an SMS delivery status updates.
type EventSMSDeliveryUpdated struct {
	DevID string
	MessageID string
	State     string
	Time      time.Time
}

// EventSMSDeliveryCompleted is published when an SMS delivery completes.
type EventSMSDeliveryCompleted struct {
	DevID string
	MessageID string
	Time      time.Time
}

// EventSMSDeliveryFailed is published when an SMS delivery fails.
type EventSMSDeliveryFailed struct {
	DevID string
	MessageID string
	Error     string
	Time      time.Time
}

// EventLocalNumberLearned is published when the local phone number is learned.
type EventLocalNumberLearned struct {
	DevID string
	IMSI     string
	Number   string
	Source   string
}

// EventLogNotify is a log notification event.
type EventLogNotify struct {
	DevID string
	Message  string
}

// EventUSSDResult is published with a USSD result.
type EventUSSDResult struct {
	DevID string
	SessionID string
	Code      string
	Message   string
}

// EventIncomingCall is published on an incoming call.
type EventIncomingCall struct {
	DevID string
	Caller   string
	Callee   string
	Time     time.Time
}

// EventCallRinging is published when a call starts ringing.
type EventCallRinging struct {
	DevID string
	CallID   string
	Time     time.Time
}

// EventCallAnswered is published when a call is answered.
type EventCallAnswered struct {
	DevID string
	CallID   string
	Time     time.Time
}

// EventCallEnded is published when a call ends.
type EventCallEnded struct {
	DevID string
	CallID   string
	Time     time.Time
}

// EventCallFailed is published when a call fails.
type EventCallFailed struct {
	DevID string
	CallID   string
	Reason   string
	Time     time.Time
}

// EventCallCanceled is published when a call is canceled.
type EventCallCanceled struct {
	DevID string
	CallID   string
	Time     time.Time
}

// EventCallMediaUpdated is published when call media is updated.
type EventCallMediaUpdated struct {
	DevID string
	CallID   string
	Time     time.Time
}

// Type returns "SMSReceived".
func (e *EventSMSReceived) Type() string { return "SMSReceived" }

// DeviceID returns the device ID.
func (e *EventSMSReceived) DeviceID() string { return e.DevID }

// Type returns "SMSSent".
func (e *EventSMSSent) Type() string { return "SMSSent" }

// DeviceID returns the device ID.
func (e *EventSMSSent) DeviceID() string { return e.DevID }

// Type returns "SMSSendAccepted".
func (e *EventSMSSendAccepted) Type() string { return "SMSSendAccepted" }

// DeviceID returns the device ID.
func (e *EventSMSSendAccepted) DeviceID() string { return e.DevID }

// Type returns "SMSDeliveryUpdated".
func (e *EventSMSDeliveryUpdated) Type() string { return "SMSDeliveryUpdated" }

// DeviceID returns the device ID.
func (e *EventSMSDeliveryUpdated) DeviceID() string { return e.DevID }

// Type returns "SMSDeliveryCompleted".
func (e *EventSMSDeliveryCompleted) Type() string { return "SMSDeliveryCompleted" }

// DeviceID returns the device ID.
func (e *EventSMSDeliveryCompleted) DeviceID() string { return e.DevID }

// Type returns "SMSDeliveryFailed".
func (e *EventSMSDeliveryFailed) Type() string { return "SMSDeliveryFailed" }

// DeviceID returns the device ID.
func (e *EventSMSDeliveryFailed) DeviceID() string { return e.DevID }

// Type returns "LocalNumberLearned".
func (e *EventLocalNumberLearned) Type() string { return "LocalNumberLearned" }

// DeviceID returns the device ID.
func (e *EventLocalNumberLearned) DeviceID() string { return e.DevID }

// Type returns "LogNotify".
func (e *EventLogNotify) Type() string { return "LogNotify" }

// DeviceID returns the device ID.
func (e *EventLogNotify) DeviceID() string { return e.DevID }

// Type returns "USSDResult".
func (e *EventUSSDResult) Type() string { return "USSDResult" }

// DeviceID returns the device ID.
func (e *EventUSSDResult) DeviceID() string { return e.DevID }

// Type returns "IncomingCall".
func (e *EventIncomingCall) Type() string { return "IncomingCall" }

// DeviceID returns the device ID.
func (e *EventIncomingCall) DeviceID() string { return e.DevID }

// Type returns "CallRinging".
func (e *EventCallRinging) Type() string { return "CallRinging" }

// DeviceID returns the device ID.
func (e *EventCallRinging) DeviceID() string { return e.DevID }

// Type returns "CallAnswered".
func (e *EventCallAnswered) Type() string { return "CallAnswered" }

// DeviceID returns the device ID.
func (e *EventCallAnswered) DeviceID() string { return e.DevID }

// Type returns "CallEnded".
func (e *EventCallEnded) Type() string { return "CallEnded" }

// DeviceID returns the device ID.
func (e *EventCallEnded) DeviceID() string { return e.DevID }

// Type returns "CallFailed".
func (e *EventCallFailed) Type() string { return "CallFailed" }

// DeviceID returns the device ID.
func (e *EventCallFailed) DeviceID() string { return e.DevID }

// Type returns "CallCanceled".
func (e *EventCallCanceled) Type() string { return "CallCanceled" }

// DeviceID returns the device ID.
func (e *EventCallCanceled) DeviceID() string { return e.DevID }

// Type returns "CallMediaUpdated".
func (e *EventCallMediaUpdated) Type() string { return "CallMediaUpdated" }

// DeviceID returns the device ID.
func (e *EventCallMediaUpdated) DeviceID() string { return e.DevID }
