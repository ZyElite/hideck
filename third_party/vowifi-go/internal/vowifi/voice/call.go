package voice

import (
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

// NewCall creates a call owned by the agent.
func NewCall(agent *Agent, direction callstate.Direction, callID, peer string) *Call {
	return &Call{
		agent:     agent,
		state:     callstate.StateIdle,
		direction: direction,
		callID:    callID,
		peer:      peer,
	}
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

// Direction returns the call direction.
func (c *Call) Direction() callstate.Direction {
	if c == nil {
		return callstate.DirectionOutbound
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.direction
}

// GetState returns the current call state.
func (c *Call) GetState() callstate.State {
	if c == nil {
		return callstate.StateIdle
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Transition moves the call to the next state, validating the transition.
func (c *Call) Transition(to callstate.State) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !callstate.CanTransition(c.state, to) {
		return &StateTransitionError{From: c.state, To: to}
	}
	c.state = to
	if callstate.IsTerminal(to) && c.endTime.IsZero() {
		c.endTime = time.Now()
	}
	return nil
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
	return callstate.IsTerminal(c.GetState())
}

// IsConnected reports whether the call is connected (media active).
func (c *Call) IsConnected() bool {
	return c.GetState() == callstate.StateConnected
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

// Duration returns the call duration.
func (c *Call) Duration() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.startTime.IsZero() {
		return 0
	}
	end := c.endTime
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(c.startTime)
}

// SetIMSDialog attaches the IMS dialog handle.
func (c *Call) SetIMSDialog(h *imscore.DialogHandle) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.imsDialog = h
	c.mu.Unlock()
}

// IMSDialog returns the IMS dialog handle.
func (c *Call) IMSDialog() *imscore.DialogHandle {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imsDialog
}

// SetIMSInviteHandle attaches the IMS invite handle.
func (c *Call) SetIMSInviteHandle(h *imscore.InviteHandle) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.imsInvite = h
	c.mu.Unlock()
}

// IMSInviteHandle returns the IMS invite handle.
func (c *Call) IMSInviteHandle() *imscore.InviteHandle {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.imsInvite
}

// SetRouteSet sets the dialog route set.
func (c *Call) SetRouteSet(route []string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.routeSet = append([]string{}, route...)
	c.mu.Unlock()
}

// RouteSet returns the dialog route set.
func (c *Call) RouteSet() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string{}, c.routeSet...)
}

// SetRTPRelay attaches the media relay.
func (c *Call) SetRTPRelay(r *media.RTPRelay) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.rtpRelay = r
	c.mu.Unlock()
}

// RTPRelay returns the media relay.
func (c *Call) RTPRelay() *media.RTPRelay {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rtpRelay
}

// MarkACKSent records that the ACK was sent.
func (c *Call) MarkACKSent() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ackSent = true
	c.mu.Unlock()
}

// IsACKSent reports whether the ACK was sent.
func (c *Call) IsACKSent() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ackSent
}

// MarkInviteFinalSeen records that the INVITE final response was seen.
func (c *Call) MarkInviteFinalSeen() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.inviteFinalSeen = true
	c.mu.Unlock()
}

// HasInviteFinalSeen reports whether the INVITE final was seen.
func (c *Call) HasInviteFinalSeen() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inviteFinalSeen
}

// MarkInviteProvisional records that a provisional INVITE response was seen.
func (c *Call) MarkInviteProvisional() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.inviteProvisional = true
	c.mu.Unlock()
}

// HasInviteProvisional reports whether a provisional was seen.
func (c *Call) HasInviteProvisional() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.inviteProvisional
}

// MarkLocalCancelSent records that a local CANCEL was sent.
func (c *Call) MarkLocalCancelSent() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.localCancelSent = true
	c.mu.Unlock()
}

// HasLocalCancelSent reports whether a local CANCEL was sent.
func (c *Call) HasLocalCancelSent() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.localCancelSent
}

// MarkReliableProvisional records that a reliable provisional (100rel) was used.
func (c *Call) MarkReliableProvisional() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.reliableProvisional = true
	c.mu.Unlock()
}

// SetOutboundCancelReason records the outbound cancel reason.
func (c *Call) SetOutboundCancelReason(reason string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.outboundCancelReason = reason
	c.mu.Unlock()
}

// OutboundCancelReason returns the outbound cancel reason.
func (c *Call) OutboundCancelReason() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.outboundCancelReason
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
		State:     c.state.String(),
		Direction: c.direction.String(),
		Peer:      c.peer,
		StartTime: c.startTime,
		EndTime:   c.endTime,
		Duration:  c.Duration(),
	}
}
