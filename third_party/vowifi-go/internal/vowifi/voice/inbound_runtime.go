package voice

import (
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// SetIncomingCallHandler installs the business callback for new IMS calls.
func (a *Agent) SetIncomingCallHandler(handler func(IncomingCall)) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.incomingHandler = handler
	a.mu.Unlock()
}

// IncomingCalls returns pending inbound calls for polling consumers.
func (a *Agent) IncomingCalls() []IncomingCall {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	calls := make([]*Call, 0, len(a.calls))
	for _, call := range a.calls {
		if call.CallDirection() == callstate.DirectionInbound && !call.IsTerminalState() {
			calls = append(calls, call)
		}
	}
	a.mu.RUnlock()
	result := make([]IncomingCall, 0, len(calls))
	for _, call := range calls {
		state := call.CallState()
		if state == callstate.StateRinging || state == callstate.StateEarlyMedia || state == callstate.StateConnected {
			result = append(result, call.incomingSnapshot())
		}
	}
	return result
}

// HandleInboundVoiceRequest routes real IMS dialog requests to the call owner.
func (a *Agent) HandleInboundVoiceRequest(request imscore.InboundVoiceRequest) (imscore.InboundVoiceResult, error) {
	if a == nil {
		return voiceResult(500), errors.New("voice: nil agent")
	}
	call := a.callByID(request.CallID)
	switch strings.ToUpper(strings.TrimSpace(request.Method)) {
	case "INVITE":
		return a.handleInboundInvite(request, call)
	case "BYE":
		return a.handleInboundBye(call)
	case "CANCEL":
		return a.handleInboundCancel(request, call)
	case "ACK":
		if call != nil {
			call.MarkACKSent()
		}
		return voiceResult(0), nil
	case "PRACK":
		if call == nil {
			return voiceResult(481), nil
		}
		call.MarkReliableProvisional(call.Timers.RSeq)
		return voiceResult(200), nil
	case "UPDATE":
		return a.handleInboundUpdate(request, call)
	default:
		return imscore.InboundVoiceResult{}, nil
	}
}

func (a *Agent) handleInboundInvite(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	if call != nil {
		if call.CallDirection() == callstate.DirectionInbound && call.CallState() == callstate.StateRinging {
			a.maybeStartInboundClient(call)
			return voiceResult(0), nil
		}
		return a.handleReinvite(request, call)
	}
	if strings.TrimSpace(request.CallID) == "" || voiceHeaderURI(request.From) == "" || voiceHeaderURI(request.To) == "" {
		return voiceResult(400), nil
	}
	if !isVoiceSDPContentType(request.ContentType) {
		return voiceResult(415), nil
	}
	if request.Responder == nil && request.ServerInvite == nil {
		return voiceResult(500), errors.New("voice: inbound INVITE reply path is unavailable")
	}
	var created bool
	var err error
	call, created, err = a.reserveInboundCall(request)
	if err != nil {
		return voiceResult(486), nil
	}
	if !created {
		a.maybeStartInboundClient(call)
		return voiceResult(0), nil
	}
	status, err := a.beginInboundInvite(call, request)
	if status != 0 || err != nil {
		return voiceResult(status), err
	}
	a.emitIncomingCall(call)
	a.emitCallRinging(call)
	a.notifyIncomingCall(call)
	a.maybeStartInboundClient(call)
	return voiceResult(0), nil
}

func (a *Agent) beginInboundInvite(call *Call, request imscore.InboundVoiceRequest) (int, error) {
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	call.SetStartTime(time.Now())
	call.setInboundRequest(request.Responder)
	call.setServerInvite(request.ServerInvite, request.Request)
	if call.ClientCallID() == "" {
		call.SetClientCallID(call.CallID() + "-" + voiceHex(16))
	}
	call.applyVoiceSessionExpires(request.SessionExpires)
	if request.ServerInvite != nil {
		if _, err := a.rejectStoredServerInvite(call, 100); err != nil {
			a.releaseInboundCall(call, err, false)
			return 0, err
		}
	}
	if err := a.prepareInboundVoiceDialog(call, request); err != nil {
		a.releaseInboundCall(call, err, false)
		return 500, err
	}
	if err := a.prepareInboundMedia(call, string(request.Body)); err != nil {
		a.releaseInboundCall(call, err, false)
		return 488, nil
	}
	if err := a.respondInboundProvisional(call, 180); err != nil {
		a.releaseInboundCall(call, err, false)
		return 0, err
	}
	call.markInboundPrepared()
	a.startInboundNoAnswerTimer(call)
	return 0, nil
}

func (a *Agent) respondInboundProvisional(call *Call, status int) error {
	if call == nil {
		return errInboundCallUnavailable
	}
	return a.sendStatusResponseResult(status, imscore.SIPStatusText(status), call)
}

func (a *Agent) handleInboundCancel(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	if call == nil {
		return voiceResult(481), nil
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.CallState() != callstate.StateRinging {
		return voiceResult(481), nil
	}
	responder := call.inboundResponseWriter()
	if responder == nil && !call.hasServerInvite() {
		return voiceResult(500), errors.New("voice: inbound INVITE response context is unavailable")
	}
	if request.Responder == nil {
		return voiceResult(500), errors.New("voice: CANCEL reply path is unavailable")
	}
	cancelErr := request.Responder.Respond(imscore.InboundVoiceResponse{
		StatusCode: 200, ToTag: call.inboundLocalTagValue(),
	})
	structured, inviteErr := a.rejectStoredServerInvite(call, 487)
	if !structured && responder != nil {
		inviteErr = responder.Respond(imscore.InboundVoiceResponse{StatusCode: 487})
	}
	clientErr := a.sendClientCancel(call)
	a.releaseInboundCall(call, errors.New("voice: call canceled by IMS"), true)
	return voiceResult(0), errors.Join(cancelErr, inviteErr, clientErr)
}

func (a *Agent) handleInboundUpdate(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	if call == nil {
		return voiceResult(481), nil
	}
	if len(request.Body) > 0 {
		return a.handleReinvite(request, call)
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.CallState() != callstate.StateConnected {
		return voiceResult(491), nil
	}
	call.applyVoiceSessionExpires(request.SessionExpires)
	if err := a.applyIMSUpdate(call); err != nil {
		return voiceResult(200), err
	}
	a.startVoiceSessionTimer(call)
	return voiceResult(200), nil
}

func (a *Agent) handleReinvite(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.CallState() != callstate.StateConnected {
		return voiceResult(491), nil
	}
	call.applyVoiceSessionExpires(request.SessionExpires)
	if len(request.Body) == 0 {
		if err := a.applyIMSUpdate(call); err != nil {
			return voiceResult(200), err
		}
		a.startVoiceSessionTimer(call)
		return voiceResult(200), nil
	}
	if request.Responder == nil || !isVoiceSDPContentType(request.ContentType) {
		return voiceResult(488), nil
	}
	relay := call.RTPRelay()
	clientAnswer, imsAnswer := call.localSDPs()
	if relay == nil || clientAnswer == "" || imsAnswer == "" {
		return voiceResult(488), nil
	}
	if err := validateSDPMediaEndpoint(request.Body, "IMS re-INVITE"); err != nil {
		return voiceResult(488), nil
	}
	rewritten, err := ProcessIncomingIMSSDP(call, request.Body, clientRelayIP)
	if err != nil {
		return voiceResult(488), nil
	}
	call.setRemoteSDP(string(request.Body), string(rewritten))
	if err := request.Responder.Respond(a.voiceSDPResponse(call, 200, imsAnswer)); err != nil {
		return voiceResult(0), err
	}
	if err := a.applyIMSUpdate(call); err != nil {
		return voiceResult(0), err
	}
	a.startVoiceSessionTimer(call)
	a.notifyIncomingCall(call)
	return voiceResult(0), nil
}

func isVoiceSDPContentType(value string) bool {
	mediaType, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(mediaType) == "application/sdp"
}
