// Package callstate defines the voice call state machine and the actor
// (single-goroutine task queue) that serializes call transitions.
//
// Reconstructed from the decompiled internal/vowifi/voice/callstate
// (actor.go, state.go). The state machine has 8 states (0-7) with transitions
// recovered from the binary's transition map.
package callstate

import "fmt"

// State is the call state.
type State int

// Call states (recovered from the binary's State.String switch and the
// transition map in map.init.0).
const (
	StateInit State = iota
	StateCalling
	StateRinging
	StateEarlyMedia
	StatePreconditionWait
	StateConnected
	StateTerminating
	StateTerminated
)

// Compatibility aliases retain source compatibility with the interim state
// names. StateConnectedCurrent names the interim numeric connected state,
// which the original binary uses for precondition waiting.
const (
	StateIdle             = StateInit
	StateDialing          = StateCalling
	StateAlerting         = StateRinging
	StateConnecting       = StateEarlyMedia
	StateConnectedCurrent = StatePreconditionWait
	StateDisconnected     = StateTerminating
	StateFailed           = StateTerminating
	StateEnded            = StateTerminated
)

// String returns the state name.
func (s State) String() string {
	switch s {
	case StateInit:
		return "Init"
	case StateCalling:
		return "Calling"
	case StateRinging:
		return "Ringing"
	case StateEarlyMedia:
		return "EarlyMedia"
	case StatePreconditionWait:
		return "PreconditionWait"
	case StateConnected:
		return "Connected"
	case StateTerminating:
		return "Terminating"
	case StateTerminated:
		return "Terminated"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// transitionMap is the recovered transition table from the binary's
// callstate.map.init.0.
var transitionMap = map[State]map[State]bool{
	StateInit:             {StateCalling: true, StateRinging: true, StateConnected: true, StateTerminated: true},
	StateCalling:          {StateRinging: true, StateEarlyMedia: true, StateConnected: true, StateTerminating: true, StateTerminated: true},
	StateRinging:          {StateEarlyMedia: true, StateConnected: true, StateTerminating: true, StateTerminated: true},
	StateEarlyMedia:       {StatePreconditionWait: true, StateConnected: true, StateTerminating: true, StateTerminated: true},
	StatePreconditionWait: {StateEarlyMedia: true, StateConnected: true, StateTerminating: true, StateTerminated: true},
	StateConnected:        {StateTerminating: true, StateTerminated: true},
	StateTerminating:      {StateTerminated: true},
	StateTerminated:       {},
}

// CanTransition reports whether from may transition to to.
func CanTransition(from, to State) bool {
	next, ok := transitionMap[from]
	if !ok {
		return false
	}
	return next[to]
}

// IsTerminal reports whether call teardown is terminating or terminated.
func IsTerminal(s State) bool {
	return s == StateTerminating || s == StateTerminated
}

// Direction is the call direction.
type Direction int

// Call directions.
const (
	DirectionInbound Direction = iota
	DirectionOutbound
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

// MediaPhase is the current media-plane phase. It retains the phase constants
// added by the restored implementation without shadowing the original
// MediaState resource structure.
type MediaPhase int

// Media states.
const (
	MediaNone MediaPhase = iota
	MediaActive
	MediaHeld
	MediaMuted
)

// String returns the media state name.
func (m MediaPhase) String() string {
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

// DialogPhase is the current SIP dialog phase. The original DialogState is a
// resource-bearing structure restored in context.go.
type DialogPhase int

// Dialog states.
const (
	DialogNone DialogPhase = iota
	DialogEarly
	DialogConfirmed
	DialogTerminated
)

// String returns the dialog state name.
func (d DialogPhase) String() string {
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
