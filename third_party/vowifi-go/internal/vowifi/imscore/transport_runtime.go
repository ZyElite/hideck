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

type inboundSIPResult struct {
	response   string
	afterReply func()
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

func (s *Service) handleInboundSIP(ctx context.Context, raw string) (inboundSIPResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return inboundSIPResult{}, ctx.Err()
	default:
	}
	method := strings.ToUpper(sipRequestMethod(raw))
	if method == "" {
		return inboundSIPResult{}, errors.New("imscore: invalid inbound SIP message")
	}
	switch method {
	case "NOTIFY":
		logging.Info("IMS NOTIFY(reg) 已确认", "event", rawSIPHeaderValue(raw, "Event"))
		response, err := buildSIPRequestResponse(raw, 200)
		return inboundSIPResult{response: response}, err
	case "OPTIONS":
		response, err := buildSIPRequestResponse(raw, 200)
		return inboundSIPResult{response: response}, err
	case "MESSAGE":
		return s.handleInboundSMS(raw)
	default:
		response, err := buildSIPRequestResponse(raw, 405)
		return inboundSIPResult{response: response}, err
	}
}

func (s *Service) dispatchInboundSIP(raw string, reply func(string) error) error {
	response := parseSIPResponse(raw)
	if response != nil && response.StatusCode != 0 {
		s.transport.DeliverResponse(response)
		return nil
	}
	s.transport.DeliverRequest(raw)
	result, err := s.handleInboundSIP(context.Background(), raw)
	if result.response == "" {
		return err
	}
	if reply == nil {
		return errors.New("imscore: inbound SIP reply path is unavailable")
	}
	if err := reply(result.response); err != nil {
		return err
	}
	if result.afterReply != nil {
		s.networkDone.Add(1)
		go func() {
			defer s.networkDone.Done()
			result.afterReply()
		}()
	}
	return err
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
