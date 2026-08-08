package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// --- Call naming aliases (recovered API surface) ---

// GetStartTime returns the call start time.
func (c *Call) GetStartTime() time.Time { return c.StartTime() }

// GetEndTime returns the call end time.
func (c *Call) GetEndTime() time.Time { return c.EndTime() }

// IMSDialogValue returns the IMS dialog handle.
func (c *Call) IMSDialogValue() *imscore.DialogHandle { return c.IMSDialog() }

// IMSInviteHandleValue returns the IMS invite handle.
func (c *Call) IMSInviteHandleValue() *imscore.InviteHandle { return c.IMSInviteHandle() }

// LocalCancelReasonValue returns the outbound cancel reason.
func (c *Call) LocalCancelReasonValue() string { return c.OutboundCancelReason() }

// SetOutboundCancel records the outbound cancel reason.
func (c *Call) SetOutboundCancel(reason string) { c.SetOutboundCancelReason(reason) }

// SetOutboundRuntimeCancel records a runtime outbound cancel reason.
func (c *Call) SetOutboundRuntimeCancel(reason string) { c.SetOutboundCancelReason(reason) }

// MarkErrorACKSent records that an error ACK was sent.
func (c *Call) MarkErrorACKSent() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.ackSent = true
	c.mu.Unlock()
}

// CancelOutboundInviteTimer cancels the no-answer timer.
func (c *Call) CancelOutboundInviteTimer() error { return c.StopOutboundNoAnswerTimer() }

// --- Call constructors ---

// NewOutboundCall creates an outbound call in the Dialing state.
func NewOutboundCall(agent *Agent, number string) (*Call, error) {
	if agent == nil {
		return nil, errors.New("voice: nil agent")
	}
	call := NewCall(agent, callstate.DirectionOutbound, newVoiceCallID(), number)
	call.SetStartTime(time.Now())
	if err := call.Transition(callstate.StateDialing); err != nil {
		return nil, err
	}
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	return call, nil
}

// NewCallFromRequest creates a call from an inbound request.
func NewCallFromRequest(agent *Agent, peer, callID string) *Call {
	call := NewCall(agent, callstate.DirectionInbound, callID, peer)
	_ = call.Transition(callstate.StateAlerting)
	if agent != nil {
		agent.mu.Lock()
		agent.calls[callID] = call
		agent.activeCall = call
		agent.mu.Unlock()
	}
	return call
}

// NewCallFromClientInvite creates a call from a client-side INVITE.
func NewCallFromClientInvite(agent *Agent, peer, callID, clientCallID string) *Call {
	call := NewCall(agent, callstate.DirectionOutbound, callID, peer)
	call.SetClientCallID(clientCallID)
	_ = call.Transition(callstate.StateDialing)
	if agent != nil {
		agent.mu.Lock()
		agent.calls[callID] = call
		agent.activeCall = call
		agent.mu.Unlock()
	}
	return call
}

// --- SDP processing ---

// ProcessIncomingIMSSDP parses an SDP offer received from the IMS network.
func ProcessIncomingIMSSDP(sdp string) (*SDPInfo, error) {
	return ParseSDP(sdp)
}

// ProcessOutgoingClientSDP parses an SDP offer received from the local client.
func ProcessOutgoingClientSDP(sdp string) (*SDPInfo, error) {
	return ParseSDP(sdp)
}

// RewriteSDPForClient rewrites an SDP body for the local client.
func RewriteSDPForClient(sdp, ip string, port int) string {
	return RewriteSDP(sdp, ip, port)
}

// --- Client request handlers ---

// HandleClientInvite handles an inbound INVITE from the local client bridge.
func (a *Agent) HandleClientInvite(peer string, sdp string) (*Call, error) {
	if a == nil {
		return nil, errors.New("voice: nil agent")
	}
	if _, err := ProcessOutgoingClientSDP(sdp); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceInviteTimeout)
	defer cancel()
	return a.dialContext(ctx, peer, sdp)
}

// HandleClientBye handles a client BYE.
func (a *Agent) HandleClientBye(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	return call.Hangup()
}

// HandleClientCancel handles a client CANCEL.
func (a *Agent) HandleClientCancel(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	if call.GetState() == callstate.StateConnected {
		return errors.New("voice: connected call must be ended with BYE")
	}
	if err := a.sendIMSDialogRequest(BuildIMSCancel(a, call)); err != nil {
		return fmt.Errorf("voice: send CANCEL: %w", err)
	}
	call.MarkLocalCancelSent()
	call.SetOutboundCancelReason("local_cancel")
	if err := call.Transition(callstate.StateFailed); err != nil {
		return err
	}
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	a.emitCallCanceled(call)
	a.finalizeActiveCall(call)
	return nil
}

// HandleClientAck handles a client ACK.
func (a *Agent) HandleClientAck(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	call.MarkACKSent()
	return nil
}

// HandleClientPrack handles a client PRACK.
func (a *Agent) HandleClientPrack(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	return errors.New("voice: PRACK requires the reliable provisional response context")
}

// --- IMS event handlers ---

// OnIMSInvite handles an incoming INVITE from the IMS network.
func (a *Agent) OnIMSInvite(peer, callID string, sdp string) *Call {
	call := NewCallFromRequest(a, peer, callID)
	_, _ = ProcessIncomingIMSSDP(sdp)
	a.emitIncomingCall(call)
	a.emitCallRinging(call)
	return call
}

// OnIMSBye handles a BYE from the IMS network.
func (a *Agent) OnIMSBye(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.IsTerminalState() {
		return nil
	}
	return a.finishRemoteBye(call)
}

func (a *Agent) finishRemoteBye(call *Call) error {
	_ = call.Transition(callstate.StateDisconnected)
	_ = call.Transition(callstate.StateEnded)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	a.emitCallEnded(call)
	a.finalizeActiveCall(call)
	return nil
}

// OnIMSCancel handles a CANCEL from the IMS network.
func (a *Agent) OnIMSCancel(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	_ = call.Transition(callstate.StateFailed)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	a.emitCallCanceled(call)
	a.finalizeActiveCall(call)
	return nil
}

// OnIMSUpdate handles a re-INVITE/UPDATE from the IMS network.
func (a *Agent) OnIMSUpdate(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	return a.applyIMSUpdate(call)
}

func (a *Agent) applyIMSUpdate(call *Call) error {
	if call.GetState() != callstate.StateConnected {
		return errors.New("voice: call is not connected")
	}
	if err := call.Transition(callstate.StateConnecting); err != nil {
		return err
	}
	if err := call.Transition(callstate.StateConnected); err != nil {
		return err
	}
	a.emitCallMediaUpdated(call)
	return nil
}

// HandleIMSByeEvent handles an IMS BYE event.
func (a *Agent) HandleIMSByeEvent(callID string) error { return a.OnIMSBye(callID) }

// HandleIMSCancelEvent handles an IMS CANCEL event.
func (a *Agent) HandleIMSCancelEvent(callID string) error { return a.OnIMSCancel(callID) }

// HandleIMSUpdateEvent handles an IMS UPDATE event.
func (a *Agent) HandleIMSUpdateEvent(callID string) error { return a.OnIMSUpdate(callID) }

// HandleOutboundInvite verifies that the synchronous INVITE flow completed.
func (a *Agent) HandleOutboundInvite(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	call := a.callByID(callID)
	if call == nil {
		return errors.New("voice: call not found")
	}
	if !call.HasInviteFinalSeen() {
		return errors.New("voice: INVITE final response not received")
	}
	return nil
}

// HandleOutboundACK handles the outbound ACK for a 2xx response.
func (a *Agent) HandleOutboundACK(callID string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	a.mu.RLock()
	call := a.calls[callID]
	a.mu.RUnlock()
	if call == nil {
		return errors.New("voice: call not found")
	}
	if !call.IsACKSent() {
		return errors.New("voice: outbound ACK has not been sent")
	}
	return nil
}

// HandlePrack handles a PRACK transaction.
func (a *Agent) HandlePrack(callID string) error { return a.HandleClientPrack(callID) }

// HandleCancel handles a local cancel.
func (a *Agent) HandleCancel(callID string) error { return a.HandleClientCancel(callID) }

// --- Wiring ---

// ReplaceIMSProvider swaps the IMS service.
func (a *Agent) ReplaceIMSProvider(ims *imscore.Service) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.ims = ims
	a.mu.Unlock()
}

// SetClientAdapter wires the client adapter.
func (a *Agent) SetClientAdapter(adapter interface{}) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.clientAdapter = adapter
	a.mu.Unlock()
}

// GetClientAdapter returns the client adapter.
func (a *Agent) GetClientAdapter() interface{} {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.clientAdapter
}

// SetEventDispatcher wires the event dispatcher.
func (a *Agent) SetEventDispatcher(dispatcher interface{ Dispatch(interface{}) }) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.eventDispatcher = dispatcher
	a.mu.Unlock()
}

// BuildResponse builds a status response for the call.
func (c *Call) BuildResponse() string {
	return buildCallStatusResponse(c, 0, "")
}

// BuildResponseWithSDP builds a status response with an SDP body.
func (c *Call) BuildResponseWithSDP(status int, sdp string) string {
	return buildCallStatusResponse(c, status, sdp)
}

// buildCallStatusResponse builds a SIP status response for a call.
func buildCallStatusResponse(c *Call, status int, sdp string) string {
	if c == nil {
		return ""
	}
	if status == 0 {
		status = 200
	}
	var b strings.Builder
	cfg := c.agentIMSConfig()
	domain := cfg.Domain
	if domain == "" {
		domain = "ims.mnc000.mcc000.3gppnetwork.org"
	}
	b.WriteString(fmt.Sprintf("SIP/2.0 %d %s\r\n", status, imscore.SIPStatusText(status)))
	b.WriteString(fmt.Sprintf("Via: SIP/2.0/UDP %s;branch=z9hG4bK%s;rport\r\n", cfg.LocalAddr, voiceBranch()))
	b.WriteString(fmt.Sprintf("From: <sip:%s@%s>;tag=%s\r\n", cfg.IMPI, domain, voiceTag()))
	b.WriteString(fmt.Sprintf("To: <sip:%s@%s>;tag=%s\r\n", sanitizeVoicePhone(c.Peer()), domain, voiceTag()))
	b.WriteString(fmt.Sprintf("Call-ID: %s\r\n", c.CallID()))
	b.WriteString("CSeq: 1 INVITE\r\n")
	if sdp != "" {
		b.WriteString("Content-Type: application/sdp\r\n")
		b.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(sdp)))
		b.WriteString(sdp)
	} else {
		b.WriteString("Content-Length: 0\r\n\r\n")
	}
	return b.String()
}

// agentIMSConfig returns the agent's IMS config view.
func (c *Call) agentIMSConfig() *voiceIMSConfig {
	if c == nil || c.agent == nil {
		return &voiceIMSConfig{}
	}
	return &voiceIMSConfig{
		Domain:    c.agent.imsConfig().Domain,
		IMPI:      c.agent.imsConfig().IMPI,
		LocalAddr: c.agent.localAddr(),
	}
}

// voiceIMSConfig is the config view used by response builders.
type voiceIMSConfig struct {
	Domain    string
	IMPI      string
	LocalAddr string
}
