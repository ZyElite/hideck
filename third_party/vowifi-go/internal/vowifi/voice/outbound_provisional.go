package voice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func (a *Agent) handleOutboundProvisional(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) (resultErr error) {
	return a.handleIMS1xxResponse(ctx, call, response)
}

func (a *Agent) handleIMS1xxResponse(
	ctx context.Context,
	call *Call,
	response imscore.SIPResponse,
) (resultErr error) {
	if call == nil || response.StatusCode <= 100 || response.StatusCode >= 200 {
		return nil
	}
	logOutboundInviteResponse("IMS INVITE 临时响应", response)
	call.MarkInviteProvisional(response.StatusCode)
	if response.StatusCode == 180 {
		a.emitCallRinging(call)
	}
	call.learnVoiceDialog(response)
	call.applyVoiceSessionExpires(voiceResponseHeader(response.Headers, "Session-Expires"))
	if isVoiceSDPContentType(voiceResponseHeader(response.Headers, "Content-Type")) && len(response.Body) > 0 {
		if err := a.updateRemoteMedia(call, response); err != nil {
			logging.WarnRate("ims-invite-provisional-sdp", "IMS INVITE 临时响应 SDP 处理失败",
				"status", response.StatusCode, "err", err)
		}
	}
	if !sipHeaderHasToken(voiceResponseHeader(response.Headers, "Require"), "100rel") {
		return nil
	}
	rseq, err := reliableProvisionalRSeq(response)
	if err != nil {
		return err
	}
	if !call.markReliableProvisionalRSeq(rseq) {
		return nil
	}
	return a.sendReliableProvisionalPRACK(ctx, call, rseq)
}

func logOutboundInviteResponse(message string, response imscore.SIPResponse) {
	logging.Info(message,
		"status", response.StatusCode,
		"reason", response.Reason,
		"require", voiceResponseHeader(response.Headers, "Require"),
		"rseq", voiceResponseHeader(response.Headers, "RSeq"),
		"warning", logging.RedactSIPRaw(voiceResponseHeader(response.Headers, "Warning")),
		"network_reason", logging.RedactSIPRaw(voiceResponseHeader(response.Headers, "Reason")),
	)
}

func (a *Agent) sendReliableProvisionalPRACK(ctx context.Context, call *Call, rseq uint32) error {
	if a == nil || a.dialog == nil || call == nil {
		return errors.New("voice: dialog controller is unavailable")
	}
	invite := call.IMSInviteHandle()
	if invite == nil {
		return errors.New("voice: IMS INVITE handle is unavailable")
	}
	err := a.dialog.SendReliableProvisionalPRACK(ctx, a.deviceID, imsendpoint.ReliableProvisionalOptions{
		Invite: invite,
		RSeq:   strconv.FormatUint(uint64(rseq), 10),
	})
	if err != nil {
		return fmt.Errorf("voice: PRACK transaction failed: %w", err)
	}
	logging.Info("IMS PRACK 成功", "rseq", rseq, "status", 200)
	return nil
}

func reliableProvisionalRSeq(response imscore.SIPResponse) (uint32, error) {
	value := strings.TrimSpace(voiceResponseHeader(response.Headers, "RSeq"))
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("voice: reliable provisional response has invalid RSeq %q", value)
	}
	return uint32(parsed), nil
}

func sipHeaderHasToken(value, token string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), token) {
			return true
		}
	}
	return false
}
