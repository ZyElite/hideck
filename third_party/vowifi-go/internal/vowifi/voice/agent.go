package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/dialog"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

const (
	voiceInviteTimeout         = 30 * time.Second
	voiceHangupTimeout         = 10 * time.Second
	voiceActorEventLogInterval = 10 * time.Second
	inboundClientWaitTimeout   = 120 * time.Second
	inboundClientTxTimeout     = 32 * time.Second
)

// NewAgent creates a voice agent for a device.
func NewAgent(deviceID string, ims *imscore.Service, bus *imscore.EventBus) *Agent {
	if bus == nil {
		bus = imscore.NewEventBus()
	}
	agent := &Agent{
		deviceID: deviceID,
		ims:      ims,
		bus:      bus,
		actor: callstate.NewActorWithConfig(callstate.ActorConfig{
			DeviceID: deviceID,
		}),
		dialog:       dialog.NewController(deviceID, ims),
		clientBridge: client.NewBridge(deviceID, nil),
		calls:        make(map[string]*Call),
	}
	agent.newMediaRelay = func(localIP string) (*media.RTPRelay, error) {
		return newVoiceMediaRelay(imscorePacketListener{service: ims}, localIP)
	}
	return agent
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
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.actor.Start(a.ctx)
	if a.ims != nil {
		a.ims.SetVoiceRequestHandler(a)
		a.imsUnsubscribe = a.subscribeIMSEvents()
	}
	if a.clientAdapter != nil {
		a.clientBridge = client.NewBridge(a.deviceID, a.clientAdapter)
		a.clientBridge.Start(a.ctx)
	}
	a.started = true
	a.mu.Unlock()
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
	cancel := a.cancel
	a.cancel = nil
	a.ctx = nil
	clientBridge := a.clientBridge
	unsubscribe := a.imsUnsubscribe
	a.imsUnsubscribe = nil
	var activeCall *Call
	if a.activeCall != nil && !a.activeCall.IsTerminalState() {
		activeCall = a.activeCall
	}
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if unsubscribe != nil {
		unsubscribe()
	}
	if clientBridge != nil {
		clientBridge.Stop()
	}
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
	if a.ims != nil {
		a.ims.SetVoiceRequestHandler(nil)
	}
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
		a.notifyIMSEvent(ev)
	}
}

func (a *Agent) notifyIMSEvent(ev events.Event) {
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	if call := a.callForEvent(ev); call != nil && call.actor != nil &&
		call.actor.Enqueue("voice_event_"+ev.Type(), func() { a.notify(ev) }) {
		return
	}
	if a.actor != nil && a.actor.Enqueue("ims_event_"+ev.Type(), func() { a.notify(ev) }) {
		return
	}
	if started {
		logging.WarnRate("voice-actor-event-rejected:"+a.deviceID, voiceActorEventLogInterval,
			"voice actor rejected IMS event", "device", a.deviceID, "event", ev.Type())
	}
	// Preserve delivery while the agent is stopped or the bounded queue rejects
	// work. Rejection is surfaced above instead of silently losing the event.
	a.notify(ev)
}

func (a *Agent) callForEvent(ev events.Event) *Call {
	callID := voiceEventCallID(ev)
	if callID == "" {
		return nil
	}
	return a.callByID(callID)
}

func voiceEventCallID(ev events.Event) string {
	switch typed := ev.(type) {
	case *events.EventIncomingCall:
		return typed.CallID
	case *events.EventCallRinging:
		return typed.CallID
	case *events.EventCallAnswered:
		return typed.CallID
	case *events.EventCallEnded:
		return typed.CallID
	case *events.EventCallFailed:
		return typed.CallID
	case *events.EventCallCanceled:
		return typed.CallID
	case *events.EventCallMediaUpdated:
		return typed.CallID
	default:
		return voiceValueEventCallID(ev)
	}
}

func voiceValueEventCallID(ev events.Event) string {
	switch typed := ev.(type) {
	case events.EventIncomingCall:
		return typed.CallID
	case events.EventCallRinging:
		return typed.CallID
	case events.EventCallAnswered:
		return typed.CallID
	case events.EventCallEnded:
		return typed.CallID
	case events.EventCallFailed:
		return typed.CallID
	case events.EventCallCanceled:
		return typed.CallID
	case events.EventCallMediaUpdated:
		return typed.CallID
	default:
		return ""
	}
}

// Dial places an outbound call to the given number.
func (a *Agent) Dial(number string) (*Call, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(), voiceInviteTimeout+outboundCancelSettle,
	)
	defer cancel()
	return a.DialContext(ctx, number)
}

// DialContext starts an outbound call and waits for the final INVITE response.
func (a *Agent) DialContext(ctx context.Context, number string) (*Call, error) {
	return a.dialContext(ctx, number, "")
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
	runtimeCtx, cancelRuntime := context.WithCancel(ctx)
	call.SetOutboundRuntimeCancel(cancelRuntime)
	defer cancelRuntime()
	_, err = a.executeOutboundCall(runtimeCtx, call, sdp)
	if err != nil {
		return nil, err
	}
	return call, nil
}

func (a *Agent) executeOutboundCall(
	ctx context.Context,
	call *Call,
	sdp string,
) (imscore.SIPResponse, error) {
	imsOffer, err := a.prepareOutboundCallSDP(call, sdp)
	if err != nil {
		return imscore.SIPResponse{}, a.handleOutboundInviteRuntimeError(call, err)
	}
	invite, err := buildIMSInviteWithSDPChecked(a, call, imsOffer)
	if err != nil {
		return imscore.SIPResponse{}, a.handleOutboundInviteRuntimeError(call, err)
	}
	call.setOutboundInvite(invite)
	if err := call.StartOutboundNoAnswerTimer(voiceInviteTimeout); err != nil {
		return imscore.SIPResponse{}, a.handleOutboundInviteRuntimeError(call, err)
	}
	logging.RunDebug("IMS INVITE outbound", "sip", logging.RedactSIPRaw(invite))
	response, err := a.startVoiceClientInvite(ctx, call, invite)
	_ = call.StopOutboundNoAnswerTimer()
	if response.StatusCode >= 200 {
		logOutboundInviteResponse("IMS INVITE 最终响应", response)
		return response, errors.Join(
			a.handleOutboundInviteRuntimeResponse(ctx, call, response), err,
		)
	}
	if err != nil {
		return response, a.handleOutboundInviteRuntimeError(
			call, fmt.Errorf("voice: INVITE transaction failed: %w", err),
		)
	}
	return response, a.handleOutboundInviteRuntimeError(
		call, errors.New("voice: INVITE transaction ended without a final response"),
	)
}

func (a *Agent) startOutboundCall(number string) (*Call, error) {
	call := NewCall(a, callstate.DirectionOutbound, newVoiceCallID(), number)
	call.SetStartTime(time.Now())
	if err := a.prepareVoiceDialog(call, number); err != nil {
		return nil, err
	}
	if err := call.TransitionChecked(callstate.StateDialing); err != nil {
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

func (a *Agent) completeOutboundInvite(ctx context.Context, call *Call, response imscore.SIPResponse) error {
	call.learnVoiceDialog(response)
	accepted := response.StatusCode >= 200 && response.StatusCode < 300
	if accepted {
		// A 2xx ACK is a dialog request. Non-2xx ACKs belong to the INVITE
		// client transaction and are emitted by imscore before RoundTrip returns.
		if _, err := a.sendCallDialogRequest(ctx, call, buildIMSACKForStatus(a, call, response.StatusCode)); err != nil {
			return fmt.Errorf("voice: send INVITE ACK: %w", err)
		}
	}
	call.MarkACKSent()
	if !accepted {
		call.MarkInviteFinalSeen()
		return fmt.Errorf("voice: INVITE rejected: %d %s", response.StatusCode, response.Reason)
	}
	call.applyVoiceSessionExpires(voiceResponseHeader(response.Headers, "Session-Expires"))
	if state := call.CallState(); state == callstate.StateDialing || state == callstate.StateAlerting {
		if err := call.TransitionChecked(callstate.StateConnecting); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) failEstablishedOutboundCall(ctx context.Context, call *Call, cause error) error {
	response, err := a.sendCallDialogRequest(ctx, call, BuildIMSBye(a, call))
	if err != nil {
		cause = errors.Join(cause, fmt.Errorf("voice: cleanup BYE transaction failed: %w", err))
	} else if response.StatusCode < 200 || response.StatusCode >= 300 {
		cause = errors.Join(cause, fmt.Errorf("voice: cleanup BYE rejected: %d %s", response.StatusCode, response.Reason))
	}
	if err := a.closeCallDialog(ctx, call); err != nil {
		cause = errors.Join(cause, fmt.Errorf("voice: cleanup dialog close failed: %w", err))
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
	if call.CallDirection() != callstate.DirectionInbound {
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
	err := a.hangupCall(ctx, call)
	if err != nil {
		a.forceReleaseCall(call, err)
	}
	return err
}

func (a *Agent) hangupCall(ctx context.Context, call *Call) error {
	if call == nil || call.IsTerminalState() {
		return nil
	}
	if call.CallDirection() == callstate.DirectionInbound {
		return a.hangupInboundCall(ctx, call)
	}
	if call.CallState() != callstate.StateConnected {
		if err := a.cancelVoiceClientInvite(ctx, call, "local_hangup"); err != nil {
			return fmt.Errorf("voice: send CANCEL: %w", err)
		}
		a.finishLocalCancel(call, "local_hangup")
		return nil
	}
	response, err := a.sendCallDialogRequest(ctx, call, BuildIMSBye(a, call))
	if err != nil {
		return fmt.Errorf("voice: BYE transaction failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: BYE rejected: %d %s", response.StatusCode, response.Reason)
	}
	if err := a.closeCallDialog(ctx, call); err != nil {
		return fmt.Errorf("voice: close dialog: %w", err)
	}
	return a.finishLocalHangup(call)
}

func (a *Agent) hangupInboundCall(ctx context.Context, call *Call) error {
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.IsTerminalState() {
		return nil
	}
	if call.CallState() != callstate.StateConnected {
		return a.rejectInboundCall(call, 486)
	}
	response, err := a.sendCallDialogRequest(ctx, call, BuildIMSBye(a, call))
	if err != nil {
		return fmt.Errorf("voice: BYE transaction failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: BYE rejected: %d %s", response.StatusCode, response.Reason)
	}
	if err := a.closeCallDialog(ctx, call); err != nil {
		return fmt.Errorf("voice: close dialog: %w", err)
	}
	return a.finishLocalHangup(call)
}

func (a *Agent) finishLocalHangup(call *Call) error {
	if err := call.TransitionChecked(callstate.StateDisconnected); err != nil {
		return err
	}
	if !call.claimTerminalFinalization() {
		return nil
	}
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	call.CloseDone()
	a.emitCallEnded(call, "local_hangup")
	a.finalizeActiveCall(call)
	return nil
}

func (a *Agent) finishLocalCancel(call *Call, reason string) {
	if call == nil || !call.claimTerminalFinalization() {
		return
	}
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	call.CloseDone()
	a.emitCallCanceled(call, reason)
	a.finalizeActiveCall(call)
}

func (a *Agent) failOutboundCall(call *Call, cause error) error {
	if call == nil || !call.claimTerminalFinalization() {
		return cause
	}
	_ = call.TransitionChecked(callstate.StateFailed)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = a.closeCallDialog(context.Background(), call)
	call.CloseDone()
	a.emitCallFailed(call, cause.Error())
	a.finalizeActiveCall(call)
	return cause
}

func (a *Agent) forceReleaseCall(call *Call, cause error) {
	if call == nil || !call.claimTerminalFinalization() {
		return
	}
	_ = call.TransitionChecked(callstate.StateFailed)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = a.closeCallDialog(context.Background(), call)
	call.CloseDone()
	if cause != nil {
		a.emitCallFailed(call, cause.Error())
	}
	a.finalizeActiveCall(call)
}

func (a *Agent) refreshVoiceSession(ctx context.Context, call *Call) error {
	response, err := a.sendCallDialogRequest(ctx, call, buildIMSSessionUpdate(a, call))
	if err != nil {
		return fmt.Errorf("voice: session refresh failed: %w", err)
	}
	call.learnVoiceDialog(response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: session refresh rejected: %d %s", response.StatusCode, response.Reason)
	}
	call.applyVoiceSessionExpires(voiceResponseHeader(response.Headers, "Session-Expires"))
	return nil
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
	answeredAt := time.Now()
	a.emit(events.EventCallAnswered{
		DevID: a.deviceID, CallID: c.CallID(), AnsweredAt: answeredAt, Time: answeredAt,
	})
}

// emitCallEnded publishes the CallEnded event.
func (a *Agent) emitCallEnded(c *Call, reason string) {
	if a == nil || c == nil {
		return
	}
	endedAt := time.Now()
	a.emit(events.EventCallEnded{
		DevID: a.deviceID, CallID: c.CallID(), Reason: strings.TrimSpace(reason),
		EndedAt: endedAt, Time: endedAt,
	})
}

// emitCallFailed publishes the CallFailed event.
func (a *Agent) emitCallFailed(c *Call, reason string) {
	if a == nil || c == nil {
		return
	}
	a.emit(&events.EventCallFailed{DevID: a.deviceID, CallID: c.CallID(), Reason: reason, Time: time.Now()})
}

// emitCallCanceled publishes the CallCanceled event.
func (a *Agent) emitCallCanceled(c *Call, reason string) {
	if a == nil || c == nil {
		return
	}
	a.emit(events.EventCallCanceled{
		DevID: a.deviceID, CallID: c.CallID(), Reason: strings.TrimSpace(reason), Time: time.Now(),
	})
}

// emitCallMediaUpdated publishes the CallMediaUpdated event.
func (a *Agent) emitCallMediaUpdated(c *Call) {
	if a == nil || c == nil {
		return
	}
	a.emit(events.EventCallMediaUpdated{
		DevID: a.deviceID, CallID: c.CallID(), Direction: c.CallDirection().String(),
		State: c.CallState().String(), Time: time.Now(),
	})
}

// emitIncomingCall publishes the IncomingCall event.
func (a *Agent) emitIncomingCall(c *Call) {
	if a == nil || c == nil {
		return
	}
	receivedAt := time.Now()
	a.emit(events.EventIncomingCall{
		DevID: a.deviceID, CallID: c.CallID(), Caller: c.Peer(), Callee: a.deviceID,
		ReceivedAt: receivedAt, Time: receivedAt,
	})
}

// emit publishes a locally-created event and notifies the local callback.
func (a *Agent) emit(ev events.Event) {
	if a == nil {
		return
	}
	if a.bus != nil {
		a.bus.Publish(ev)
	}
	a.notifyIMSEvent(ev)
}

func (a *Agent) notify(ev events.Event) {
	if a == nil || ev == nil {
		return
	}
	a.mu.RLock()
	fn := a.notifier
	a.mu.RUnlock()
	if fn != nil {
		a.notifierMu.Lock()
		defer a.notifierMu.Unlock()
		fn(ev)
	}
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

// SimulateCallNumber retains the additive direct-dial convenience API.
func (a *Agent) SimulateCallNumber(number string) (*Call, error) {
	return a.Dial(number)
}

// simulateCall preserves the recovered private symbol without bypassing IMS.
func (a *Agent) simulateCall(number string) (*Call, error) {
	return a.SimulateCallNumber(number)
}

// newVoiceCallID generates a call ID.
func newVoiceCallID() string {
	return "vohive-" + randomVoiceHex(32)
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
