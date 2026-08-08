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
		if call.Direction() == callstate.DirectionInbound && !call.IsTerminalState() {
			calls = append(calls, call)
		}
	}
	a.mu.RUnlock()
	result := make([]IncomingCall, 0, len(calls))
	for _, call := range calls {
		state := call.GetState()
		if state == callstate.StateAlerting || state == callstate.StateConnecting || state == callstate.StateConnected {
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
		call.MarkReliableProvisional()
		return voiceResult(200), nil
	case "UPDATE":
		return a.handleInboundUpdate(request, call)
	default:
		return imscore.InboundVoiceResult{}, nil
	}
}

func (a *Agent) handleInboundInvite(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	if call != nil {
		return a.handleReinvite(request, call)
	}
	if strings.TrimSpace(request.CallID) == "" || voiceHeaderURI(request.From) == "" || voiceHeaderURI(request.To) == "" {
		return voiceResult(400), nil
	}
	if !isVoiceSDPContentType(request.ContentType) {
		return voiceResult(415), nil
	}
	if request.Responder == nil {
		return voiceResult(500), errors.New("voice: inbound INVITE reply path is unavailable")
	}
	var err error
	call, err = a.reserveInboundCall(request)
	if err != nil {
		return voiceResult(486), nil
	}
	status, err := a.beginInboundInvite(call, request)
	if status != 0 || err != nil {
		return voiceResult(status), err
	}
	a.emitIncomingCall(call)
	a.emitCallRinging(call)
	a.notifyIncomingCall(call)
	return voiceResult(0), nil
}

func (a *Agent) beginInboundInvite(call *Call, request imscore.InboundVoiceRequest) (int, error) {
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	call.SetStartTime(time.Now())
	call.setInboundRequest(request.Responder)
	if err := a.prepareInboundVoiceDialog(call, request); err != nil {
		a.releaseInboundCall(call, err, false)
		return 500, err
	}
	if err := a.prepareInboundMedia(call, string(request.Body)); err != nil {
		a.releaseInboundCall(call, err, false)
		return 488, nil
	}
	if err := request.Responder.Respond(imscore.InboundVoiceResponse{StatusCode: 180}); err != nil {
		a.releaseInboundCall(call, err, false)
		return 0, err
	}
	a.startInboundNoAnswerTimer(call)
	return 0, nil
}

func (a *Agent) handleInboundCancel(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	if call == nil {
		return voiceResult(481), nil
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.GetState() != callstate.StateAlerting {
		return voiceResult(481), nil
	}
	responder := call.inboundResponseWriter()
	if responder == nil {
		return voiceResult(500), errors.New("voice: inbound INVITE response context is unavailable")
	}
	if request.Responder == nil {
		return voiceResult(500), errors.New("voice: CANCEL reply path is unavailable")
	}
	cancelErr := request.Responder.Respond(imscore.InboundVoiceResponse{
		StatusCode: 200, ToTag: responder.LocalTag(),
	})
	inviteErr := responder.Respond(imscore.InboundVoiceResponse{StatusCode: 487})
	a.releaseInboundCall(call, errors.New("voice: call canceled by IMS"), true)
	return voiceResult(0), errors.Join(cancelErr, inviteErr)
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
	if call.GetState() != callstate.StateConnected {
		return voiceResult(491), nil
	}
	return voiceResult(200), a.applyIMSUpdate(call)
}

func (a *Agent) handleReinvite(request imscore.InboundVoiceRequest, call *Call) (imscore.InboundVoiceResult, error) {
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.GetState() != callstate.StateConnected {
		return voiceResult(491), nil
	}
	if len(request.Body) == 0 {
		return voiceResult(200), a.applyIMSUpdate(call)
	}
	if request.Responder == nil || !isVoiceSDPContentType(request.ContentType) {
		return voiceResult(488), nil
	}
	offer, err := ProcessIncomingIMSSDP(string(request.Body))
	if err != nil {
		return voiceResult(488), nil
	}
	remote, err := mediaRemote(offer)
	if err != nil {
		return voiceResult(488), nil
	}
	relay := call.RTPRelay()
	clientAnswer, imsAnswer := call.localSDPs()
	if relay == nil || clientAnswer == "" || imsAnswer == "" {
		return voiceResult(488), nil
	}
	parsedClientAnswer, err := ProcessOutgoingClientSDP(clientAnswer)
	if err != nil {
		return voiceResult(488), nil
	}
	relay.SetRemoteAddr(remote)
	relay.SetPTMapping(ExtractAndApplyPTMapping(offer, parsedClientAnswer))
	call.setRemoteSDP(string(request.Body), RewriteSDP(string(request.Body), clientRelayIP, relay.LANPort()))
	if err := request.Responder.Respond(a.voiceSDPResponse(200, imsAnswer)); err != nil {
		return voiceResult(0), err
	}
	if err := a.applyIMSUpdate(call); err != nil {
		return voiceResult(0), err
	}
	a.notifyIncomingCall(call)
	return voiceResult(0), nil
}

func isVoiceSDPContentType(value string) bool {
	mediaType, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(mediaType) == "application/sdp"
}
