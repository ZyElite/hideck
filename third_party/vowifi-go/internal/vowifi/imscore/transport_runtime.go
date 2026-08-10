package imscore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

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

type inboundSIPDispatch struct {
	raw         string
	reply       func(string) error
	transaction *serverSIPTransaction
	events      imsEventPublishReceipt
}

func (s *Service) receiverStarted() {
	s.receiverMu.Lock()
	s.activeReceivers++
	active := s.activeReceivers > 0
	startStats := s.activeReceivers == 1
	s.receiverMu.Unlock()
	s.setSMSReceiverReady(active)
	if startStats {
		s.startInboundStatsLogger(context.Background(), inboundStatsLoggerOptions{
			Interval: defaultInboundStatsInterval,
		})
	}
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
	return s.handleInboundSIPDispatch(ctx, inboundSIPDispatch{
		raw: raw, reply: reply, transaction: transaction,
	})
}

func (s *Service) handleInboundSIPDispatch(
	ctx context.Context,
	dispatch inboundSIPDispatch,
) (inboundSIPResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return inboundSIPResult{}, ctx.Err()
	default:
	}
	method := strings.ToUpper(sipRequestMethod(dispatch.raw))
	if method == "" {
		return inboundSIPResult{}, errors.New("imscore: invalid inbound SIP message")
	}
	switch method {
	case "NOTIFY":
		response, err := buildSIPRequestResponse(dispatch.raw, 200)
		return inboundSIPResult{response: response, afterReply: func() {
			s.handleRegistrationNotification(dispatch.raw)
		}}, err
	case "OPTIONS":
		response, err := buildSIPRequestResponse(dispatch.raw, 200)
		return inboundSIPResult{response: response}, err
	case "MESSAGE":
		return s.handleInboundSMS(dispatch.raw)
	case "INFO", "BYE":
		result, handled, err := s.handleInboundUSSI(dispatch.raw)
		if handled {
			return result, err
		}
		result, handled, err = s.handleInboundVoice(dispatch)
		if handled {
			return result, err
		}
		response, responseErr := buildSIPRequestResponse(dispatch.raw, 405)
		return inboundSIPResult{response: response}, responseErr
	case "INVITE", "CANCEL", "PRACK", "UPDATE":
		result, handled, err := s.handleInboundVoice(dispatch)
		if handled {
			return result, err
		}
		response, responseErr := buildSIPRequestResponse(dispatch.raw, 405)
		return inboundSIPResult{response: response}, responseErr
	case "ACK":
		dispatch.transaction = nil
		result, handled, err := s.handleInboundVoice(dispatch)
		if handled {
			return result, err
		}
		return inboundSIPResult{}, err
	default:
		response, err := buildSIPRequestResponse(dispatch.raw, 405)
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
	s.UpdateLastPingAt()
	s.inboundSIPParsedMessage.Add(1)
	switch parsed := message.(type) {
	case *sip.Response:
		s.inboundSIPParsedResp.Add(1)
		s.publishIMSEvent(s.buildIMSEventFromResponse(parsed))
		s.transport.DeliverResponse(newSIPResponse(parsed))
		return nil
	case *sip.Request:
		s.inboundSIPParsedRequest.Add(1)
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
	events := s.publishIMSEvent(s.buildInboundRequestEvent(request, transaction))
	responseWriter := reply
	if transaction != nil {
		responseWriter = transaction.respondRaw
	}
	result, err := s.handleInboundSIPDispatch(context.Background(), inboundSIPDispatch{
		raw: raw, reply: responseWriter, transaction: transaction, events: events,
	})
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
