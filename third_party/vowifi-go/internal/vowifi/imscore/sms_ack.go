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

type rpReportRequest struct {
	Inbound     string
	Body        []byte
	RPMR        byte
	Fingerprint string
}

const (
	rpReportInitialDelay = 100 * time.Millisecond
	rpReportRetryDelay   = time.Second
	rpReportMaxAttempts  = 4
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

func (s *Service) sendRPReport(report rpReportRequest) error {
	request, err := s.buildRPAckMESSAGE(report.Inbound, report.Body)
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
		transport: modeCtx.Transport, callID: request.CallID().Value(), rpMR: int(report.RPMR),
		fingerprint: report.Fingerprint, at: time.Now(),
	}
	logging.RunDebug("IMS RP report send",
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

func (s *Service) sendRPReportWithRetry(report rpReportRequest) {
	err := s.sendRPReportWithRetryPolicy(
		report, rpReportInitialDelay, rpReportRetryDelay,
	)
	if err != nil {
		logging.WarnRate("smsip-rp-report-retry-exhausted:"+s.cfg.DeviceID, 30*time.Second,
			"IMS RP report delivery failed after retries",
			"device", s.cfg.DeviceID, "attempts", rpReportMaxAttempts,
			"rp_mr", int(report.RPMR), "error", err)
	}
}

func (s *Service) sendRPReportWithRetryPolicy(
	report rpReportRequest,
	initialDelay time.Duration,
	retryDelay time.Duration,
) error {
	if !s.waitSMSRetryDelay(initialDelay) {
		return nil
	}
	delay := retryDelay
	var lastErr error
	for attempt := 0; attempt < rpReportMaxAttempts; attempt++ {
		if attempt > 0 && !s.waitSMSRetryDelay(delay) {
			return nil
		}
		lastErr = s.sendRPReport(report)
		if lastErr == nil {
			return nil
		}
		delay *= 2
	}
	return lastErr
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

func resolveRpAckTarget(assertedIdentity, from string) (string, error) {
	if target := firstSIPHeaderURI(assertedIdentity); target != "" {
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
