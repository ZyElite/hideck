package voice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// NewCall creates a call owned by the agent.
func NewCall(agent *Agent, direction callstate.Direction, callID, peer string) *Call {
	deviceID := ""
	if agent != nil {
		deviceID = agent.DeviceID()
	}
	call := newCall(callInit{
		agent: agent, deviceID: deviceID, direction: direction,
		callID: callID, peer: peer, generateTrace: true,
	})
	call.startActor()
	return call
}

type callInit struct {
	agent         *Agent
	deviceID      string
	direction     callstate.Direction
	callID        string
	peer          string
	traceID       string
	generateTrace bool
}

func newCall(init callInit) *Call {
	ctx, cancel := context.WithCancel(context.Background())
	traceID := init.traceID
	if init.generateTrace && strings.TrimSpace(traceID) == "" {
		traceID = common.NewTraceID()
	}
	call := &Call{
		DeviceID: init.deviceID, Direction: int(init.direction), State: int(callstate.StateInit),
		TraceID: traceID, Done: make(chan struct{}), Ctx: ctx, Cancel: cancel,
		agent: init.agent, callID: init.callID, peer: init.peer,
	}
	call.DialogState.CallID = init.callID
	if init.direction == callstate.DirectionInbound {
		call.DialogState.CallerID = init.peer
	} else {
		call.DialogState.CalleeID = init.peer
	}
	return call
}

// CallID returns the call ID.
func (c *Call) CallID() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.callID
}

// ClientCallID returns the client-side call ID.
func (c *Call) ClientCallID() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientCallID
}

// SetClientCallID sets the client-side call ID.
func (c *Call) SetClientCallID(id string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.clientCallID = id
	c.DialogState.ClientCallID = id
	c.mu.Unlock()
}

// Peer returns the remote party.
func (c *Call) Peer() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.peer
}

// CallDirection returns the call direction without shadowing the recovered
// public Direction field.
func (c *Call) CallDirection() callstate.Direction {
	if c == nil {
		return callstate.DirectionOutbound
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return callstate.Direction(c.Direction)
}

// GetState retains the recovered integer state API.
func (c *Call) GetState() int {
	if c == nil {
		return int(callstate.StateInit)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.State
}

// CallState returns the typed additive state view.
func (c *Call) CallState() callstate.State { return callstate.State(c.GetState()) }

// Transition retains the recovered boolean state transition API.
func (c *Call) Transition(to int) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.transitionLocked(to)
}

func (c *Call) transitionLocked(to int) bool {
	from := callstate.State(c.State)
	next := callstate.State(to)
	if from == next {
		return true
	}
	if !callstate.CanTransition(from, next) {
		return false
	}
	c.State = to
	return true
}

func (c *Call) startActor() {
	if c == nil || c.Ctx == nil {
		return
	}
	c.mu.Lock()
	if c.actor == nil {
		c.actor = callstate.NewActorWithConfig(callstate.ActorConfig{DeviceID: c.DeviceID, TraceID: c.TraceID})
	}
	actor := c.actor
	c.mu.Unlock()
	actor.Start(c.Ctx)
}

// TransitionChecked preserves the additive error-returning transition API.
func (c *Call) TransitionChecked(to callstate.State) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	from := c.CallState()
	if c.Transition(int(to)) {
		return nil
	}
	return &StateTransitionError{From: from, To: to}
}

// StateTransitionError reports an invalid state transition.
type StateTransitionError struct {
	From callstate.State
	To   callstate.State
}

// Error implements error.
func (e *StateTransitionError) Error() string {
	return "voice: invalid state transition " + e.From.String() + " -> " + e.To.String()
}

// IsTerminalState reports whether the call is in a terminal state.
func (c *Call) IsTerminalState() bool {
	return callstate.IsTerminal(c.CallState())
}

// IsConnected reports whether the call is connected (media active).
func (c *Call) IsConnected() bool {
	return c.CallState() == callstate.StateConnected
}

// StartTime returns the call start time.
func (c *Call) StartTime() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.startTime
}

// SetStartTime records the call start time.
func (c *Call) SetStartTime(t time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.startTime = t
	c.mu.Unlock()
}

// EndTime returns the call end time.
func (c *Call) EndTime() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.endTime
}

// Duration retains the recovered nanosecond duration API.
func (c *Call) Duration() int64 { return int64(c.CallDuration()) }

// CallDuration returns the typed additive duration view.
func (c *Call) CallDuration() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.callDurationLocked(time.Now())
}

func (c *Call) callDurationLocked(now time.Time) time.Duration {
	if c.startTime.IsZero() {
		return 0
	}
	end := c.endTime
	if end.IsZero() {
		end = now
	}
	return end.Sub(c.startTime)
}

// Snapshot returns a point-in-time view of the call.
func (c *Call) Snapshot() *CallSnapshot {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &CallSnapshot{
		CallID:    c.callID,
		State:     callstate.State(c.State).String(),
		Direction: callstate.Direction(c.Direction).String(),
		Peer:      c.peer,
		StartTime: c.startTime,
		EndTime:   c.endTime,
		Duration:  c.callDurationLocked(time.Now()),
		ClientSDP: c.clientRemoteSDP,
	}
}
