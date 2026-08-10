package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
)

func classifyOutboundInviteOutcome(
	status int,
	reason string,
	cancelReason string,
	noAnswer bool,
) (string, int, string, string) {
	reason = strings.TrimSpace(reason)
	if noAnswer || (cancelReason == "no_answer" && (status == 0 || status == 487)) {
		return "no_answer", status, reason, cancelReason
	}
	switch status {
	case 408, 480:
		return "temporarily_unavailable", status, reason, cancelReason
	case 486, 600:
		return "busy", status, reason, cancelReason
	case 603:
		return "declined", status, reason, cancelReason
	}
	if status >= 300 && status < 400 {
		return "redirected", status, reason, cancelReason
	}
	return "failed", status, reason, cancelReason
}

func formatSimulateCallReason(
	kind string,
	status int,
	reason string,
	cancelReason string,
) string {
	reason = strings.TrimSpace(reason)
	switch kind {
	case "busy":
		return fmt.Sprintf("对方忙线 (%d %s)", status, reason)
	case "declined":
		return fmt.Sprintf("对方拒接 (%d %s)", status, reason)
	case "no_answer":
		return "无人接听（30s 超时）"
	case "temporarily_unavailable":
		return fmt.Sprintf("暂时无法接通 (%d %s)", status, reason)
	}
	if status == 487 {
		return fmt.Sprintf("请求在接通前被终止 (%d %s)", status, reason)
	}
	if status <= 0 {
		if cancelReason != "" {
			return cancelReason
		}
		if reason != "" {
			return reason
		}
		return "call failed"
	}
	return fmt.Sprintf("呼叫失败 (%d %s)", status, reason)
}

func (a *Agent) sendOutboundInviteCancel(ctx context.Context, call *Call, reason string) error {
	if a == nil || a.dialog == nil || call == nil {
		return errors.New("voice: outbound INVITE cancel context is unavailable")
	}
	handle := call.IMSInviteHandleValue()
	if handle == nil {
		return errors.New("voice: IMS INVITE handle is unavailable")
	}
	return a.dialog.CancelClientInvite(ctx, a.deviceID, handle, imsendpoint.ClientInviteCancelOptions{
		Reason: strings.TrimSpace(reason),
	})
}

func (a *Agent) sendMarkedOutboundInviteCancel(
	ctx context.Context,
	call *Call,
	wireReason string,
	markerReason string,
) error {
	if !call.MarkLocalCancelSent(markerReason) {
		return nil
	}
	_ = call.StopOutboundNoAnswerTimer()
	return a.sendOutboundInviteCancel(ctx, call, wireReason)
}

func (a *Agent) handleOutboundInviteNoAnswerTimeout(call *Call) {
	if call == nil || call.IsTerminalState() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	if err := a.sendMarkedOutboundInviteCancel(ctx, call, "cancel_no_answer", "no_answer"); err != nil {
		_ = a.failOutboundCall(call, errors.Join(errors.New("no_answer"), err))
		return
	}
	a.scheduleOutboundCancelSettle(call)
}

func (a *Agent) handleLateInvite2xxAfterLocalCancel(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) error {
	return a.closeLateAcceptedInvite(ctx, call, response)
}
