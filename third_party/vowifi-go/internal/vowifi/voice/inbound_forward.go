package voice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

type inboundClientTarget struct {
	contactURI  string
	destination string
	username    string
}

func (a *Agent) maybeStartInboundClient(call *Call) {
	if a == nil || call == nil {
		return
	}
	bridge, adapter := a.inboundClientDependencies()
	if bridge == nil || adapter == nil {
		return
	}
	if !call.startInboundClientOnce() {
		return
	}
	call.setInboundClientBridge(bridge)
	go a.deliverInboundCall(call, bridge, adapter)
}

func (a *Agent) inboundClientDependencies() (*client.Bridge, voiceclient.Adapter) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.clientBridge, a.clientAdapter
}

func (a *Agent) deliverInboundCall(call *Call, bridge *client.Bridge, adapter voiceclient.Adapter) {
	online := adapter.SubscribeDeviceOnline(a.deviceID)
	if a.tryForwardInboundCall(call, bridge) {
		return
	}
	if err := bridge.SendPush(a.deviceID, call.ClientCallID(), call.CallerID, call.CalleeID); err != nil {
		logging.WarnRate("voice-inbound-push:"+call.CallID(), voiceActorEventLogInterval,
			"voice inbound push failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
	}
	timer := time.NewTimer(inboundClientWaitTimeout)
	defer timer.Stop()
	for {
		select {
		case <-call.Done:
			return
		case <-timer.C:
			return
		case _, open := <-online:
			if !open {
				online = nil
				continue
			}
			if a.tryForwardInboundCall(call, bridge) {
				return
			}
		}
	}
}

func (a *Agent) tryForwardInboundCall(call *Call, bridge *client.Bridge) bool {
	if call == nil || call.IsTerminalState() {
		return true
	}
	contactURI, destination, username, err := bridge.Contact(nil)
	if err != nil || strings.TrimSpace(contactURI) == "" {
		if err != nil {
			logging.WarnRate("voice-inbound-contact:"+call.CallID(), voiceActorEventLogInterval,
				"voice client contact lookup failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
		}
		return false
	}
	return a.forwardToClient(call, bridge, inboundClientTarget{
		contactURI: contactURI, destination: destination, username: username,
	})
}

// forwardToClient runs the retained local UAC transaction for one IMS call.
func (a *Agent) forwardToClient(
	call *Call,
	bridge *client.Bridge,
	target inboundClientTarget,
) bool {
	ctx, cancel := context.WithTimeout(call.Ctx, inboundClientTxTimeout)
	defer cancel()
	request, err := a.buildClientInviteReq(call, bridge, target)
	if err != nil {
		a.failInboundClientDelivery(call, 500, err)
		return true
	}
	transaction, err := bridge.StartTransaction(ctx, "inbound_client_invite", request)
	if err != nil {
		logging.WarnRate("voice-inbound-client-tx:"+call.CallID(), voiceActorEventLogInterval,
			"voice client INVITE transaction failed", "device", a.deviceID, "call_id", call.CallID(), "err", err)
		return false
	}
	if transaction == nil {
		a.failInboundClientDelivery(call, 500,
			errors.New("voice: client INVITE transaction is unavailable"))
		return true
	}
	call.storeClientInvite(request)
	defer transaction.Terminate()
	response, err := a.waitFinalResponse(ctx, call, transaction)
	if err != nil {
		a.handleInboundClientTransactionError(call, err)
		return true
	}
	return a.handleInboundClientFinal(call, response)
}

func (a *Agent) waitFinalResponse(
	ctx context.Context,
	call *Call,
	transaction sip.ClientTransaction,
) (*sip.Response, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.Done:
			return nil, context.Canceled
		case response, open := <-transaction.Responses():
			if !open {
				return nil, transactionError(transaction)
			}
			if response != nil && response.StatusCode >= 200 {
				return response, nil
			}
		case <-transaction.Done():
			return nil, transactionError(transaction)
		}
	}
}

func transactionError(transaction sip.ClientTransaction) error {
	if transaction != nil && transaction.Err() != nil {
		return transaction.Err()
	}
	return errors.New("voice: client INVITE transaction ended without a final response")
}

func (a *Agent) handleInboundClientFinal(call *Call, response *sip.Response) bool {
	if response == nil {
		a.failInboundClientDelivery(call, 500, errors.New("voice: client returned an empty INVITE response"))
		return true
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return a.handleClientAnswer(call, response)
	}
	if response.StatusCode == 408 || response.StatusCode == 480 || response.StatusCode == 503 {
		return false
	}
	a.failInboundClientDelivery(call, response.StatusCode, errors.New("voice: client rejected inbound call"))
	return true
}

func (a *Agent) handleClientAnswer(call *Call, response *sip.Response) bool {
	call.storeClientInviteResponse(response)
	if err := a.sendClientACK(call); err != nil {
		a.failInboundClientDelivery(call, 500, err)
		return true
	}
	if _, err := a.AnswerWithSDP(call.CallID(), string(response.Body())); err != nil {
		_ = a.sendClientBye(call)
		a.failInboundClientDelivery(call, 500, err)
	}
	return true
}

func (a *Agent) handleInboundClientTransactionError(call *Call, cause error) {
	if call == nil || call.IsTerminalState() {
		return
	}
	_ = a.sendClientCancel(call)
	status := 408
	if errors.Is(cause, context.Canceled) && call.CallState() != callstate.StateRinging {
		return
	}
	a.failInboundClientDelivery(call, status, cause)
}

func (a *Agent) failInboundClientDelivery(call *Call, status int, cause error) {
	if call == nil || call.IsTerminalState() {
		return
	}
	if status < 300 || status > 699 {
		status = 500
	}
	if err := a.Reject(call.CallID(), status); err != nil {
		logging.WarnRate("voice-inbound-client-failure:"+call.CallID(), voiceActorEventLogInterval,
			"voice inbound client failure response failed", "device", a.deviceID,
			"call_id", call.CallID(), "status", status, "cause", cause, "err", err)
	}
}
