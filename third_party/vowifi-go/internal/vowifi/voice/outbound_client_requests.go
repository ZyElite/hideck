package voice

import (
	"context"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// HandleCancel acknowledges a matching local CANCEL and cancels the IMS INVITE.
func (a *Agent) HandleCancel(request *sip.Request, transaction sip.ServerTransaction) {
	call := a.activeCallForRequest(request)
	if call == nil {
		a.respondClientRequestWithFallback(request, transaction, 481, "Call/Transaction Does Not Exist")
		return
	}
	a.respondClientRequestWithFallback(request, transaction, 200, "OK")
	a.runCallTask(call, "client_cancel", func() { a.cancelClientOutboundInvite(call) })
}

func (a *Agent) cancelClientOutboundInvite(call *Call) {
	if call == nil || call.IsTerminalState() || call.CallState() == callstate.StateConnected {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	sent, err := a.sendMarkedOutboundInviteCancel(ctx, call, "client_cancel", "client_cancel")
	if err != nil {
		logging.WarnRate("voice-client-cancel:"+call.CallID(), 10*time.Second,
			"local voice CANCEL failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
		return
	}
	if sent {
		a.scheduleOutboundCancelSettle(call, "client_cancel")
	}
}

func (a *Agent) scheduleOutboundCancelSettle(call *Call, reason string) {
	if call == nil {
		return
	}
	timer := time.NewTimer(a.outboundCancelSettleDuration())
	go func() {
		defer timer.Stop()
		select {
		case <-call.Done:
			return
		case <-timer.C:
			a.settleOutboundCancel(call, reason)
		}
	}()
}

func (a *Agent) outboundCancelSettleDuration() time.Duration {
	if a != nil && a.outboundCancelSettle > 0 {
		return a.outboundCancelSettle
	}
	return outboundCancelSettle
}

func (a *Agent) settleOutboundCancel(call *Call, reason string) {
	if call == nil || callDone(call) || call.HasInviteFinalSeen() {
		return
	}
	if reason == "no_answer" {
		a.respondSyntheticFinalToClient(call, 408, "Request Timeout")
	} else {
		a.respondSyntheticFinalToClient(call, 487, "Request Terminated")
	}
	call.cancelOutboundRuntime()
	if reason == "no_answer" {
		a.finishOutboundNoAnswer(call)
		return
	}
	a.reportCallCleanupError(call, a.finishLocalCancel(call, reason))
}

func (c *Call) cancelOutboundRuntime() {
	if c == nil {
		return
	}
	c.CancelOutboundInviteTimer()
}

// HandleOutboundACK consumes the local ACK after the final client response.
func (a *Agent) HandleOutboundACK(request *sip.Request) {
	call := a.activeCallForRequest(request)
	if call != nil {
		call.MarkACKSent()
	}
}

// HandleClientBye acknowledges a matching local BYE and closes the IMS dialog.
func (a *Agent) HandleClientBye(request *sip.Request, transaction sip.ServerTransaction) {
	call := a.activeCallForRequest(request)
	if call == nil {
		a.respondClientRequestWithFallback(request, transaction, 481, "Call/Transaction Does Not Exist")
		return
	}
	a.respondClientRequestWithFallback(request, transaction, 200, "OK")
	a.runCallTask(call, "client_bye", func() {
		if err := a.hangupCall(context.Background(), call); err != nil {
			logging.WarnRate("voice-client-bye:"+call.CallID(), 10*time.Second,
				"local voice BYE failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
		}
	})
}

// HandlePrack acknowledges a matching local PRACK and forwards its RAck context.
func (a *Agent) HandlePrack(request *sip.Request, transaction sip.ServerTransaction) {
	call := a.activeCallForRequest(request)
	if call == nil {
		respondClientRequest(transaction, request, 481, "Call/Transaction Does Not Exist")
		return
	}
	respondClientRequest(transaction, request, 200, "OK")
	rack := requestHeaderValue(request, "RAck")
	if rack == "" {
		return
	}
	a.runCallTask(call, "client_prack", func() { a.forwardClientPRACK(call, rack) })
}

func (a *Agent) forwardClientPRACK(call *Call, rack string) {
	if call == nil || call.IsTerminalState() || a.dialog == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	err := a.sendReliableProvisionalPRACKWithOptions(ctx, call, forwardedPRACKOptions(call, rack))
	if err != nil {
		logging.WarnRate("voice-client-prack:"+call.CallID(), 10*time.Second,
			"local voice PRACK failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
	}
}

func (a *Agent) activeCallForRequest(request *sip.Request) *Call {
	callID := requestCallID(request)
	if a == nil || callID == "" {
		return nil
	}
	a.mu.RLock()
	call := a.activeCall
	a.mu.RUnlock()
	if call != nil && callMatchesID(call, callID) {
		return call
	}
	return nil
}

func requestCallID(request *sip.Request) string {
	if request == nil || request.CallID() == nil {
		return ""
	}
	return strings.TrimSpace(request.CallID().Value())
}

func callMatchesID(call *Call, callID string) bool {
	if call == nil || callID == "" {
		return false
	}
	call.mu.RLock()
	defer call.mu.RUnlock()
	return callID == call.callID || callID == call.clientCallID ||
		callID == call.DialogState.IMSCallID || callID == call.DialogState.OutboundIMSCallID
}

func (a *Agent) runCallTask(call *Call, name string, task func()) {
	if call == nil || task == nil {
		return
	}
	wrapped := func() {
		select {
		case <-call.Ctx.Done():
			return
		default:
			task()
		}
	}
	if call.actor != nil && call.actor.Enqueue(name, wrapped) {
		return
	}
	if a != nil && a.actor != nil && a.actor.Enqueue(name, wrapped) {
		return
	}
	logging.WarnRate("voice-task-rejected:"+call.CallID()+":"+name, 10*time.Second,
		"voice task queues rejected work", "device", call.DeviceID, "call_id", call.CallID(), "task", name)
	go wrapped()
}
