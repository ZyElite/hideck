package imscore

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type lateMOSIPFinalContext struct {
	messageID string
	part      outboundSMSPart
	pending   *smsPendingInfo
}

func (s *Service) moSubmitTransactionCallbacks(
	messageID string,
	part outboundSMSPart,
	pending *smsPendingInfo,
) sipTransactionCallbacks {
	lateContext := lateMOSIPFinalContext{messageID: messageID, part: part, pending: pending}
	return sipTransactionCallbacks{
		onLateFinal: func(response *sipResponse) error {
			return s.handleLateMOSIPFinal(lateContext, response)
		},
		retainFinalAfterContext: func(cause error) bool {
			return errors.Is(cause, errMOSMSFinalProbeTimeout)
		},
		lateFinalRetention: s.smsReportTimeout,
	}
}

func (s *Service) handleLateMOSIPFinal(
	lateContext lateMOSIPFinalContext,
	response *sipResponse,
) error {
	if response == nil {
		return errors.New("imscore: nil late MESSAGE final response")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return s.handleLateMOAccepted(lateContext, response.StatusCode)
	}
	reason := fmt.Sprintf(
		"MESSAGE rejected with status %d (%s)", response.StatusCode, strings.TrimSpace(response.Reason),
	)
	persistErr := s.persistOutboundSIPResult(
		lateContext.messageID, lateContext.part.number,
		response.StatusCode, smsDeliveryStateFailed, reason,
	)
	s.takePendingSMSByCallID(lateContext.part.callID)
	notifySMSPending(lateContext.pending, smsSendResult{
		Code: response.StatusCode, Status: smsDeliveryStateFailed,
		Reason: errors.Join(errors.New(reason), persistErr).Error(), At: time.Now(),
	})
	return persistErr
}

func (s *Service) handleLateMOAccepted(
	lateContext lateMOSIPFinalContext,
	sipCode int,
) error {
	state, err := s.persistAcceptedSIPPart(lateContext.messageID, lateContext.part, sipCode)
	if err != nil {
		s.takePendingSMSByCallID(lateContext.part.callID)
		notifySMSPending(lateContext.pending, smsSendResult{
			Code: sipCode, Status: smsDeliveryStateFailed, Reason: err.Error(), At: time.Now(),
		})
		return err
	}
	notifySMSPending(lateContext.pending, smsSendResult{Code: sipCode, Status: state, At: time.Now()})
	s.expirePendingSMSAfter(lateContext.part.callID, pendingSMSReportRetention)
	return nil
}

func notifySMSPending(pending *smsPendingInfo, result smsSendResult) {
	if pending == nil || pending.RespCh == nil {
		return
	}
	select {
	case pending.RespCh <- result:
	default:
	}
}
