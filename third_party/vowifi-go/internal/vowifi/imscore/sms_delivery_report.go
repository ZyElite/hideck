package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/warthog618/sms/encoding/tpdu"
)

const (
	smsDeliveryStateAcked = "acked"
	smsSubmitReportAck    = "submit report ack"
)

type smsDeliveryReport struct {
	reference  int
	state      string
	sipCode    int
	rpCause    int
	errorText  string
	reportedAt time.Time
}

func (s *Service) handleInboundRPReport(raw string, info smscodec.RPDUInfo, state, errorText string) (inboundSIPResult, error) {
	response, err := buildSIPRequestResponse(raw, 200)
	if err != nil {
		return inboundSIPResult{}, err
	}
	if state == smsDeliveryStateAcked && strings.TrimSpace(errorText) == "" {
		errorText = smsSubmitReportAck
	}
	report := smsDeliveryReport{
		reference: int(info.MR), state: state, sipCode: 0,
		rpCause: info.Cause, errorText: errorText,
	}
	return inboundSIPResult{response: response}, s.recordSMSDeliveryReport(raw, report)
}

func (s *Service) handleInboundTPStatusReport(raw string, rpMR byte, payload []byte) (inboundSIPResult, error) {
	report, err := parseTPStatusReport(payload)
	if err != nil {
		return s.inboundSMSProtocolError(raw, 400, rpMR, true, err)
	}
	response, err := buildSIPRequestResponse(raw, 200)
	if err != nil {
		return inboundSIPResult{}, err
	}
	recordErr := s.recordSMSDeliveryReport(raw, report)
	return inboundSIPResult{
		response: response,
		afterReply: func() {
			s.sendRpAckWithRetry(raw, smscodec.BuildRPAck(rpMR), rpMR, "")
		},
	}, recordErr
}

func parseTPStatusReport(payload []byte) (smsDeliveryReport, error) {
	report := &tpdu.TPDU{Direction: tpdu.MT}
	if err := report.UnmarshalBinary(payload); err != nil {
		return smsDeliveryReport{}, fmt.Errorf("decode SMS-STATUS-REPORT: %w", err)
	}
	if report.SmsType() != tpdu.SmsStatusReport {
		return smsDeliveryReport{}, errors.New("RP-DATA does not contain SMS-STATUS-REPORT")
	}
	state := smsDeliveryStateFailed
	if report.ST <= 0x1f {
		state = smsDeliveryStateAcked
	} else if report.ST <= 0x3f {
		state = smsDeliveryStatePending
	}
	errorText := ""
	if state == smsDeliveryStateFailed {
		errorText = fmt.Sprintf("SMS-STATUS-REPORT status 0x%02x", report.ST)
	}
	return smsDeliveryReport{
		reference: int(report.MR), state: state, sipCode: 0,
		rpCause: int(report.ST), errorText: errorText, reportedAt: report.DT.Time,
	}, nil
}

func (s *Service) recordSMSDeliveryReport(raw string, report smsDeliveryReport) error {
	s.recordMORPErrorCause(report.rpCause)
	inReplyTo := rawSIPHeaderValue(raw, "In-Reply-To")
	callID := rawSIPHeaderValue(raw, "Call-ID")
	if s.delivery == nil {
		return errors.New("imscore: SMS delivery report store is unavailable")
	}
	reportedAt := report.reportedAt
	if reportedAt.IsZero() {
		reportedAt = time.Now()
	}
	match, err := s.delivery.MarkSMSDeliveryPartReport(
		inReplyTo, callID,
		s.cfg.DeviceID, report.reference, report.state, report.sipCode,
		report.rpCause, report.errorText, reportedAt,
	)
	if err != nil {
		return fmt.Errorf("imscore: persist SMS delivery report: %w", err)
	}
	if !match.Matched || strings.TrimSpace(match.MessageID) == "" {
		return errors.New("imscore: SMS delivery report did not match a pending part")
	}
	if err := s.delivery.RecomputeSMSDelivery(match.MessageID, time.Now()); err != nil {
		return fmt.Errorf("imscore: recompute SMS delivery: %w", err)
	}
	if _, err = s.publishSMSDeliveryStatus(match.MessageID); err != nil {
		return err
	}
	s.completePendingSMSByReport(inReplyTo, callID, report.reference, smsSendResult{
		Code: report.sipCode, Status: report.state, Reason: report.errorText,
		Body: append([]byte(nil), []byte(raw)...), At: time.Now(),
	})
	return nil
}

func (s *Service) recordMORPErrorCause(cause int) {
	switch cause {
	case 28:
		s.moRPErrorCause28.Add(1)
	case 30:
		s.moRPErrorCause30.Add(1)
	case 38:
		s.moRPErrorCause38.Add(1)
	}
}

func (s *Service) publishSMSDeliveryStatus(messageID string) (*DeliveryStatus, error) {
	status, err := s.delivery.GetSMSDeliveryStatus(messageID)
	if err != nil {
		return nil, fmt.Errorf("imscore: read SMS delivery status: %w", err)
	}
	now := time.Now()
	partNo, sipCode, rpCause := latestDeliveryPart(status)
	completed := status.State == smsDeliveryStateAcked || status.State == smsDeliveryStateFailed
	s.dispatchSMSDeliveryUpdated(status, partNo, sipCode, rpCause, completed, now)
	switch status.State {
	case smsDeliveryStateAcked:
		s.dispatchSMSDeliveryCompleted(status, now)
	case smsDeliveryStateFailed:
		s.handleReportFailure(status, sipCode, now)
	}
	return status, nil
}

func (s *Service) dispatchSMSDeliveryUpdated(
	status *DeliveryStatus,
	partNo, sipCode, rpCause int,
	completed bool,
	at time.Time,
) {
	if status == nil {
		return
	}
	s.bus.Publish(events.EventSMSDeliveryUpdated{
		DevID: s.cfg.DeviceID, MessageID: status.MessageID, PartNo: partNo,
		PartsTotal: status.PartsTotal, State: status.State, SIPCode: sipCode,
		RPCause: rpCause, UpdatedAt: at, Completed: completed,
		FailureText: status.LastError, Time: at,
	})
}

func (s *Service) dispatchSMSDeliveryCompleted(status *DeliveryStatus, at time.Time) {
	if status == nil {
		return
	}
	s.bus.Publish(events.EventSMSDeliveryCompleted{
		DevID: s.cfg.DeviceID, MessageID: status.MessageID, PartsTotal: status.PartsTotal,
		CompletedAt: at, Time: at,
	})
}

func (s *Service) handleReportFailure(status *DeliveryStatus, sipCode int, at time.Time) {
	if status == nil {
		return
	}
	s.bus.Publish(events.EventSMSDeliveryFailed{
		DevID: s.cfg.DeviceID, TargetURI: status.Peer, Reason: status.LastError,
		SIPCode: sipCode, RecommendCSFallback: recommendCSFallback(sipCode),
		MessageID: status.MessageID, Error: status.LastError, Time: at,
	})
}

func latestDeliveryPart(status *DeliveryStatus) (partNo, sipCode, rpCause int) {
	if status == nil || len(status.Parts) == 0 {
		return 0, 0, 0
	}
	part := status.Parts[len(status.Parts)-1]
	return part.PartNo, part.SIPCode, part.RPCause
}

func recommendCSFallback(sipCode int) bool {
	return sipCode == 408 || sipCode == 480 || sipCode == 503
}

func (s *Service) waitDeliveryReport(
	ctx context.Context,
	pending *smsPendingInfo,
	timeout time.Duration,
) (smsSendResult, error) {
	if pending == nil || pending.RespCh == nil {
		return smsSendResult{}, errors.New("imscore: missing pending SMS report channel")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-pending.RespCh:
		return result, nil
	case <-timer.C:
		s.takePendingSMSByCallID(pending.CallID)
		return smsSendResult{}, fmt.Errorf("SMS delivery report timeout after %s", timeout)
	case <-ctx.Done():
		s.takePendingSMSByCallID(pending.CallID)
		return smsSendResult{}, ctx.Err()
	case <-s.stop:
		return smsSendResult{}, context.Canceled
	}
}
