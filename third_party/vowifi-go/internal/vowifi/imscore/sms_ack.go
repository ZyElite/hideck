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
	response, err := s.sendByMode(outboundSendOperation{
		Context: ctx,
		Mode:    modeCtx,
		Request: request,
		Timeout: inboundSMSAckTimeout,
	})
	if err == nil {
		err = validateRPReportResponse(response)
	}
	if err != nil {
		s.mtAckSendErr.Add(1)
		s.recordMTAckAudit(audit, err)
		return err
	}
	s.mtAckSendOK.Add(1)
	s.recordMTAckAudit(audit, nil)
	return nil
}

func validateRPReportResponse(response *sipResponse) error {
	if response == nil {
		return errors.New("IMS RP report returned no SIP response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("IMS RP report rejected: SIP %d %s", response.StatusCode, strings.TrimSpace(response.Reason))
	}
	return nil
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
