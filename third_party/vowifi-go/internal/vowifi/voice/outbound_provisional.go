package voice

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

func (a *Agent) handleOutboundProvisional(ctx context.Context, call *Call, response imscore.SIPResponse) error {
	if call == nil || response.StatusCode <= 100 || response.StatusCode >= 200 {
		return nil
	}
	logOutboundInviteResponse("IMS INVITE 临时响应", response)
	call.MarkInviteProvisional()
	call.learnVoiceDialog(response)
	call.applyVoiceSessionExpires(voiceResponseHeader(response.Headers, "Session-Expires"))
	a.applyProvisionalState(call, response.StatusCode)
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

func (a *Agent) applyProvisionalState(call *Call, status int) {
	state := call.GetState()
	if status == 180 && state == callstate.StateDialing {
		if call.Transition(callstate.StateAlerting) == nil {
			a.emitCallRinging(call)
		}
		return
	}
	if status == 183 && (state == callstate.StateDialing || state == callstate.StateAlerting) {
		_ = call.Transition(callstate.StateConnecting)
	}
}

func (a *Agent) sendReliableProvisionalPRACK(ctx context.Context, call *Call, rseq uint32) error {
	request := buildIMSPrack(a, call, rseq)
	if err := call.configurePrackRetransmission(func() error {
		return a.ims.SendRawSIP(request)
	}); err != nil {
		return err
	}
	if err := call.StartPrackRuntimeRetransmission(); err != nil {
		return err
	}
	defer call.StopPrackTimer()
	response, err := a.ims.RoundTripSIP(ctx, request)
	if err != nil {
		return fmt.Errorf("voice: PRACK transaction failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("voice: PRACK rejected: %d %s", response.StatusCode, response.Reason)
	}
	logging.Info("IMS PRACK 成功", "rseq", rseq, "status", response.StatusCode)
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
