package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

var (
	errInboundCallUnavailable     = errors.New("voice: inbound call is unavailable")
	errInboundResponseUnavailable = errors.New("voice: inbound INVITE response context is unavailable")
)

func (a *Agent) rejectBusy(
	request *sip.Request,
	session *imsendpoint.Session,
	handle imsendpoint.ServerInviteHandle,
) {
	if a == nil || request == nil || handle == nil {
		return
	}
	call := NewCallFromRequest(a.deviceID, request, session)
	call.agent = a
	call.setServerInvite(handle, request)
	call.DialogState.ToTag = voiceTag()
	a.sendStatusResponse(486, "Busy Here", call)
}

func (a *Agent) sendStatusResponse(status int, reason string, call *Call) {
	if err := a.sendStatusResponseResult(status, reason, call); err != nil {
		logging.WarnRate("voice-status-response:"+callIDValue(call), voiceActorEventLogInterval,
			"voice status response failed", "device", a.DeviceID(), "call_id", callIDValue(call),
			"status", status, "err", err)
	}
}

func (a *Agent) sendStatusResponseResult(status int, reason string, call *Call) error {
	if call == nil {
		return errInboundCallUnavailable
	}
	structured, err := a.rejectStoredServerInviteWithReason(call, status, reason)
	if structured || err != nil {
		return err
	}
	responder := call.inboundResponseWriter()
	if responder == nil {
		return errInboundResponseUnavailable
	}
	return responder.Respond(imscore.InboundVoiceResponse{
		StatusCode: status, ToTag: call.inboundLocalTagValue(),
	})
}

func (a *Agent) sendStatusResponseWithSDP(
	status int,
	reason string,
	call *Call,
	sdp string,
) {
	if status < 200 || status >= 300 {
		a.sendStatusResponse(status, reason, call)
		return
	}
	if _, err := a.answerStoredServerInviteResult(call, serverInviteAnswer{
		status: status, reason: reason, sdp: sdp,
	}); err != nil {
		logging.WarnRate("voice-sdp-response:"+callIDValue(call), voiceActorEventLogInterval,
			"voice SDP response failed", "device", a.DeviceID(), "call_id", callIDValue(call),
			"status", status, "err", err)
	}
}

func (a *Agent) sendIMSBye(call *Call) {
	if a == nil || call == nil || call.IMSDialogValue() == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
		defer cancel()
		response, err := a.sendCallDialogRequest(ctx, call, BuildIMSBye(a, call))
		if err == nil && (response.StatusCode < 200 || response.StatusCode >= 300) {
			err = fmt.Errorf("voice: IMS BYE rejected: %d %s", response.StatusCode, response.Reason)
		}
		if err != nil {
			logging.WarnRate("voice-ims-bye:"+call.CallID(), voiceActorEventLogInterval,
				"voice IMS BYE failed", "device", a.DeviceID(), "call_id", call.CallID(), "err", err)
		}
	}()
}

func callIDValue(call *Call) string {
	if call == nil {
		return ""
	}
	return strings.TrimSpace(call.CallID())
}
