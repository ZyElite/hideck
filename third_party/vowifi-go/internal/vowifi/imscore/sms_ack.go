package imscore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	rpAckInitialDelay = 100 * time.Millisecond
	rpAckRetryDelay   = time.Second
	rpAckMaxAttempts  = 4
)

type mtAckAudit struct {
	traceID, target, destination, transport, callID, fingerprint string
	rpMR                                                         int
	at                                                           time.Time
}

func (s *Service) recordMTAckAudit(audit mtAckAudit, err error) {
	if s == nil {
		return
	}
	s.lastMTAckMu.Lock()
	s.lastMTAckTraceID = strings.TrimSpace(audit.traceID)
	s.lastMTAckTarget = strings.TrimSpace(audit.target)
	s.lastMTAckDestination = strings.TrimSpace(audit.destination)
	s.lastMTAckTransport = strings.TrimSpace(audit.transport)
	s.lastMTAckCallID = strings.TrimSpace(audit.callID)
	s.lastMTAckRPMR = audit.rpMR
	s.lastMTAckFingerprint = strings.TrimSpace(audit.fingerprint)
	s.lastMTAckAt = audit.at
	s.lastMTAckErr = ""
	if err != nil {
		s.lastMTAckErr = err.Error()
	}
	s.lastMTAckMu.Unlock()
}

func (s *Service) sendRpAckWithRetry(inbound string, body []byte, rpMR byte, fingerprint string) {
	s.sendRpAckWithRetryPolicy(inbound, body, rpMR, fingerprint, rpAckInitialDelay, rpAckRetryDelay)
}

func (s *Service) sendRpAckWithRetryPolicy(
	inbound string,
	body []byte,
	rpMR byte,
	fingerprint string,
	initialDelay, retryDelay time.Duration,
) {
	if !s.waitSMSRetryDelay(initialDelay) {
		return
	}
	delay := retryDelay
	var lastErr error
	for attempt := 0; attempt < rpAckMaxAttempts; attempt++ {
		if attempt > 0 && !s.waitSMSRetryDelay(delay) {
			return
		}
		lastErr = s.sendRpAck(inbound, body, rpMR, fingerprint)
		if lastErr == nil {
			return
		}
		delay *= 2
	}
	logging.WarnRate("smsip_rp_ack_retry_exhausted", "IMS RP-ACK retries exhausted",
		"attempts", rpAckMaxAttempts, "err", lastErr)
}

func (s *Service) waitSMSRetryDelay(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.stop:
		return false
	}
}

func (s *Service) sendRpAck(inbound string, body []byte, rpMR byte, fingerprint string) error {
	request, err := s.buildRPAckMESSAGE(inbound, body)
	if err != nil {
		s.mtAckSendErr.Add(1)
		return err
	}
	modeCtx, err := s.resolveOutboundModeContext("mt-rp-ack", request)
	if err != nil {
		s.mtAckSendErr.Add(1)
		return err
	}
	traceID := common.NewTraceID()
	audit := mtAckAudit{
		traceID: traceID, target: request.Recipient.String(), destination: destinationFromContext(modeCtx),
		transport: modeCtx.Transport, callID: request.CallID().Value(), rpMR: int(rpMR),
		fingerprint: fingerprint, at: time.Now(),
	}
	logging.RunDebug("IMS RP ACK send",
		"trace_id", traceID, "target", audit.target, "destination", audit.destination,
		"transport", audit.transport, "call_id", audit.callID, "rp_mr", audit.rpMR)
	ctx, cancel := context.WithTimeout(common.WithTraceID(context.Background(), traceID), inboundSMSAckTimeout)
	defer cancel()
	err = s.sendOutOfDialogRequest(ctx, modeCtx, request)
	if err != nil {
		s.mtAckSendErr.Add(1)
		s.recordMTAckAudit(audit, err)
		return err
	}
	s.mtAckSendOK.Add(1)
	s.recordMTAckAudit(audit, nil)
	return nil
}

func resolveRpAckTarget(contact, from string) (string, error) {
	if target := firstSIPHeaderURI(contact); target != "" {
		return target, nil
	}
	if target := firstSIPHeaderURI(from); target != "" {
		return target, nil
	}
	return "", errors.New("IMS RP-ACK target is unavailable")
}

func routeFromRemoteEndpoint(host string, port int) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if net.ParseIP(host) == nil || port < 1 {
		return ""
	}
	return fmt.Sprintf("<sip:%s;lr>", net.JoinHostPort(host, fmt.Sprint(port)))
}
