package voice

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func (a *Agent) answerStoredServerInvite(call *Call, sdp string) (bool, error) {
	if a == nil || a.dialog == nil || call == nil {
		return false, errors.New("voice: dialog controller is unavailable")
	}
	invite, request := call.serverInviteContext()
	if invite == nil || request == nil {
		return false, nil
	}
	contact := a.dialog.Context().CachedContactHdr
	if contact == nil {
		return true, errors.New("voice: inbound answer Contact is unavailable")
	}
	response := sip.NewResponseFromRequest(request, 200, "OK", []byte(sdp))
	response.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	if expires := call.voiceSessionExpires(); expires > 0 {
		response.AppendHeader(sip.NewHeader("Session-Expires", strconv.FormatInt(int64(expires/time.Second), 10)))
	}
	if responder := call.inboundResponseWriter(); responder != nil && response.To() != nil {
		response.To().Params.Add("tag", responder.LocalTag())
	}
	dialog, err := a.dialog.AnswerServerInvite(
		context.Background(), a.deviceID, invite,
		imsendpoint.ServerInviteAnswerOptions{Response: response, Contact: contact.Clone()},
	)
	if err != nil {
		return true, err
	}
	return true, call.storeDialogHandle(dialog)
}

func (a *Agent) rejectStoredServerInvite(call *Call, statusCode int) (bool, error) {
	if a == nil || a.dialog == nil || call == nil {
		return false, errors.New("voice: dialog controller is unavailable")
	}
	invite, request := call.serverInviteContext()
	if invite == nil || request == nil {
		return false, nil
	}
	reason := strings.TrimSpace(imscore.SIPStatusText(statusCode))
	err := a.dialog.RejectServerInvite(
		context.Background(), a.deviceID, invite,
		imsendpoint.ServerInviteRejectOptions{Code: statusCode, Reason: reason},
	)
	return true, err
}
