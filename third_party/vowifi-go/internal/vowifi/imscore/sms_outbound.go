package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

const (
	outboundSMSTransactionTimeout   = 30 * time.Second
	defaultSMSDeliveryReportTimeout = 120 * time.Second
	smsDeliveryStatePending         = "pending"
	smsDeliveryStateFailed          = "failed"
	smsDeliveryPartStateTimeout     = "timeout"
)

type outboundSMSPart struct {
	number  int
	rpMR    byte
	callID  string
	request string
}

func (s *Service) sendOutboundSMS(ctx context.Context, to, text string, opts SMSSendOptions) (*SMSSendOutcome, error) {
	if s == nil || s.cfg == nil {
		return nil, errors.New("imscore: service not configured")
	}
	readiness := s.SMSReadiness()
	if !readiness.Ready {
		return nil, fmt.Errorf("imscore: SMS not ready: %s", readiness.Reason)
	}
	recipient, err := normalizeSMSRecipient(to)
	if err != nil {
		return nil, err
	}
	parts, err := s.buildOutboundSMSParts(recipient, text, opts)
	if err != nil {
		return nil, err
	}
	messageID := newMessageID()
	if err := s.createOutboundDelivery(messageID, recipient, text, len(parts)); err != nil {
		return nil, err
	}
	outcome := &SMSSendOutcome{
		Ref: messageID, MessageID: messageID,
		PartsTotal: len(parts), State: smsDeliveryStatePending,
	}
	for _, part := range parts {
		if err := s.sendOutboundSMSPart(ctx, messageID, part); err != nil {
			outcome.Err = err
			outcome.State = smsDeliveryStateFailed
			return outcome, err
		}
	}
	s.publishOutboundSMS(recipient, text, len(parts))
	s.scheduleSMSDeliveryTimeout(messageID, parts)
	return outcome, nil
}

func (s *Service) buildOutboundSMSParts(recipient, text string, opts SMSSendOptions) ([]outboundSMSPart, error) {
	tpdus, err := smscodec.BuildSubmitTPDUsWithOptions(recipient, text, smscodec.SubmitOptions{
		Encoding: opts.Encoding, ConcatReference: int(s.allocateSMSConcatReference()),
	})
	if err != nil {
		return nil, fmt.Errorf("imscore: encode SMS-SUBMIT: %w", err)
	}
	remoteURI := fmt.Sprintf("sip:%s@%s;user=phone", recipient, strings.TrimSpace(s.cfg.Domain))
	parts := make([]outboundSMSPart, 0, len(tpdus))
	for index := range tpdus {
		rpMR := s.allocateSMSRPMR()
		tpdus[index].MR = rpMR
		tpduBytes, err := tpdus[index].MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("imscore: encode SMS-SUBMIT part %d: %w", index+1, err)
		}
		request, err := s.buildSMSMESSAGE(remoteURI, smscodec.BuildRPData(rpMR, "", s.cfg.SMSC, tpduBytes))
		if err != nil {
			return nil, fmt.Errorf("imscore: build SMS MESSAGE part %d: %w", index+1, err)
		}
		parts = append(parts, outboundSMSPart{
			number: index + 1, rpMR: rpMR,
			callID: rawSIPHeaderValue(request, "Call-ID"), request: request,
		})
	}
	return parts, nil
}

func (s *Service) sendOutboundSMSPart(ctx context.Context, messageID string, part outboundSMSPart) error {
	sentAt := time.Now()
	if s.delivery != nil {
		if err := s.delivery.UpsertSMSDeliveryPart(messageID, part.number, part.callID, int(part.rpMR), smsDeliveryStatePending, sentAt); err != nil {
			return s.recordOutboundSMSFailure(messageID, part, fmt.Errorf("persist pending part: %w", err))
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.smsTransactionTimeout <= 0 {
		return s.recordOutboundSMSFailure(messageID, part, errors.New("SMS transaction timeout is not configured"))
	}
	transactionCtx, cancel := context.WithTimeout(ctx, s.smsTransactionTimeout)
	defer cancel()
	response, err := s.transport.RoundTrip(transactionCtx, part.request)
	if err != nil {
		transactionErr := classifySMSTransactionError(ctx, transactionCtx, s.smsTransactionTimeout, err)
		persistErr := s.persistOutboundSIPResult(messageID, part.number, 0, smsDeliveryStateFailed, transactionErr.Error())
		return s.recordOutboundSMSFailure(messageID, part, errors.Join(transactionErr, persistErr))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = fmt.Errorf("MESSAGE rejected with status %d (%s)", response.StatusCode, strings.TrimSpace(response.Reason))
		persistErr := s.persistOutboundSIPResult(
			messageID, part.number, response.StatusCode, smsDeliveryStateFailed, err.Error(),
		)
		return s.recordOutboundSMSFailure(messageID, part, errors.Join(err, persistErr))
	}
	if err := s.persistOutboundSIPResult(messageID, part.number, response.StatusCode, smsDeliveryStatePending, ""); err != nil {
		return s.recordOutboundSMSFailure(messageID, part, err)
	}
	return nil
}

func classifySMSTransactionError(
	callerCtx, transactionCtx context.Context,
	timeout time.Duration,
	transactionErr error,
) error {
	if callerErr := callerCtx.Err(); callerErr != nil {
		if errors.Is(callerErr, context.Canceled) {
			return fmt.Errorf("MESSAGE transaction canceled by caller: %w", callerErr)
		}
		return fmt.Errorf("MESSAGE transaction caller deadline exceeded: %w", callerErr)
	}
	if errors.Is(transactionCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("MESSAGE final response timeout after %s: %w", timeout, context.DeadlineExceeded)
	}
	return fmt.Errorf("MESSAGE transaction failed: %w", transactionErr)
}

func (s *Service) persistOutboundSIPResult(
	messageID string,
	partNo, sipCode int,
	state, errText string,
) error {
	if s.delivery == nil {
		return nil
	}
	store, ok := s.delivery.(SMSDeliverySIPResultStore)
	if !ok {
		return errors.New("persist MESSAGE result: delivery store does not support SIP result persistence")
	}
	if err := store.MarkSMSDeliveryPartSIPResult(
		messageID, partNo, sipCode, state, errText, time.Now(),
	); err != nil {
		return fmt.Errorf("persist MESSAGE result: %w", err)
	}
	return nil
}

func (s *Service) createOutboundDelivery(messageID, recipient, text string, total int) error {
	if s.delivery == nil {
		return nil
	}
	if err := s.delivery.CreateSMSDelivery(messageID, s.cfg.IMSI, s.cfg.DeviceID, recipient, text, total, time.Now()); err != nil {
		return fmt.Errorf("imscore: create SMS delivery: %w", err)
	}
	return nil
}

func (s *Service) recordOutboundSMSFailure(messageID string, part outboundSMSPart, sendErr error) error {
	if s.delivery == nil {
		return fmt.Errorf("imscore: send SMS part %d: %w", part.number, sendErr)
	}
	persistErr := s.delivery.UpsertSMSDeliveryPart(
		messageID, part.number, part.callID, int(part.rpMR), smsDeliveryStateFailed, time.Now(),
	)
	stateErr := s.delivery.UpdateSMSDeliveryState(messageID, smsDeliveryStateFailed, sendErr.Error(), 0, time.Now())
	if persistErr != nil || stateErr != nil {
		return errors.Join(fmt.Errorf("imscore: send SMS part %d: %w", part.number, sendErr), persistErr, stateErr)
	}
	return fmt.Errorf("imscore: send SMS part %d: %w", part.number, sendErr)
}

func (s *Service) publishOutboundSMS(recipient, text string, total int) {
	s.bus.Publish(&events.EventSMSSent{
		DevID: s.cfg.DeviceID, TargetURI: recipient, Content: text,
		Time: time.Now(), TotalParts: total,
	})
}

func (s *Service) allocateSMSRPMR() byte {
	s.mu.Lock()
	reference := s.nextSMSRPMR
	s.nextSMSRPMR++
	s.mu.Unlock()
	return reference
}

func (s *Service) allocateSMSConcatReference() byte {
	s.mu.Lock()
	s.nextSMSConcatRef++
	if s.nextSMSConcatRef == 0 {
		s.nextSMSConcatRef++
	}
	reference := s.nextSMSConcatRef
	s.mu.Unlock()
	return reference
}

func normalizeSMSRecipient(value string) (string, error) {
	value = strings.TrimSpace(value)
	var normalized strings.Builder
	for index, character := range value {
		if character >= '0' && character <= '9' {
			normalized.WriteRune(character)
			continue
		}
		if character == '+' && index == 0 {
			normalized.WriteRune(character)
			continue
		}
		if strings.ContainsRune(" -()", character) {
			continue
		}
		return "", fmt.Errorf("imscore: invalid SMS recipient %q", value)
	}
	recipient := normalized.String()
	if recipient == "" || recipient == "+" {
		return "", errors.New("imscore: SMS recipient is empty")
	}
	return recipient, nil
}
