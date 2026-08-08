package voice

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// AnswerWithSDP answers an inbound call using the local client's real RTP endpoint.
func (a *Agent) AnswerWithSDP(callID, clientSDP string) (InboundAnswer, error) {
	call := a.callByID(callID)
	if call == nil {
		return InboundAnswer{}, errors.New("voice: call not found")
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.Direction() != callstate.DirectionInbound || call.GetState() != callstate.StateAlerting {
		return InboundAnswer{}, errors.New("voice: inbound call is not alerting")
	}
	responder := call.inboundResponseWriter()
	if responder == nil {
		return InboundAnswer{}, errors.New("voice: inbound INVITE response context is unavailable")
	}
	answer, err := a.applyInboundAnswer(call, clientSDP)
	if err != nil {
		return InboundAnswer{}, err
	}
	if err := call.StartMedia(); err != nil {
		a.releaseInboundCall(call, err, false)
		return InboundAnswer{}, err
	}
	_, imsAnswer := call.localSDPs()
	if err := responder.Respond(a.voiceSDPResponse(call, 200, imsAnswer)); err != nil {
		a.releaseInboundCall(call, err, false)
		return InboundAnswer{}, err
	}
	_ = call.StopOutboundNoAnswerTimer()
	if err := transitionInboundConnected(call); err != nil {
		a.releaseInboundCall(call, err, false)
		return InboundAnswer{}, err
	}
	if err := call.StartSessionTimer(call.voiceSessionExpires()); err != nil {
		a.releaseInboundCall(call, err, false)
		return InboundAnswer{}, err
	}
	answer.State = call.GetState().String()
	a.emitCallAnswered(call)
	return answer, nil
}

// Reject sends a final failure response for a pending inbound call.
func (a *Agent) Reject(callID string, statusCode int) error {
	call := a.callByID(callID)
	if call == nil {
		return errors.New("voice: call not found")
	}
	if statusCode < 300 || statusCode > 699 {
		return fmt.Errorf("voice: reject status must be 300-699, got %d", statusCode)
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	return a.rejectInboundCall(call, statusCode)
}

func (a *Agent) rejectInboundCall(call *Call, statusCode int) error {
	if call.Direction() != callstate.DirectionInbound {
		return errors.New("voice: call is not inbound")
	}
	if call.GetState() != callstate.StateAlerting {
		return errors.New("voice: inbound call is not alerting")
	}
	responder := call.inboundResponseWriter()
	if responder == nil {
		return errors.New("voice: inbound INVITE response context is unavailable")
	}
	if err := responder.Respond(imscore.InboundVoiceResponse{StatusCode: statusCode}); err != nil {
		return err
	}
	a.releaseInboundCall(call, fmt.Errorf("voice: inbound call rejected with %d", statusCode), false)
	return nil
}

func (a *Agent) voiceSDPResponse(call *Call, status int, sdp string) imscore.InboundVoiceResponse {
	response := imscore.InboundVoiceResponse{StatusCode: status, ContentType: "application/sdp", Body: []byte(sdp)}
	if expires := call.voiceSessionExpires(); expires > 0 {
		response.SessionExpires = strconv.FormatInt(int64(expires/time.Second), 10)
	}
	if profile, err := a.ims.RegisteredSIPDialogProfile(); err == nil {
		response.Contact = profile.ContactURI
	}
	return response
}

func (a *Agent) notifyIncomingCall(call *Call) {
	a.mu.RLock()
	handler := a.incomingHandler
	a.mu.RUnlock()
	if handler != nil {
		handler(call.incomingSnapshot())
	}
}

func (a *Agent) startInboundNoAnswerTimer(call *Call) {
	call.mu.Lock()
	call.noAnswerTimer = time.AfterFunc(voiceInviteTimeout, func() {
		call.inboundDecisionMu.Lock()
		defer call.inboundDecisionMu.Unlock()
		if call.GetState() != callstate.StateAlerting {
			return
		}
		cause := a.sendInboundTimeout(call)
		a.releaseInboundCall(call, cause, false)
	})
	call.mu.Unlock()
}

func (a *Agent) sendInboundTimeout(call *Call) error {
	cause := errors.New("voice: inbound call timed out")
	responder := call.inboundResponseWriter()
	if responder == nil {
		return cause
	}
	if err := responder.Respond(imscore.InboundVoiceResponse{StatusCode: 480}); err != nil {
		return errors.Join(cause, fmt.Errorf("send 480 response: %w", err))
	}
	return cause
}

func (a *Agent) releaseInboundCall(call *Call, cause error, canceled bool) {
	_ = call.Transition(callstate.StateFailed)
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	_ = call.CloseDone()
	if canceled {
		a.emitCallCanceled(call)
	} else if cause != nil {
		a.emitCallFailed(call, cause.Error())
	}
	_ = call.Transition(callstate.StateEnded)
	a.finalizeActiveCall(call)
}

func (a *Agent) handleInboundBye(call *Call) (imscore.InboundVoiceResult, error) {
	if call == nil {
		return voiceResult(481), nil
	}
	call.inboundDecisionMu.Lock()
	defer call.inboundDecisionMu.Unlock()
	if call.GetState() != callstate.StateConnected {
		return voiceResult(481), nil
	}
	return voiceResult(200), a.finishRemoteBye(call)
}

func transitionInboundConnected(call *Call) error {
	if err := call.Transition(callstate.StateConnecting); err != nil {
		return err
	}
	return call.Transition(callstate.StateConnected)
}

func voiceResult(status int) imscore.InboundVoiceResult {
	return imscore.InboundVoiceResult{Handled: true, StatusCode: status}
}
