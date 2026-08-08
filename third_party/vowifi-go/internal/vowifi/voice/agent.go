package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

const (
	voiceInviteTimeout         = 45 * time.Second
	voiceHangupTimeout         = 10 * time.Second
	defaultVoiceSessionRefresh = 30 * time.Minute
)

// NewAgent creates a voice agent for a device.
func NewAgent(deviceID string, ims *imscore.Service, bus *imscore.EventBus) *Agent {
	if bus == nil {
		bus = imscore.NewEventBus()
	}
	return &Agent{
		deviceID:      deviceID,
		ims:           ims,
		bus:           bus,
		actor:         callstate.NewActor(),
		calls:         make(map[string]*Call),
		newMediaRelay: newVoiceMediaRelay,
	}
}

// DeviceID returns the device ID.
func (a *Agent) DeviceID() string {
	if a == nil {
		return ""
	}
	return a.deviceID
}

// Start launches the agent's actor and subscribes to IMS events.
func (a *Agent) Start() error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return nil
	}
	a.started = true
	a.mu.Unlock()
	a.actor.Start(context.Background())
	return nil
}

// Stop shuts the agent down.
func (a *Agent) Stop() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return nil
	}
	a.started = false
	var activeCall *Call
	if a.activeCall != nil && !a.activeCall.IsTerminalState() {
		activeCall = a.activeCall
	}
	a.mu.Unlock()
	var stopErr error
	if activeCall != nil {
		ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
		if err := a.hangupCall(ctx, activeCall); err != nil {
			stopErr = errors.Join(stopErr, err)
			a.forceReleaseCall(activeCall, err)
		}
		cancel()
	}
	a.actor.Stop()
	return stopErr
}

// SetNotifier wires the event notifier callback.
func (a *Agent) SetNotifier(fn func(events.Event)) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.notifier = fn
	a.mu.Unlock()
}

// OnIMSEvent handles events published on the IMS event bus.
func (a *Agent) OnIMSEvent(ev events.Event) {
	if a == nil || ev == nil {
		return
	}
	switch ev.(type) {
	case *events.EventIncomingCall, *events.EventCallRinging,
		*events.EventCallAnswered, *events.EventCallEnded,
		*events.EventCallFailed, *events.EventCallCanceled,
		*events.EventCallMediaUpdated:
		a.notify(ev)
	}
}

// Dial places an outbound call to the given number.
func (a *Agent) Dial(number string) (*Call, error) {
	ctx, cancel := context.WithTimeout(context.Background(), voiceInviteTimeout)
	defer cancel()
	return a.DialContext(ctx, number)
}

// DialContext starts an outbound call and waits for the final INVITE response.
func (a *Agent) DialContext(ctx context.Context, number string) (*Call, error) {
	return nil, errors.New("voice: client SDP is required; use HandleClientInvite")
}

func (a *Agent) dialContext(ctx context.Context, number, sdp string) (*Call, error) {
	if a == nil {
		return nil, errors.New("voice: nil agent")
	}
	if a.ims == nil {
		return nil, errors.New("voice: no IMS service")
	}
	if !a.ims.IsRegistered() {
		return nil, errors.New("voice: IMS not registered")
	}
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	if !started {
		return nil, errors.New("voice: agent not started")
	}
	call, err := a.startOutboundCall(number)
	if err != nil {
		return nil, err
	}
	imsOffer, err := a.prepareOutboundMedia(call, sdp)
	if err != nil {
		return nil, a.failOutboundCall(call, err)
	}
	invite := buildIMSInviteWithSDP(a, call, imsOffer)
	response, err := a.ims.RoundTripSIP(ctx, invite)
	if err != nil {
		return nil, a.failOutboundCall(call, fmt.Errorf("voice: INVITE transaction failed: %w", err))
	}
	if err := a.completeOutboundInvite(call, response); err != nil {
		return nil, a.failOutboundCall(call, err)
	}
	if err := a.completeOutboundMedia(call, response); err != nil {
		return nil, a.failEstablishedOutboundCall(ctx, call, err)
	}
	if err := call.Transition(callstate.StateConnected); err != nil {
		return nil, a.failEstablishedOutboundCall(ctx, call, err)
	}
	if err := call.StartSessionTimer(defaultVoiceSessionRefresh); err != nil {
		return nil, a.failEstablishedOutboundCall(ctx, call, err)
	}
	a.emitCallAnswered(call)
	return call, nil
}

func (a *Agent) startOutboundCall(number string) (*Call, error) {
	call := NewCall(a, callstate.DirectionOutbound, newVoiceCallID(), number)
	call.SetStartTime(time.Now())
	if err := a.prepareVoiceDialog(call, number); err != nil {
		return nil, err
	}
	if err := call.Transition(callstate.StateDialing); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeCall != nil && !a.activeCall.IsTerminalState() {
		return nil, errors.New("voice: busy")
	}
	a.calls[call.CallID()] = call
	a.activeCall = call
	return call, nil
}

func (a *Agent) completeOutboundInvite(call *Call, response imscore.SIPResponse) error {
	call.MarkInviteFinalSeen()
	call.learnVoiceDialog(response)
	if err := a.sendIMSDialogRequest(buildIMSACKForStatus(a, call, response.StatusCode)); err != nil {
		return fmt.Errorf("voice: send INVITE ACK: %w", err)
	}
	call.MarkACKSent()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: INVITE rejected: %d %s", response.StatusCode, response.Reason)
	}
	if err := call.Transition(callstate.StateConnecting); err != nil {
		return err
	}
	return nil
}

func (a *Agent) failEstablishedOutboundCall(ctx context.Context, call *Call, cause error) error {
	response, err := a.ims.RoundTripSIP(ctx, BuildIMSBye(a, call))
	if err != nil {
		cause = errors.Join(cause, fmt.Errorf("voice: cleanup BYE transaction failed: %w", err))
	} else if response.StatusCode < 200 || response.StatusCode >= 300 {
		cause = errors.Join(cause, fmt.Errorf("voice: cleanup BYE rejected: %d %s", response.StatusCode, response.Reason))
	}
	return a.failOutboundCall(call, cause)
}

// Answer rejects SDP-less answering so a call can never appear connected with
// an unusable media endpoint. Use AnswerWithSDP for inbound calls.
func (a *Agent) Answer(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	if call.Direction() != callstate.DirectionInbound {
		return errors.New("voice: not an inbound call")
	}
	return errors.New("voice: inbound answer requires client SDP")
}

// Hangup ends a call.
func (a *Agent) Hangup(callID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	return a.HangupContext(ctx, callID)
}

// HangupContext ends a live IMS dialog and waits for the network response.
func (a *Agent) HangupContext(ctx context.Context, callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	return a.hangupCall(ctx, call)
}

func (a *Agent) hangupCall(ctx context.Context, call *Call) error {
	if call == nil || call.IsTerminalState() {
		return nil
	}
	if call.Direction() == callstate.DirectionInbound {
		return a.hangupInboundCall(ctx, call)
	}
	if call.GetState() != callstate.StateConnected {
		if err := a.sendIMSDialogRequest(BuildIMSCancel(a, call)); err != nil {
			return fmt.Errorf("voice: send CANCEL: %w", err)
		}
		call.MarkLocalCancelSent()
		return a.finishLocalHangup(call)
	}
	response, err := a.ims.RoundTripSIP(ctx, BuildIMSBye(a, call))
	if err != nil {
		return fmt.Errorf("voice: BYE transaction failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: BYE rejected: %d %s", response.StatusCode, response.Reason)
	}
	return a.finishLocalHangup(call)
}

func (a *Agent) hangupInboundCall(ctx context.Context, call *Call) error {
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.IsTerminalState() {
		return nil
	}
	if call.GetState() != callstate.StateConnected {
		return a.rejectInboundCall(call, 486)
	}
	response, err := a.ims.RoundTripSIP(ctx, BuildIMSBye(a, call))
	if err != nil {
		return fmt.Errorf("voice: BYE transaction failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: BYE rejected: %d %s", response.StatusCode, response.Reason)
	}
	return a.finishLocalHangup(call)
}

func (a *Agent) finishLocalHangup(call *Call) error {
	if err := call.Transition(callstate.StateDisconnected); err != nil {
		return err
	}
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	a.emitCallEnded(call)
	a.finalizeActiveCall(call)
	return nil
}

func (a *Agent) failOutboundCall(call *Call, cause error) error {
	_ = call.Transition(callstate.StateFailed)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	a.emitCallFailed(call, cause.Error())
	a.finalizeActiveCall(call)
	return cause
}

func (a *Agent) forceReleaseCall(call *Call, cause error) {
	if call == nil {
		return
	}
	_ = call.Transition(callstate.StateFailed)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	if cause != nil {
		a.emitCallFailed(call, cause.Error())
	}
	a.finalizeActiveCall(call)
}

func (a *Agent) refreshVoiceSession(ctx context.Context, call *Call) error {
	response, err := a.ims.RoundTripSIP(ctx, buildIMSReinvite(a, call))
	if err != nil {
		return fmt.Errorf("voice: session refresh failed: %w", err)
	}
	call.learnVoiceDialog(response)
	if err := a.sendIMSDialogRequest(buildIMSACKForStatus(a, call, response.StatusCode)); err != nil {
		return fmt.Errorf("voice: send refresh ACK: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: session refresh rejected: %d %s", response.StatusCode, response.Reason)
	}
	return a.updateRemoteMedia(call, response)
}

// Ready reports whether the agent can start an IMS voice transaction.
func (a *Agent) Ready() bool {
	if a == nil || a.ims == nil {
		return false
	}
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	return started && a.ims.IsRegistered()
}

func (a *Agent) callByID(callID string) *Call {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.calls[strings.TrimSpace(callID)]
}

// IsBusy reports whether the agent has an active call.
func (a *Agent) IsBusy() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeCall != nil && !a.activeCall.IsTerminalState()
}

// GetCallByClientCallID returns a call by its client-side call ID.
func (a *Agent) GetCallByClientCallID(clientCallID string) *Call {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.calls {
		if c.ClientCallID() == clientCallID {
			return c
		}
	}
	return nil
}

// ActiveCall returns the active call.
func (a *Agent) ActiveCall() *Call {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeCall
}

// Snapshot returns a point-in-time view of the agent.
func (a *Agent) Snapshot() *AgentSnapshot {
	if a == nil {
		return &AgentSnapshot{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	snap := &AgentSnapshot{
		DeviceID: a.deviceID,
		Busy:     a.activeCall != nil && !a.activeCall.IsTerminalState(),
	}
	if a.activeCall != nil {
		snap.ActiveCall = a.activeCall.Snapshot()
	}
	for _, c := range a.calls {
		snap.Calls = append(snap.Calls, c.Snapshot())
	}
	return snap
}

// emitCallRinging publishes the CallRinging event.
func (a *Agent) emitCallRinging(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallRinging{DevID: a.deviceID, CallID: c.CallID(), Time: time.Now()})
}

// emitCallAnswered publishes the CallAnswered event.
func (a *Agent) emitCallAnswered(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallAnswered{DevID: a.deviceID, CallID: c.CallID(), Time: time.Now()})
}

// emitCallEnded publishes the CallEnded event.
func (a *Agent) emitCallEnded(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallEnded{DevID: a.deviceID, CallID: c.CallID(), Time: time.Now()})
}

// emitCallFailed publishes the CallFailed event.
func (a *Agent) emitCallFailed(c *Call, reason string) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallFailed{DevID: a.deviceID, CallID: c.CallID(), Reason: reason, Time: time.Now()})
}

// emitCallCanceled publishes the CallCanceled event.
func (a *Agent) emitCallCanceled(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallCanceled{DevID: a.deviceID, CallID: c.CallID(), Time: time.Now()})
}

// emitCallMediaUpdated publishes the CallMediaUpdated event.
func (a *Agent) emitCallMediaUpdated(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallMediaUpdated{DevID: a.deviceID, CallID: c.CallID(), Time: time.Now()})
}

// emitIncomingCall publishes the IncomingCall event.
func (a *Agent) emitIncomingCall(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventIncomingCall{DevID: a.deviceID, CallID: c.CallID(), Caller: c.Peer(), Callee: a.deviceID, Time: time.Now()})
}

// emit publishes a locally-created event and notifies the local callback.
func (a *Agent) emit(ev events.Event) {
	if a == nil {
		return
	}
	if a.bus != nil {
		a.bus.Publish(ev)
	}
	a.notify(ev)
}

func (a *Agent) notify(ev events.Event) {
	if a == nil || ev == nil {
		return
	}
	a.mu.RLock()
	fn := a.notifier
	a.mu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}

// sendIMSDialogRequest sends a SIP request through the IMS service.
func (a *Agent) sendIMSDialogRequest(req string) error {
	if a == nil || a.ims == nil {
		return errors.New("voice: no IMS service")
	}
	return a.ims.SendRawSIP(req)
}

// finalizeActiveCall clears the active call when it reaches a terminal state.
func (a *Agent) finalizeActiveCall(call *Call) {
	if a == nil || call == nil {
		return
	}
	a.mu.Lock()
	if a.activeCall == call {
		a.activeCall = nil
	}
	a.mu.Unlock()
}

// register registers the device with the IMS network.
func (a *Agent) register() error {
	if a == nil || a.ims == nil {
		return errors.New("voice: no IMS service")
	}
	return a.ims.Register(context.Background())
}

// unregister deregisters the device.
func (a *Agent) unregister() error {
	if a == nil || a.ims == nil {
		return errors.New("voice: no IMS service")
	}
	return a.ims.Unregister(context.Background())
}

// deviceStatus returns the device registration status.
func (a *Agent) deviceStatus() map[string]interface{} {
	if a == nil || a.ims == nil {
		return map[string]interface{}{"registered": false}
	}
	return map[string]interface{}{
		"registered": a.ims.IsRegistered(),
		"reg_state":  a.ims.RegState(),
		"device_id":  a.deviceID,
	}
}

// SimulateCall preserves the public API while using the real IMS transaction.
func (a *Agent) SimulateCall(number string) (*Call, error) {
	return a.Dial(number)
}

// simulateCall preserves the recovered private symbol without bypassing IMS.
func (a *Agent) simulateCall(number string) (*Call, error) {
	return a.Dial(number)
}

// newVoiceCallID generates a call ID.
func newVoiceCallID() string {
	return fmt.Sprintf("call-%s-%d", randomVoiceHex(8), time.Now().UnixNano()%100000)
}

// randomVoiceHex generates a hex string of n random bytes.
func randomVoiceHex(n int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = randVoiceRead(b)
	for i := range b {
		b[i] = digits[int(b[i])%16]
	}
	return string(b)
}
