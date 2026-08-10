package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

// --- Call naming aliases (recovered API surface) ---

// GetStartTime returns the call start time.
func (c *Call) GetStartTime() time.Time { return c.StartTime() }

// GetEndTime returns the call end time.
func (c *Call) GetEndTime() time.Time { return c.EndTime() }

// IMSDialogValue returns the IMS dialog handle.
func (c *Call) IMSDialogValue() imsendpoint.DialogHandle {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DialogState.IMSDialog
}

// IMSInviteHandleValue returns the IMS invite handle.
func (c *Call) IMSInviteHandleValue() imsendpoint.InviteHandle {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DialogState.IMSInviteHandle
}

// LocalCancelReasonValue returns the outbound cancel reason.
func (c *Call) LocalCancelReasonValue() string { return c.OutboundCancelReason() }

// SetOutboundCancel retains the recovered cancel callback API.
func (c *Call) SetOutboundCancel(cancel func()) { c.SetOutboundRuntimeCancel(cancel) }

// SetOutboundRuntimeCancel stores the recovered runtime cancel callback.
func (c *Call) SetOutboundRuntimeCancel(cancel func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.outboundRuntimeCancel = cancel
	c.mu.Unlock()
}

// MarkErrorACKSent records that an error ACK was sent.
func (c *Call) MarkErrorACKSent() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.DialogState.ErrorACKSent {
		return false
	}
	c.DialogState.ErrorACKSent = true
	return true
}

// CancelOutboundInviteTimer cancels the no-answer timer.
func (c *Call) CancelOutboundInviteTimer() error { return c.StopOutboundNoAnswerTimer() }

// --- Call constructors ---

// NewOutboundCall retains the recovered call-ID/caller/callee constructor.
func NewOutboundCall(callID, callerID, calleeID string) *Call {
	call := newCall(callInit{
		direction: callstate.DirectionOutbound, callID: callID,
		peer: calleeID, traceID: strings.TrimSpace(callID),
	})
	call.DialogState.CallerID = callerID
	call.DialogState.CalleeID = calleeID
	call.startActor()
	return call
}

// NewOutboundCallForAgent preserves the additive Agent-owned constructor.
func NewOutboundCallForAgent(agent *Agent, number string) (*Call, error) {
	if agent == nil {
		return nil, errors.New("voice: nil agent")
	}
	call := NewCall(agent, callstate.DirectionOutbound, newVoiceCallID(), number)
	call.SetStartTime(time.Now())
	if err := call.TransitionChecked(callstate.StateDialing); err != nil {
		return nil, err
	}
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	return call, nil
}

// NewCallFromRequest retains the recovered inbound SIP constructor.
func NewCallFromRequest(deviceID string, request *sip.Request, session *imsendpoint.Session) *Call {
	call := newCall(callInit{deviceID: deviceID, direction: callstate.DirectionInbound})
	call.DialogState.IMSSession = session
	if request != nil {
		call.DialogState.OriginalRequest = request.Clone()
		call.parseInviteRequest(request)
	}
	call.callID = call.DialogState.CallID
	call.peer = call.DialogState.CallerID
	call.callee = call.DialogState.CalleeID
	call.TraceID = call.originalTraceID()
	call.startActor()
	return call
}

// NewCallFromRequestForAgent preserves the additive Agent-owned constructor.
func NewCallFromRequestForAgent(agent *Agent, peer, callID string) *Call {
	call := NewCall(agent, callstate.DirectionInbound, callID, peer)
	_ = call.TransitionChecked(callstate.StateAlerting)
	if agent != nil {
		agent.mu.Lock()
		agent.calls[callID] = call
		agent.activeCall = call
		agent.mu.Unlock()
	}
	return call
}

// NewCallFromClientInvite retains the recovered local SIP constructor.
func NewCallFromClientInvite(deviceID string, request *sip.Request) *Call {
	callID, callerID, calleeID := legacyRequestIdentity(request)
	call := NewOutboundCall(callID, callerID, calleeID)
	call.DeviceID = deviceID
	call.parseLegacyClientInvite(request)
	return call
}

// NewCallFromClientInviteForAgent preserves the additive Agent-owned constructor.
func NewCallFromClientInviteForAgent(agent *Agent, peer, callID, clientCallID string) *Call {
	call := NewCall(agent, callstate.DirectionOutbound, callID, peer)
	call.SetClientCallID(clientCallID)
	_ = call.TransitionChecked(callstate.StateDialing)
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
	if call.CallState() == callstate.StateConnected {
		return errors.New("voice: connected call must be ended with BYE")
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	if err := a.cancelVoiceClientInvite(ctx, call, "local_cancel"); err != nil {
		return fmt.Errorf("voice: send CANCEL: %w", err)
	}
	a.finishLocalCancel(call, call.OutboundCancelReason())
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
	clientErr := a.sendClientBye(call)
	_ = call.TransitionChecked(callstate.StateDisconnected)
	_ = call.TransitionChecked(callstate.StateEnded)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	closeErr := a.closeCallDialog(context.Background(), call)
	call.CloseDone()
	a.emitCallEnded(call, "remote_bye")
	a.finalizeActiveCall(call)
	return errors.Join(clientErr, closeErr)
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
	clientErr := a.sendClientCancel(call)
	a.releaseInboundCall(call, errors.New("voice: call canceled by IMS"), true)
	return clientErr
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
	if call.CallState() != callstate.StateConnected {
		return errors.New("voice: call is not connected")
	}
	if err := call.TransitionChecked(callstate.StateConnecting); err != nil {
		return err
	}
	if err := call.TransitionChecked(callstate.StateConnected); err != nil {
		return err
	}
	a.emitCallMediaUpdated(call)
	return nil
}

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

// SetClientAdapter wires the structured local SIP client adapter.
func (a *Agent) SetClientAdapter(adapter voiceclient.Adapter) {
	if a == nil {
		return
	}
	bridge := client.NewBridge(a.deviceID, adapter)
	a.mu.Lock()
	previous := a.clientBridge
	a.clientAdapter = adapter
	a.clientBridge = bridge
	if a.started && adapter != nil {
		bridge.Start(a.ctx)
	}
	a.mu.Unlock()
	if previous != nil {
		previous.Stop()
	}
}

// GetClientAdapter returns the client adapter.
func (a *Agent) GetClientAdapter() voiceclient.Adapter {
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

// BuildResponseText preserves the additive textual response helper.
func (c *Call) BuildResponseText(status int, sdp string) string {
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
