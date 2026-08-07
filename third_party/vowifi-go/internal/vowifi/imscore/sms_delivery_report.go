package imscore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/warthog618/sms/encoding/tpdu"
)

const smsDeliveryStateAcked = "acked"

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
	report := smsDeliveryReport{
		reference: int(info.MR), state: state, sipCode: 200,
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
			s.sendInboundSMSControl(raw, smscodec.BuildRPAck(rpMR))
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
		reference: int(report.MR), state: state, sipCode: 200,
		rpCause: int(report.ST), errorText: errorText, reportedAt: report.DT.Time,
	}, nil
}

func (s *Service) recordSMSDeliveryReport(raw string, report smsDeliveryReport) error {
	if s.delivery == nil {
		return errors.New("imscore: SMS delivery report store is unavailable")
	}
	reportedAt := report.reportedAt
	if reportedAt.IsZero() {
		reportedAt = time.Now()
	}
	match, err := s.delivery.MarkSMSDeliveryPartReport(
		rawSIPHeaderValue(raw, "In-Reply-To"), rawSIPHeaderValue(raw, "Call-ID"),
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
	return s.publishSMSDeliveryStatus(match.MessageID)
}

func (s *Service) publishSMSDeliveryStatus(messageID string) error {
	status, err := s.delivery.GetSMSDeliveryStatus(messageID)
	if err != nil {
		return fmt.Errorf("imscore: read SMS delivery status: %w", err)
	}
	s.bus.Publish(&events.EventSMSDeliveryUpdated{
		DevID: s.cfg.DeviceID, MessageID: messageID, State: status.State, Time: time.Now(),
	})
	switch status.State {
	case smsDeliveryStateAcked:
		s.bus.Publish(&events.EventSMSDeliveryCompleted{DevID: s.cfg.DeviceID, MessageID: messageID, Time: time.Now()})
	case smsDeliveryStateFailed:
		s.bus.Publish(&events.EventSMSDeliveryFailed{
			DevID: s.cfg.DeviceID, MessageID: messageID, Error: status.LastError, Time: time.Now(),
		})
	}
	return nil
}

func (s *Service) scheduleSMSDeliveryTimeout(messageID string, parts []outboundSMSPart) {
	if s.delivery == nil || s.smsReportTimeout <= 0 {
		return
	}
	deadline := s.smsReportTimeout
	s.networkDone.Add(1)
	go func() {
		defer s.networkDone.Done()
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.expireSMSDelivery(messageID, parts)
		case <-s.stop:
		}
	}()
}

func (s *Service) expireSMSDelivery(messageID string, parts []outboundSMSPart) {
	status, err := s.delivery.GetSMSDeliveryStatus(messageID)
	if err != nil {
		logging.WarnRate("ims-sms-delivery-timeout-read", "IMS SMS delivery timeout state read failed", "err", err)
		return
	}
	pending := pendingDeliveryParts(status)
	matched := false
	for _, part := range parts {
		if !pending[part.number] {
			continue
		}
		_, markErr := s.delivery.MarkSMSDeliveryPartReport(
			part.callID, part.callID, s.cfg.DeviceID, int(part.rpMR),
			smsDeliveryPartStateTimeout, 0, 0, "delivery report timeout", time.Now(),
		)
		if markErr != nil {
			logging.WarnRate("ims-sms-delivery-timeout-write", "IMS SMS delivery timeout persist failed", "err", markErr)
			continue
		}
		matched = true
	}
	if !matched {
		return
	}
	if err := s.delivery.RecomputeSMSDelivery(messageID, time.Now()); err != nil {
		logging.WarnRate("ims-sms-delivery-timeout-recompute", "IMS SMS delivery timeout recompute failed", "err", err)
		return
	}
	if err := s.publishSMSDeliveryStatus(messageID); err != nil {
		logging.WarnRate("ims-sms-delivery-timeout-publish", "IMS SMS delivery timeout publish failed", "err", err)
	}
}

func pendingDeliveryParts(status *DeliveryStatus) map[int]bool {
	pending := make(map[int]bool)
	if status == nil {
		return pending
	}
	for _, part := range status.Parts {
		if part.State == smsDeliveryStatePending {
			pending[part.PartNo] = true
		}
	}
	return pending
}
