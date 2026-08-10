package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

func (a *Agent) prepareOutboundCallSDP(call *Call, clientOffer string) (string, error) {
	return a.prepareOutboundMedia(call, clientOffer)
}

func (a *Agent) handleOutboundInviteRuntimeResponse(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return a.handleIMS2xxResponse(ctx, call, response)
	}
	return a.handleIMSErrorResponse(ctx, call, response)
}

func (a *Agent) handleOutboundInviteRuntimeError(call *Call, cause error) error {
	if cause == nil {
		cause = errors.New("voice: outbound INVITE failed without an error")
	}
	return a.failOutboundCall(call, cause)
}

func (a *Agent) handleIMS2xxResponse(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) error {
	if call.HasLocalCancelSent() {
		return a.handleLateInvite2xxAfterLocalCancel(ctx, call, response)
	}
	if err := a.completeOutboundInvite(ctx, call, response); err != nil {
		return a.failOutboundCall(call, err)
	}
	if err := a.completeOutboundMedia(call, response); err != nil {
		return a.failEstablishedOutboundCall(ctx, call, err)
	}
	if err := call.TransitionChecked(callstate.StateConnected); err != nil {
		return a.failEstablishedOutboundCall(ctx, call, err)
	}
	if err := call.StartSessionTimer(call.voiceSessionExpires()); err != nil {
		return a.failEstablishedOutboundCall(ctx, call, err)
	}
	a.emitCallAnswered(call)
	return nil
}

func (a *Agent) handleIMSErrorResponse(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) error {
	if response.StatusCode < 300 {
		return a.failOutboundCall(call, fmt.Errorf(
			"voice: unexpected INVITE response: %d %s", response.StatusCode, response.Reason,
		))
	}
	if err := a.sendOutboundInviteErrorACK(call, response); err != nil {
		return a.failOutboundCall(call, err)
	}
	completeErr := a.completeOutboundInvite(ctx, call, response)
	kind, status, reason, cancelReason := classifyOutboundInviteOutcome(
		response.StatusCode, response.Reason, call.OutboundCancelReason(), false,
	)
	cause := errors.New(formatSimulateCallReason(kind, status, reason, cancelReason))
	return a.failOutboundCall(call, errors.Join(cause, completeErr))
}

func (a *Agent) handleIMSResponseEvent(
	ctx context.Context,
	call *Call,
	response *sip.Response,
) error {
	if call == nil || response == nil || !callMatchesID(call, responseCallID(response)) {
		return nil
	}
	cseq := response.CSeq()
	if cseq == nil || responseCSeqNumber(response) == 0 ||
		cseq.MethodName != sip.INVITE || response.StatusCode == 100 {
		return nil
	}
	updateCallDialogFromResponse(call, response)
	if response.StatusCode < 200 {
		handleErr := a.handleIMS1xxResponse(ctx, call, publicVoiceSIPResponse(response))
		return errors.Join(handleErr, a.forwardOrDispatchIMSResponse(call, response))
	}
	return nil
}

func (a *Agent) sendOutboundInviteErrorACK(call *Call, response imscore.SIPResponse) error {
	if call == nil || response.StatusCode < 300 {
		return errors.New("voice: non-2xx INVITE response is required for error ACK")
	}
	// imscore emits the transaction-owned non-2xx ACK before returning the
	// final response to voice; this marker records that completed operation.
	call.MarkErrorACKSent()
	return nil
}

func (a *Agent) forwardOrDispatchIMSResponse(call *Call, response *sip.Response) error {
	if call == nil || response == nil {
		return nil
	}
	call.mu.RLock()
	responseChannel := call.DialogState.IMSResponseCh
	call.mu.RUnlock()
	if responseChannel != nil {
		return enqueueSimulateIMSResponse(call.Ctx, responseChannel, response)
	}
	return a.forwardResponseToClient(call, publicVoiceSIPResponse(response))
}

func enqueueSimulateIMSResponse(
	ctx context.Context,
	destination chan *sip.Response,
	response *sip.Response,
) error {
	if destination == nil || response == nil {
		return errors.New("voice: simulated INVITE response destination is unavailable")
	}
	select {
	case destination <- response.Clone():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("voice: simulated INVITE response queue is full")
	}
}

func responseCallID(response *sip.Response) string {
	if response == nil || response.CallID() == nil {
		return ""
	}
	return strings.TrimSpace(response.CallID().Value())
}

func responseCSeqNumber(response *sip.Response) uint32 {
	if response == nil || response.CSeq() == nil {
		return 0
	}
	return response.CSeq().SeqNo
}

func responseHeaderValues(response *sip.Response, name string) []string {
	if response == nil {
		return nil
	}
	var values []string
	for _, header := range response.GetHeaders(name) {
		if value := strings.TrimSpace(header.Value()); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func updateCallToTagFromResponse(call *Call, response *sip.Response) {
	if call == nil || response == nil || response.To() == nil {
		return
	}
	tag := sipHeaderTag(response.To())
	if tag == "" {
		return
	}
	call.mu.Lock()
	call.DialogState.IMSToTag = tag
	call.mu.Unlock()
}

func updateCallDialogFromResponse(call *Call, response *sip.Response) {
	if call == nil || response == nil {
		return
	}
	updateCallToTagFromResponse(call, response)
	routes := responseHeaderValues(response, "Record-Route")
	if len(routes) > 0 {
		call.SetRouteSet(routes)
	}
	contact := ""
	if response.Contact() != nil {
		contact = strings.TrimSpace(response.Contact().Value())
	}
	call.mu.Lock()
	call.DialogState.IMSContact = contact
	call.mu.Unlock()
	call.learnVoiceDialog(publicVoiceSIPResponse(response))
}

func taggedToHeaderValue(call *Call) string {
	if call == nil {
		return ""
	}
	call.mu.RLock()
	request := call.DialogState.OriginalRequest
	tag := strings.TrimSpace(call.DialogState.ClientToTag)
	call.mu.RUnlock()
	if request == nil || request.To() == nil {
		return ""
	}
	value := strings.TrimSpace(request.To().Value())
	if tag == "" || strings.Contains(strings.ToLower(value), ";tag=") {
		return value
	}
	return value + ";tag=" + tag
}
