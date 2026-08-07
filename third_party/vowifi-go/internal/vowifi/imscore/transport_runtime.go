package imscore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

// SMSReceiverStatus is a snapshot of the live SIP receiver.
type SMSReceiverStatus struct {
	Active       bool
	Transport    string
	LocalAddress string
}

func (s *Service) receiverStarted() {
	s.receiverMu.Lock()
	s.activeReceivers++
	active := s.activeReceivers > 0
	s.receiverMu.Unlock()
	s.setSMSReceiverReady(active)
}

func (s *Service) receiverStopped() {
	s.receiverMu.Lock()
	if s.activeReceivers > 0 {
		s.activeReceivers--
	}
	active := s.activeReceivers > 0
	s.receiverMu.Unlock()
	s.setSMSReceiverReady(active)
}

func (s *Service) receiverStatus() SMSReceiverStatus {
	if s == nil || s.cfg == nil {
		return SMSReceiverStatus{}
	}
	s.receiverMu.Lock()
	active := s.activeReceivers > 0
	s.receiverMu.Unlock()
	return SMSReceiverStatus{
		Active: active, Transport: strings.ToLower(strings.TrimSpace(s.cfg.Transport)),
		LocalAddress: net.JoinHostPort(s.cfg.LocalIP.String(), fmt.Sprint(s.cfg.LocalPort)),
	}
}

func (s *Service) handleInboundSIP(ctx context.Context, raw string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	method := strings.ToUpper(sipRequestMethod(raw))
	if method == "" {
		return "", errors.New("imscore: invalid inbound SIP message")
	}
	switch method {
	case "NOTIFY":
		logging.Info("IMS NOTIFY(reg) 已确认", "event", rawSIPHeaderValue(raw, "Event"))
		return buildSIPRequestResponse(raw, 200)
	case "OPTIONS":
		return buildSIPRequestResponse(raw, 200)
	default:
		return buildSIPRequestResponse(raw, 405)
	}
}

func (s *Service) dispatchInboundSIP(raw string, reply func(string) error) error {
	response := parseSIPResponse(raw)
	if response != nil && response.StatusCode != 0 {
		s.transport.DeliverResponse(response)
		return nil
	}
	s.transport.DeliverRequest(raw)
	wireResponse, err := s.handleInboundSIP(context.Background(), raw)
	if err != nil {
		return err
	}
	if reply == nil {
		return errors.New("imscore: inbound SIP reply path is unavailable")
	}
	return reply(wireResponse)
}

func (s *Service) writeSIPStream(conn net.Conn, response string) error {
	if conn == nil {
		return errors.New("imscore: nil SIP stream")
	}
	s.sipWriteMu.Lock()
	defer s.sipWriteMu.Unlock()
	if _, err := io.WriteString(conn, response); err != nil {
		return fmt.Errorf("imscore: write SIP stream: %w", err)
	}
	return nil
}
