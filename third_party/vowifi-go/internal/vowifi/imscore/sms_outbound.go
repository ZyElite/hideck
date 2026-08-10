package imscore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	outboundSMSTransactionTimeout   = 1200 * time.Millisecond
	defaultSMSDeliveryReportTimeout = 25 * time.Second
	pendingSMSReportRetention       = 2 * time.Minute
	outboundSMSInterPartDelay       = 250 * time.Millisecond
	// Recovered from the v1.5.5 data value used by dispatchSMSSendAccepted.
	smsSendAcceptedExpiresHint int64 = 1_200_000_000
	smsDeliveryStatePending          = "pending"
	smsDeliveryStateFailed           = "failed"
)

type outboundSMSPart struct {
	number  int
	rpMR    byte
	callID  string
	request string
	pending *smsPendingInfo
}

type smsSendEnvironment struct {
	recipient string
	text      string
	parts     []outboundSMSPart
	messageID string
}

type smsSubmitReportWait struct {
	messageID   string
	part        outboundSMSPart
	pending     *smsPendingInfo
	dispatchErr error
}

func (s *Service) sendOutboundSMS(
	ctx context.Context,
	to, text string,
	opts SendOptions,
) (outcome SendOutcome, returnErr error) {
	if s != nil {
		s.smsSendMu.Lock()
		defer s.smsSendMu.Unlock()
	}
	ctx = common.WithTraceID(ctx, common.TraceID(ctx))
	if s != nil {
		traceID := common.TraceID(ctx)
		defer func() { s.recordSMSSendStatus(traceID, returnErr) }()
	}
	environment, err := s.prepareSendEnv(ctx, to, text, opts)
	if err != nil {
		return SendOutcome{}, err
	}
	return s.executeSMSDelivery(ctx, environment)
}

func (s *Service) prepareSendEnv(
	_ context.Context,
	to, text string,
	opts SendOptions,
) (*smsSendEnvironment, error) {
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
	if _, err := s.resolveSendRoute(recipient); err != nil {
		return nil, err
	}
	parts, err := s.buildOutboundSMSParts(recipient, text, opts)
	if err != nil {
		return nil, err
	}
	return &smsSendEnvironment{
		recipient: recipient, text: text, parts: parts, messageID: uuid.NewString(),
	}, nil
}

func (s *Service) resolveSendRoute(recipient string) (string, error) {
	domain := ""
	if s != nil && s.cfg != nil {
		domain = strings.TrimSpace(s.cfg.Domain)
	}
	if domain == "" || strings.ContainsAny(domain, "\r\n") {
		return "", errors.New("imscore: SMS route domain is unavailable")
	}
	return fmt.Sprintf("sip:%s@%s;user=phone", recipient, domain), nil
}

func (s *Service) executeSMSDelivery(
	ctx context.Context,
	environment *smsSendEnvironment,
) (SendOutcome, error) {
	if environment == nil {
		return SendOutcome{}, errors.New("imscore: nil SMS send environment")
	}
	parts := environment.parts
	s.dispatchSMSSendAccepted(
		environment.messageID, environment.recipient, environment.text, len(parts),
	)
	if err := s.createOutboundDelivery(
		environment.messageID, environment.recipient, environment.text, len(parts),
	); err != nil {
		return SendOutcome{}, err
	}
	outcome := SendOutcome{
		MessageID: environment.messageID, PartsTotal: len(parts),
		DeliveryState: smsDeliveryStateAcked,
	}
	for index := range parts {
		pending, err := s.sendOutboundSMSPart(ctx, environment.messageID, parts[index])
		if err != nil {
			outcome.DeliveryState = smsDeliveryStateFailed
			return outcome, err
		}
		parts[index].pending = pending
		if index < len(parts)-1 {
			if err := s.waitSMSInterPartDelay(ctx); err != nil {
				outcome.DeliveryState = smsDeliveryStateFailed
				return outcome, s.recordOutboundSMSFailure(environment.messageID, parts[index+1], err)
			}
		}
	}
	if shouldSendTGSuccess(ctx) {
		s.publishOutboundSMS(environment.recipient, environment.text, len(parts))
	}
	return outcome, nil
}

func (s *Service) waitSMSInterPartDelay(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(outboundSMSInterPartDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.stop:
		return errors.New("imscore: service stopped between SMS parts")
	}
}

func (s *Service) recordSMSSendStatus(traceID string, err error) {
	s.lastMTAckMu.Lock()
	s.lastSMSSendTraceID = strings.TrimSpace(traceID)
	s.lastSMSSendAt = time.Now()
	s.lastSMSSendErr = ""
	if err != nil {
		s.lastSMSSendErr = err.Error()
	}
	s.lastMTAckMu.Unlock()
}

func (s *Service) smsSendStatus() (string, time.Time, string) {
	if s == nil {
		return "", time.Time{}, ""
	}
	s.lastMTAckMu.Lock()
	defer s.lastMTAckMu.Unlock()
	return s.lastSMSSendTraceID, s.lastSMSSendAt, s.lastSMSSendErr
}

func (s *Service) buildOutboundSMSParts(recipient, text string, opts SendOptions) ([]outboundSMSPart, error) {
	tpdus, _, err := smscodec.BuildSubmitTPDUsWithOptions(recipient, text, smscodec.SubmitOptions{
		Encoding: smscodec.SMSEncoding(opts.Encoding),
	})
	if err != nil {
		return nil, fmt.Errorf("imscore: encode SMS-SUBMIT: %w", err)
	}
	remoteURI, err := s.resolveSendRoute(recipient)
	if err != nil {
		return nil, err
	}
	parts := make([]outboundSMSPart, 0, len(tpdus))
	for index := range tpdus {
		rpMR, err := s.allocateSMSRPMR()
		if err != nil {
			return nil, fmt.Errorf("imscore: allocate RP-MR for SMS part %d: %w", index+1, err)
		}
		request, err := s.buildOutboundMESSAGE(remoteURI, smscodec.BuildRPData(rpMR, tpdus[index], s.cfg.SMSC))
		if err != nil {
			return nil, fmt.Errorf("imscore: build SMS MESSAGE part %d: %w", index+1, err)
		}
		parts = append(parts, outboundSMSPart{
			number: index + 1, rpMR: rpMR,
			callID: request.CallID().Value(), request: request.String(),
		})
	}
	return parts, nil
}

func (s *Service) sendOutboundSMSPart(
	ctx context.Context,
	messageID string,
	part outboundSMSPart,
) (*smsPendingInfo, error) {
	sentAt := time.Now()
	if s.delivery != nil {
		if err := s.delivery.UpsertSMSDeliveryPart(messageID, part.number, part.callID, int(part.rpMR), smsDeliveryStatePending, sentAt); err != nil {
			return nil, s.recordOutboundSMSFailure(messageID, part, fmt.Errorf("persist pending part: %w", err))
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.smsTransactionTimeout <= 0 {
		return nil, s.recordOutboundSMSFailure(messageID, part, errors.New("SMS transaction timeout is not configured"))
	}
	transactionCtx, cancel := context.WithTimeout(ctx, s.smsTransactionTimeout)
	defer cancel()
	pending, response, err := s.dispatchSubmitPartWithRetry(transactionCtx, messageID, part, sentAt)
	if err != nil {
		transactionErr := classifySMSTransactionError(ctx, transactionCtx, s.smsTransactionTimeout, err)
		wait := smsSubmitReportWait{
			messageID: messageID, part: part, pending: pending, dispatchErr: transactionErr,
		}
		if isSoftMOSubmitProbeTimeout(transactionErr, sipResponseCode(response), s.cfg.Transport) {
			return s.preservePendingSubmit(wait)
		}
		if shouldWaitForSubmitReport(ctx, response, transactionErr) {
			return s.waitForSubmitReport(ctx, wait)
		}
		s.takePendingSMSByCallID(part.callID)
		persistErr := s.persistOutboundSIPResult(messageID, part.number, 0, smsDeliveryStateFailed, transactionErr.Error())
		return nil, s.recordOutboundSMSFailure(messageID, part, errors.Join(transactionErr, persistErr))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		err = fmt.Errorf("MESSAGE rejected with status %d (%s)", response.StatusCode, strings.TrimSpace(response.Reason))
		persistErr := s.persistOutboundSIPResult(
			messageID, part.number, response.StatusCode, smsDeliveryStateFailed, err.Error(),
		)
		return nil, s.recordOutboundSMSFailure(messageID, part, errors.Join(err, persistErr))
	}
	if err := s.persistAcceptedSIPPart(messageID, part, response.StatusCode); err != nil {
		s.takePendingSMSByCallID(part.callID)
		return nil, s.recordOutboundSMSFailure(messageID, part, err)
	}
	s.expirePendingSMSAfter(part.callID, pendingSMSReportRetention)
	return pending, nil
}

func shouldWaitForSubmitReport(callerCtx context.Context, response *sip.Response, dispatchErr error) bool {
	if dispatchErr == nil {
		return false
	}
	if callerCtx != nil && callerCtx.Err() != nil {
		return false
	}
	return response == nil || response.StatusCode <= 0
}

func sipResponseCode(response *sip.Response) int {
	if response == nil {
		return 0
	}
	return response.StatusCode
}

func (s *Service) preservePendingSubmit(wait smsSubmitReportWait) (*smsPendingInfo, error) {
	persistErr := s.persistOutboundSIPResult(
		wait.messageID, wait.part.number, 0, smsDeliveryStatePending, wait.dispatchErr.Error(),
	)
	if persistErr == nil && s.delivery != nil {
		persistErr = s.publishSMSDeliveryStatus(wait.messageID)
	}
	if persistErr != nil {
		s.takePendingSMSByCallID(wait.part.callID)
		return nil, s.recordOutboundSMSFailure(
			wait.messageID, wait.part, errors.Join(wait.dispatchErr, persistErr),
		)
	}
	s.expirePendingSMSAfter(wait.part.callID, pendingSMSReportRetention)
	return wait.pending, nil
}

func (s *Service) waitForSubmitReport(
	ctx context.Context,
	wait smsSubmitReportWait,
) (*smsPendingInfo, error) {
	result, err := s.waitDeliveryReport(ctx, wait.pending, s.smsReportTimeout)
	if err != nil {
		waitErr := errors.Join(wait.dispatchErr, err)
		persistErr := s.persistOutboundSIPResult(
			wait.messageID, wait.part.number, 0, smsDeliveryStateFailed, waitErr.Error(),
		)
		return nil, s.recordOutboundSMSFailure(
			wait.messageID, wait.part, errors.Join(waitErr, persistErr),
		)
	}
	if result.Status == smsDeliveryStateFailed {
		reportErr := errors.New(firstNonBlank(result.Reason, "SMS delivery report failed"))
		return nil, s.recordOutboundSMSFailure(wait.messageID, wait.part, reportErr)
	}
	return wait.pending, nil
}

func (s *Service) persistAcceptedSIPPart(messageID string, part outboundSMSPart, sipCode int) error {
	if err := s.persistOutboundSIPResult(messageID, part.number, sipCode, smsDeliveryStateAcked, ""); err != nil {
		return err
	}
	if s.delivery == nil {
		return nil
	}
	match, err := s.delivery.MarkSMSDeliveryPartReport(
		part.callID, part.callID, s.cfg.DeviceID, int(part.rpMR),
		smsDeliveryStateAcked, sipCode, 0, "", time.Now(),
	)
	if err != nil {
		return fmt.Errorf("persist accepted MESSAGE: %w", err)
	}
	if !match.Matched && strings.TrimSpace(match.MessageID) == "" {
		return errors.New("persist accepted MESSAGE: delivery part did not match")
	}
	if err := s.delivery.RecomputeSMSDelivery(messageID, time.Now()); err != nil {
		return fmt.Errorf("recompute accepted MESSAGE: %w", err)
	}
	return s.publishSMSDeliveryStatus(messageID)
}

func (s *Service) dispatchSubmitPartWithRetry(
	ctx context.Context,
	messageID string,
	part outboundSMSPart,
	sentAt time.Time,
) (*smsPendingInfo, *sip.Response, error) {
	message, err := parseSIPMessage(part.request)
	if err != nil {
		return nil, nil, fmt.Errorf("parse outbound MESSAGE: %w", err)
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return nil, nil, errors.New("outbound SMS payload is not a SIP request")
	}
	request, err = s.buildOutboundRequest(request)
	if err != nil {
		return nil, nil, err
	}
	cseq := uint32(0)
	if request.CSeq() != nil {
		cseq = request.CSeq().SeqNo
	}
	pending := &smsPendingInfo{
		MessageID: messageID, PartNo: part.number, RPMR: int(part.rpMR),
		To: request.Recipient.String(), TargetURI: request.Recipient.String(),
		CSeq: cseq, CreatedAt: sentAt, RespCh: make(chan smsSendResult, 1),
	}
	s.registerPendingSMS(part.callID, pending)
	s.recordOutboundSMSAudit(common.TraceID(ctx), part.callID, pending.To, len(request.Body()))
	result, dispatchErr := s.dispatchOutboundMESSAGE(ctx, "mo-submit", request, s.smsTransactionTimeout)
	var response *sip.Response
	if result.SIPCode > 0 {
		response = sip.NewResponse(result.SIPCode, SIPStatusText(result.SIPCode))
	}
	if dispatchErr != nil {
		return pending, response, dispatchErr
	}
	if response == nil {
		return pending, nil, errors.New("MESSAGE transaction completed without a final response")
	}
	if result.SIPCode < 200 || result.SIPCode >= 300 {
		s.takePendingSMSByCallID(part.callID)
	}
	return pending, response, nil
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
		return nil
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
	return s.completeFailure(messageID, part, sendErr)
}

func (s *Service) completeFailure(messageID string, part outboundSMSPart, sendErr error) error {
	fields := []interface{}{
		"message_id", messageID, "part", part.number, "call_id", part.callID,
		"rp_mr", part.rpMR, "err", sendErr,
	}
	logging.RunDebug("IMS SMS delivery failed", appendRPErrorFields(fields, sendErr.Error())...)
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
	at := time.Now()
	s.publishLogNotification(formatVoWiFiSMSSentMessage(s.cfg.DeviceID, recipient, text, at, total))
	s.bus.Publish(&events.EventSMSSent{
		DevID: s.cfg.DeviceID, TargetURI: recipient, Content: text,
		Time: at, TotalParts: total,
	})
}

func (s *Service) publishSMSSendAccepted(messageID, recipient, text string, total int) {
	acceptedAt := time.Now()
	s.bus.Publish(events.EventSMSSendAccepted{
		DevID: s.cfg.DeviceID, MessageID: messageID, TargetURI: recipient,
		Content: text, PartsTotal: total, AcceptedAt: acceptedAt,
		ExpiresHint: smsSendAcceptedExpiresHint, Time: acceptedAt,
	})
}

func (s *Service) dispatchSMSSendAccepted(messageID, recipient, text string, total int) {
	s.publishSMSSendAccepted(messageID, recipient, text, total)
}

func (s *Service) allocateSMSRPMR() (byte, error) {
	if s == nil || s.smsRandom == nil {
		return 0, errors.New("SMS RP-MR randomness is unavailable")
	}
	var reference [1]byte
	if _, err := io.ReadFull(s.smsRandom, reference[:]); err != nil {
		return 0, err
	}
	return reference[0], nil
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
	return normalizeE164(recipient), nil
}
