package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

const (
	legacyCallOccupancyWindow = 2 * time.Hour
	outboundCancelSettle      = 5 * time.Second
)

// HandleOutboundInvite starts a real IMS INVITE for a local SIP transaction.
func (a *Agent) HandleOutboundInvite(request *sip.Request, transaction sip.ServerTransaction) {
	if a == nil {
		respondClientRequest(transaction, request, 500, "Server Internal Error")
		return
	}
	if !a.validateAndCleanExistingCall(request, transaction) {
		return
	}
	if err := validateClientInviteRequest(request); err != nil {
		a.respondClientRequestWithFallback(request, transaction, 400, "Bad Request")
		return
	}
	if err := validateClientInviteOffer(request); err != nil {
		a.respondClientRequestWithFallback(request, transaction, 488, "Not Acceptable Here")
		return
	}
	if err := a.validateOutboundRuntime(); err != nil {
		a.respondClientRequestWithFallback(request, transaction, 503, "Service Unavailable")
		return
	}
	call, err := a.newClientOutboundCall(request, transaction)
	if err != nil {
		a.respondClientRequestWithFallback(request, transaction, 500, "Server Internal Error")
		return
	}
	a.startIMSOutboundDialog(call)
}

func (a *Agent) validateAndCleanExistingCall(
	request *sip.Request,
	transaction sip.ServerTransaction,
) bool {
	a.mu.RLock()
	existing := a.activeCall
	a.mu.RUnlock()
	if existing == nil {
		return true
	}
	age := time.Since(existing.StartTime())
	if !existing.IsTerminalState() && age < legacyCallOccupancyWindow {
		a.respondClientRequestWithFallback(request, transaction, 486, "Busy Here")
		return false
	}
	a.releaseStaleOutboundCall(existing)
	return true
}

func (a *Agent) releaseStaleOutboundCall(call *Call) {
	_ = call.StopMedia()
	_ = call.EnsureTimerStopped()
	call.CloseDone()
	a.finalizeActiveCall(call)
}

func (a *Agent) newClientOutboundCall(
	request *sip.Request,
	transaction sip.ServerTransaction,
) (*Call, error) {
	call := NewCallFromClientInvite(a.deviceID, request)
	call.agent = a
	call.SetStartTime(time.Now())
	call.DialogState.ClientTx = transaction
	if err := a.prepareVoiceDialog(call, call.Peer()); err != nil {
		call.Cancel()
		return nil, err
	}
	if err := call.TransitionChecked(callstate.StateDialing); err != nil {
		call.Cancel()
		return nil, err
	}
	a.mu.Lock()
	a.calls[call.CallID()] = call
	a.activeCall = call
	a.mu.Unlock()
	return call, nil
}

func validateClientInviteOffer(request *sip.Request) error {
	if request == nil || len(request.Body()) == 0 {
		return errors.New("voice: local INVITE lacks an SDP offer")
	}
	contentType := requestHeaderValue(request, "Content-Type")
	if !isVoiceSDPContentType(contentType) {
		return fmt.Errorf("voice: unsupported local INVITE content type %q", contentType)
	}
	_, err := ProcessOutgoingClientSDP(string(request.Body()))
	return err
}

func validateClientInviteRequest(request *sip.Request) error {
	if request == nil || request.Method != sip.INVITE {
		return errors.New("voice: local request is not INVITE")
	}
	if request.CallID() == nil || request.From() == nil || request.To() == nil || request.CSeq() == nil {
		return errors.New("voice: local INVITE lacks dialog headers")
	}
	if strings.TrimSpace(sipAddressUser(request.To())) == "" {
		return errors.New("voice: local INVITE lacks a callee")
	}
	return nil
}

func (a *Agent) validateOutboundRuntime() error {
	endpoint := a.imsEndpoint()
	if endpoint == nil {
		return errors.New("voice: no IMS service")
	}
	a.mu.RLock()
	started := a.started
	a.mu.RUnlock()
	if !started {
		return errors.New("voice: agent not started")
	}
	if !endpoint.IsRegistered() {
		return errors.New("voice: IMS not registered")
	}
	return nil
}

func (a *Agent) startIMSOutboundDialog(call *Call) {
	ctx, cancel := context.WithTimeout(
		a.agentContext(), voiceInviteTimeout+outboundCancelSettle,
	)
	call.SetOutboundRuntimeCancel(cancel)
	go func() {
		defer cancel()
		request, transaction, offer := call.clientInviteRuntime()
		response, err := a.executeOutboundCall(ctx, call, offer)
		if call.HasLocalCancelSent() {
			a.respondOutboundCancellationFinal(call, response)
			if !callDone(call) {
				a.finishLocalCancel(call, call.OutboundCancelReason())
			}
		} else if response.StatusCode >= 200 {
			err = errors.Join(err, a.forwardResponseToClient(call, response))
		} else if err != nil {
			status := 500
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				status = 408
			}
			a.respondClientRequestWithFallback(
				request, transaction,
				status, imscore.SIPStatusText(status),
			)
		}
		if err != nil {
			logging.WarnRate("voice-client-invite:"+call.CallID(), 10*time.Second,
				"local voice INVITE failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
		}
	}()
}

func (c *Call) clientInviteRuntime() (*sip.Request, sip.ServerTransaction, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DialogState.OriginalRequest, c.DialogState.ClientTx, string(c.MediaState.ClientSDP)
}

func (a *Agent) respondSyntheticFinalToClient(call *Call, status int, reason string) {
	if call == nil {
		return
	}
	request, transaction, _ := call.clientInviteRuntime()
	if request == nil || !call.markClientFinalSent() {
		return
	}
	response := buildClientResponseFromRequest(request, status, reason, nil)
	if value := taggedToHeaderValue(call); value != "" {
		response.RemoveHeader("To")
		response.AppendHeader(sip.NewHeader("To", value))
	}
	if err := a.respondClientWithFallback(transaction, response); err != nil {
		logging.WarnRate("voice-client-synthetic:"+call.CallID(), 10*time.Second,
			"local synthetic INVITE response failed", "device", a.deviceID, "status", status, "err", err)
	}
}

func (a *Agent) respondOutboundCancellationFinal(call *Call, response imscore.SIPResponse) {
	if call == nil {
		return
	}
	if call.OutboundCancelReason() == "no_answer" {
		a.respondSyntheticFinalToClient(call, 408, imscore.SIPStatusText(408))
		return
	}
	if response.StatusCode >= 300 {
		if err := a.forwardResponseToClient(call, response); err != nil {
			logging.WarnRate("voice-client-cancel-final:"+call.CallID(), 10*time.Second,
				"local canceled INVITE final response failed", "device", a.deviceID,
				"call_id", call.CallID(), "status", response.StatusCode, "err", err)
		}
		return
	}
	a.respondSyntheticFinalToClient(call, 487, imscore.SIPStatusText(487))
}

func callDone(call *Call) bool {
	if call == nil {
		return true
	}
	select {
	case <-call.Done:
		return true
	default:
		return false
	}
}

func (a *Agent) agentContext() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func requestHeaderValue(request *sip.Request, name string) string {
	if request == nil {
		return ""
	}
	header := request.GetHeader(name)
	if header == nil {
		return ""
	}
	return strings.TrimSpace(header.Value())
}
