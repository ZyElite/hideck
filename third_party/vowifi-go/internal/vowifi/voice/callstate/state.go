// Package callstate defines the voice call state machine and the actor
// (single-goroutine task queue) that serializes call transitions.
//
// Reconstructed from the decompiled internal/vowifi/voice/callstate
// (actor.go, state.go). The state machine has 8 states (0-7) with strictly
// forward transitions recovered from the binary's transition map.
package callstate

import "fmt"

// State is the call state.
type State int

// Call states (recovered from the binary's State.String switch and the
// transition map in map.init.0).
const (
	StateIdle         State = iota // 0: no call
	StateDialing                   // 1: outbound INVITE sent
	StateAlerting                  // 2: 180 Ringing received
	StateConnecting                // 3: early media / 183 received
	StateConnected                 // 4: 200 OK + ACK, media active
	StateDisconnected              // 5: call released (BYE)
	StateFailed                    // 6: call failed (error response)
	StateEnded                     // 7: terminal
)

// String returns the state name.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateDialing:
		return "Dialing"
	case StateAlerting:
		return "Alerting"
	case StateConnecting:
		return "Connecting"
	case StateConnected:
		return "Connected"
	case StateDisconnected:
		return "Disconnected"
	case StateFailed:
		return "Failed"
	case StateEnded:
		return "Ended"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// transitionMap is the recovered transition table: from each state, the set
// of allowed next states. State 4 (Connected) may return to State 3
// (Connecting) for a re-INVITE media renegotiation (recovered from the
// binary's map.init.0).
var transitionMap = map[State]map[State]bool{
	StateIdle:         {StateDialing: true, StateAlerting: true, StateDisconnected: true, StateEnded: true},
	StateDialing:      {StateAlerting: true, StateConnecting: true, StateDisconnected: true, StateFailed: true, StateEnded: true},
	StateAlerting:     {StateConnecting: true, StateDisconnected: true, StateFailed: true, StateEnded: true},
	StateConnecting:   {StateConnected: true, StateDisconnected: true, StateFailed: true, StateEnded: true},
	StateConnected:    {StateConnecting: true, StateDisconnected: true, StateFailed: true, StateEnded: true},
	StateDisconnected: {StateFailed: true, StateEnded: true},
	StateFailed:       {StateEnded: true},
	StateEnded:        {},
}

// CanTransition reports whether from may transition to to.
func CanTransition(from, to State) bool {
	next, ok := transitionMap[from]
	if !ok {
		return false
	}
	return next[to]
}

// IsTerminal reports whether the state is terminal (no further transitions).
func IsTerminal(s State) bool {
	return len(transitionMap[s]) == 0
}

// Direction is the call direction.
type Direction int

// Call directions.
const (
	DirectionOutbound Direction = iota
	DirectionInbound
)

// String returns the direction name.
func (d Direction) String() string {
	switch d {
	case DirectionOutbound:
		return "outbound"
	case DirectionInbound:
		return "inbound"
	default:
		return fmt.Sprintf("Direction(%d)", int(d))
	}
}

// MediaState is the media plane state.
type MediaState int

// Media states.
const (
	MediaNone MediaState = iota
	MediaActive
	MediaHeld
	MediaMuted
)

// String returns the media state name.
func (m MediaState) String() string {
	switch m {
	case MediaNone:
		return "none"
	case MediaActive:
		return "active"
	case MediaHeld:
		return "held"
	case MediaMuted:
		return "muted"
	default:
		return fmt.Sprintf("MediaState(%d)", int(m))
	}
}

// DialogState is the SIP dialog state.
type DialogState int

// Dialog states.
const (
	DialogNone DialogState = iota
	DialogEarly
	DialogConfirmed
	DialogTerminated
)

// String returns the dialog state name.
func (d DialogState) String() string {
	switch d {
	case DialogNone:
		return "none"
	case DialogEarly:
		return "early"
	case DialogConfirmed:
		return "confirmed"
	case DialogTerminated:
		return "terminated"
	default:
		return fmt.Sprintf("DialogState(%d)", int(d))
	}
}

// Timers holds the call timer configuration.
type Timers struct {
	// NoAnswerTimeout is how long to wait for an answer before failing.
	NoAnswerTimeout int64
	// SessionTimer is the RFC 4028 session refresh interval.
	SessionTimer int64
	// OneWayTimeout is how long to wait for RTP before failing.
	OneWayTimeout int64
}
