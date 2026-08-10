package imscore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
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
	return s.handleInboundSIPWithReply(ctx, raw, nil)
}

func (s *Service) handleInboundSIPWithReply(ctx context.Context, raw string, reply func(string) error) (inboundSIPResult, error) {
	return s.handleInboundSIPTransaction(ctx, raw, reply, nil)
}

func (s *Service) handleInboundSIPTransaction(
	ctx context.Context,
	raw string,
	reply func(string) error,
	transaction *serverSIPTransaction,
) (inboundSIPResult, error) {
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
		response, err := buildSIPRequestResponse(raw, 200)
		return inboundSIPResult{response: response, afterReply: func() {
			s.handleRegistrationNotification(raw)
		}}, err
	case "OPTIONS":
		response, err := buildSIPRequestResponse(raw, 200)
		return inboundSIPResult{response: response}, err
	case "MESSAGE":
		return s.handleInboundSMS(raw)
	case "INFO", "BYE":
		result, handled, err := s.handleInboundUSSI(raw)
		if handled {
			return result, err
		}
		result, handled, err = s.handleInboundVoice(raw, reply, transaction)
		if handled {
			return result, err
		}
		response, responseErr := buildSIPRequestResponse(raw, 405)
		return inboundSIPResult{response: response}, responseErr
	case "INVITE", "CANCEL", "PRACK", "UPDATE":
		result, handled, err := s.handleInboundVoice(raw, reply, transaction)
		if handled {
			return result, err
		}
		response, responseErr := buildSIPRequestResponse(raw, 405)
		return inboundSIPResult{response: response}, responseErr
	case "ACK":
		result, handled, err := s.handleInboundVoice(raw, reply, nil)
		if handled {
			return result, err
		}
		return inboundSIPResult{}, err
	default:
		response, err := buildSIPRequestResponse(raw, 405)
		return inboundSIPResult{response: response}, err
	}
}

func (s *Service) dispatchInboundSIP(raw string, reply func(string) error) error {
	message, err := parseSIPMessage(raw)
	if err != nil {
		return fmt.Errorf("imscore: parse inbound SIP: %w", err)
	}
	return s.dispatchInboundSIPMessage(message, string(unfoldSIPHeaders([]byte(raw))), reply)
}

func (s *Service) dispatchInboundSIPMessage(message sip.Message, raw string, reply func(string) error) error {
	s.UpdateLastPingAt(time.Now())
	switch parsed := message.(type) {
	case *sip.Response:
		s.transport.DeliverResponse(newSIPResponse(parsed))
		return nil
	case *sip.Request:
		return s.dispatchInboundSIPRequest(parsed, raw, reply)
	default:
		return errors.New("imscore: unsupported inbound SIP message")
	}
}

func (s *Service) dispatchInboundSIPRequest(
	request *sip.Request,
	raw string,
	reply func(string) error,
) error {
	s.transport.DeliverRequest(raw)
	transaction, handled, err := s.acceptServerRequest(request, raw, reply)
	if handled || err != nil {
		return err
	}
	responseWriter := reply
	if transaction != nil {
		responseWriter = transaction.respondRaw
	}
	result, err := s.handleInboundSIPTransaction(
		context.Background(), raw, responseWriter, transaction,
	)
	if result.response == "" {
		if err != nil && transaction != nil {
			transaction.fail(err, true)
		}
		return err
	}
	if request.IsAck() {
		return errors.New("imscore: ACK handler attempted to send a SIP response")
	}
	if responseWriter == nil {
		return errors.New("imscore: inbound SIP reply path is unavailable")
	}
	if responseErr := responseWriter(result.response); responseErr != nil {
		return responseErr
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
