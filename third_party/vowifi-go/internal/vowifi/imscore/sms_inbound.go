package imscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	imsSMSContentType       = "application/vnd.3gpp.sms"
	rpCauseTemporaryFailure = byte(41)
	inboundSMSAckTimeout    = 10 * time.Second
)

type inboundSMS struct {
	sender    string
	targetURI string
	content   string
	timestamp time.Time
	rpMR      byte
}

func (s *Service) handleInboundSMS(raw string) (inboundSIPResult, error) {
	if normalizedContentType(rawSIPHeaderValue(raw, "Content-Type")) != imsSMSContentType {
		response, err := buildSIPRequestResponse(raw, 415)
		return inboundSIPResult{response: response}, err
	}
	body, err := rawSIPBody(raw)
	if err != nil {
		return s.inboundSMSProtocolError(raw, 400, 0, false, err)
	}
	rpdu := smscodec.DecodeBodyMaybeHex(body)
	info := smscodec.ClassifyRPDU(rpdu)
	if info.Kind != smscodec.RPDUKindData || info.RawType != 0x01 {
		return s.inboundSMSProtocolError(raw, 400, info.MR, false, fmt.Errorf("unsupported inbound RPDU type 0x%02x", info.RawType))
	}
	message, err := decodeInboundRPData(raw, rpdu)
	if err != nil {
		return s.inboundSMSProtocolError(raw, 400, info.MR, true, err)
	}
	response, err := buildSIPRequestResponse(raw, 200)
	if err != nil {
		return inboundSIPResult{}, err
	}
	s.publishInboundSMS(message)
	return inboundSIPResult{
		response: response,
		afterReply: func() {
			s.sendInboundSMSControl(raw, smscodec.BuildRPAck(message.rpMR))
		},
	}, nil
}

func decodeInboundRPData(raw string, rpdu []byte) (inboundSMS, error) {
	rpMR, originator, _, tpdu, err := smscodec.ParseRPDataWithAddresses(rpdu)
	if err != nil {
		return inboundSMS{}, err
	}
	if len(tpdu) == 0 || tpdu[0]&0x03 != 0 {
		return inboundSMS{}, errors.New("inbound RP-DATA does not contain SMS-DELIVER")
	}
	decoded := smscodec.DecodeDeliverTPDU(tpdu)
	if decoded.Err != nil {
		return inboundSMS{}, fmt.Errorf("decode SMS-DELIVER: %w", decoded.Err)
	}
	sender := strings.TrimSpace(decoded.Sender)
	if sender == "" {
		sender = strings.TrimSpace(originator)
	}
	return inboundSMS{
		sender: sender, targetURI: firstSIPHeaderURI(rawSIPHeaderValue(raw, "To")),
		content: decoded.Text, timestamp: decoded.Timestamp, rpMR: rpMR,
	}, nil
}

func (s *Service) inboundSMSProtocolError(raw string, status int, rpMR byte, sendRPError bool, protocolErr error) (inboundSIPResult, error) {
	response, responseErr := buildSIPRequestResponse(raw, status)
	if responseErr != nil {
		return inboundSIPResult{}, responseErr
	}
	result := inboundSIPResult{response: response}
	if sendRPError {
		result.afterReply = func() {
			s.sendInboundSMSControl(raw, smscodec.BuildRPError(rpMR, rpCauseTemporaryFailure))
		}
	}
	return result, protocolErr
}

func (s *Service) publishInboundSMS(message inboundSMS) {
	if message.timestamp.IsZero() {
		message.timestamp = time.Now()
	}
	s.bus.Publish(&events.EventSMSReceived{
		DevID: s.cfg.DeviceID, Sender: message.sender, TargetURI: message.targetURI,
		Content: message.content, Time: message.timestamp,
	})
}

func (s *Service) sendInboundSMSControl(inbound string, body []byte) {
	request, err := s.buildInboundSMSControlRequest(inbound, body)
	if err != nil {
		logging.WarnRate("ims-sms-rp-control-build", "IMS SMS RP control build failed", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), inboundSMSAckTimeout)
	defer cancel()
	response, err := s.transport.RoundTrip(ctx, request)
	if err != nil {
		logging.WarnRate("ims-sms-rp-control-send", "IMS SMS RP control transaction failed", "err", err)
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		logging.WarnRate("ims-sms-rp-control-reject", "IMS SMS RP control rejected", "status", response.StatusCode)
	}
}

func (s *Service) buildInboundSMSControlRequest(inbound string, body []byte) (string, error) {
	remoteURI := firstSIPHeaderURI(rawSIPHeaderValue(inbound, "From"))
	if remoteURI == "" {
		return "", errors.New("inbound SMS has no remote identity")
	}
	return s.buildSMSMESSAGE(remoteURI, body)
}

func normalizedContentType(value string) string {
	value, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(value)
}

func rawSIPBody(raw string) ([]byte, error) {
	_, body, ok := strings.Cut(raw, "\r\n\r\n")
	if !ok {
		return nil, errors.New("SIP MESSAGE has no header terminator")
	}
	length, err := parseContentLength(rawSIPHeaderValue(raw, "Content-Length"))
	if err != nil {
		return nil, err
	}
	if len(body) != length {
		return nil, fmt.Errorf("SIP MESSAGE body length %d does not match Content-Length %d", len(body), length)
	}
	return []byte(body), nil
}

func parseContentLength(value string) (int, error) {
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &length); err != nil || length < 0 {
		return 0, errors.New("invalid SIP Content-Length")
	}
	return length, nil
}
