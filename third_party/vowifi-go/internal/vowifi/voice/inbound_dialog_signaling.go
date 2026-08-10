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

type serverInviteAnswer struct {
	status int
	reason string
	sdp    string
}

func (a *Agent) answerStoredServerInvite(call *Call, sdp string) (bool, error) {
	return a.answerStoredServerInviteResult(call, serverInviteAnswer{
		status: 200, reason: "OK", sdp: sdp,
	})
}

func (a *Agent) answerStoredServerInviteResult(
	call *Call,
	answer serverInviteAnswer,
) (bool, error) {
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
	response := call.BuildResponseWithSDP(answer.status, answer.reason, []byte(answer.sdp))
	if expires := call.voiceSessionExpires(); expires > 0 {
		response.AppendHeader(sip.NewHeader("Session-Expires", strconv.FormatInt(int64(expires/time.Second), 10)))
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
	return a.rejectStoredServerInviteWithReason(call, statusCode, imscore.SIPStatusText(statusCode))
}

func (a *Agent) rejectStoredServerInviteWithReason(
	call *Call,
	statusCode int,
	reason string,
) (bool, error) {
	if a == nil || a.dialog == nil || call == nil {
		return false, errors.New("voice: dialog controller is unavailable")
	}
	invite, request := call.serverInviteContext()
	if invite == nil || request == nil {
		return false, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = strings.TrimSpace(imscore.SIPStatusText(statusCode))
	}
	response := call.BuildResponse(statusCode, reason)
	err := a.dialog.RejectServerInvite(
		context.Background(), a.deviceID, invite,
		imsendpoint.ServerInviteRejectOptions{Response: response, Code: statusCode, Reason: reason},
	)
	return true, err
}
