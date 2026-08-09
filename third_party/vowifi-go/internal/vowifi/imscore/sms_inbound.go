package imscore

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const (
	imsSMSContentType       = "application/vnd.3gpp.sms"
	rpCauseTemporaryFailure = byte(41)
	inboundSMSAckTimeout    = 10 * time.Second
	inboundSMSFragmentTTL   = 10 * time.Minute
)

type inboundSMS struct {
	sender    string
	targetURI string
	content   string
	timestamp time.Time
	rpMR      byte
	concatRef int
	refBits   int
	total     int
	partNo    int
}

type decodedInboundSMSRequest struct {
	rpdu []byte
	info smscodec.RPDUInfo
}

func inboundAckHeaders(request *sip.Request) (string, string, string, string) {
	if request == nil {
		return "", "", "", ""
	}
	callID := sipkit.FirstHeaderValue(request, "Call-ID", false)
	inReplyTo := sipkit.FirstHeaderValue(request, "In-Reply-To", false)
	from := sipkit.FirstHeaderValue(request, "From", false)
	to := sipkit.FirstHeaderValue(request, "To", false)
	if strings.TrimSpace(inReplyTo) == "" {
		inReplyTo = callID
	}
	if strings.TrimSpace(to) == "" {
		to = from
	}
	return callID, inReplyTo, from, to
}

func (s *Service) decodeInboundSMSRequest(raw string) (*decodedInboundSMSRequest, error) {
	if normalizedContentType(rawSIPHeaderValue(raw, "Content-Type")) != imsSMSContentType {
		return nil, errors.New("unsupported IMS SMS content type")
	}
	body, err := rawSIPBody(raw)
	if err != nil {
		return nil, err
	}
	rpdu, err := smscodec.DecodeBodyMaybeHex(body)
	if err != nil {
		return nil, fmt.Errorf("decode RPDU body: %w", err)
	}
	decoded := &decodedInboundSMSRequest{rpdu: rpdu, info: smscodec.ClassifyRPDU(rpdu)}
	if err := parseInboundRPDU(rpdu); err != nil {
		return decoded, err
	}
	return decoded, nil
}

func (s *Service) handleInboundSMS(raw string) (inboundSIPResult, error) {
	decoded, err := s.decodeInboundSMSRequest(raw)
	if err != nil && decoded == nil && normalizedContentType(rawSIPHeaderValue(raw, "Content-Type")) != imsSMSContentType {
		response, err := buildSIPRequestResponse(raw, 415)
		return inboundSIPResult{response: response}, err
	}
	if err != nil {
		info := smscodec.RPDUInfo{}
		if decoded != nil {
			info = decoded.info
		}
		return s.inboundSMSProtocolError(
			raw, 400, info.MR, info.Kind == smscodec.RPDUKindData, err,
		)
	}
	return s.routeDecodedInboundSMS(raw, decoded)
}

func (s *Service) routeDecodedInboundSMS(raw string, decoded *decodedInboundSMSRequest) (inboundSIPResult, error) {
	if decoded == nil {
		return s.inboundSMSProtocolError(raw, 400, 0, false, errors.New("empty decoded IMS SMS"))
	}
	info, rpdu := decoded.info, decoded.rpdu
	switch {
	case info.Kind == smscodec.RPDUKindData && info.RawType == 0x01:
		return s.handleInboundSMSData(raw, rpdu, info.MR)
	case info.Kind == smscodec.RPDUKindAck && info.RawType == 0x03:
		return s.handleInboundSMSReport(raw, info, "acked", "")
	case info.Kind == smscodec.RPDUKindError && info.RawType == 0x05:
		return s.handleInboundSMSReport(raw, info, "failed", fmt.Sprintf("RP-ERROR cause %d", info.Cause))
	default:
		return s.inboundSMSProtocolError(raw, 400, info.MR, false, fmt.Errorf("unsupported inbound RPDU type 0x%02x", info.RawType))
	}
}

func (s *Service) handleInboundSMSReport(
	raw string,
	info smscodec.RPDUInfo,
	state, errorText string,
) (inboundSIPResult, error) {
	return s.handleInboundRPReport(raw, info, state, errorText)
}

func (s *Service) handleInboundSMSData(raw string, rpdu []byte, rpMR byte) (inboundSIPResult, error) {
	return s.handleInboundRPData(raw, rpdu, rpMR)
}

func (s *Service) handleInboundRPData(raw string, rpdu []byte, rpMR byte) (inboundSIPResult, error) {
	_, _, _, payload, err := smscodec.ParseRPDataWithAddresses(rpdu)
	if err != nil {
		return s.inboundSMSProtocolError(raw, 400, rpMR, true, err)
	}
	if len(payload) > 0 && payload[0]&0x03 == 0x02 {
		return s.handleInboundTPStatusReport(raw, rpMR, payload)
	}
	message, err := decodeInboundRPData(raw, rpdu)
	if err != nil {
		return s.inboundSMSProtocolError(raw, 400, rpMR, true, err)
	}
	response, err := buildSIPRequestResponse(raw, 200)
	if err != nil {
		return inboundSIPResult{}, err
	}
	return s.finalizeInboundSMSData(raw, message, response)
}

func (s *Service) finalizeInboundSMSData(
	raw string,
	message inboundSMS,
	response string,
) (inboundSIPResult, error) {
	shouldDispatch, assembleErr := s.assembleInboundSMS(raw, &message)
	if assembleErr != nil {
		return s.inboundSMSProtocolError(raw, 400, message.rpMR, true, assembleErr)
	}
	if shouldDispatch {
		s.publishInboundSMS(message)
	}
	fingerprint := buildMTSMSFingerprint(message, raw)
	return inboundSIPResult{
		response: response,
		afterReply: func() {
			s.sendRpAckWithRetry(raw, smscodec.BuildRPAck(message.rpMR), message.rpMR, fingerprint)
		},
	}, nil
}

func fragmentLifecycleLogFields(message inboundSMS) []interface{} {
	return []interface{}{
		"sender", normalizeFragmentIdentity(message.sender), "ref", message.concatRef,
		"ref_bits", message.refBits, "total", message.total, "seq", message.partNo,
		"rp_mr", message.rpMR,
	}
}

func decodeInboundRPData(raw string, rpdu []byte) (inboundSMS, error) {
	rpMR, originator, _, tpdu, err := smscodec.ParseRPDataWithAddresses(rpdu)
	if err != nil {
		return inboundSMS{}, err
	}
	if len(tpdu) == 0 || tpdu[0]&0x03 != 0 {
		return inboundSMS{}, errors.New("inbound RP-DATA does not contain SMS-DELIVER")
	}
	sender, content, timestamp, concat, err := smscodec.DecodeDeliverTPDU(tpdu)
	if err != nil {
		return inboundSMS{}, fmt.Errorf("decode SMS-DELIVER: %w", err)
	}
	sender = strings.TrimSpace(sender)
	if sender == "" {
		sender = strings.TrimSpace(originator)
	}
	return inboundSMS{
		sender: sender, targetURI: firstSIPHeaderURI(rawSIPHeaderValue(raw, "To")),
		content: content, timestamp: timestamp, rpMR: rpMR,
		concatRef: concat.Ref, refBits: concat.RefBits,
		total: concat.Total, partNo: concat.Seq,
	}, nil
}

func (s *Service) assembleInboundSMS(raw string, message *inboundSMS) (bool, error) {
	if message == nil {
		return false, errors.New("imscore: nil inbound SMS")
	}
	if message.total <= 1 {
		return s.shouldDispatchMTSMS(*message, raw), nil
	}
	content, complete, err := s.handleSMSFragment(message.sender, &smsFragment{
		Ref: message.concatRef + message.refBits<<16, Total: message.total, Seq: message.partNo,
		Content: message.content, Time: message.timestamp, RpMr: message.rpMR,
		CallID: rawSIPHeaderValue(raw, "Call-ID"), ToURI: message.targetURI,
	})
	if err != nil || !complete {
		return false, err
	}
	message.content = content
	return s.shouldDispatchMTSMS(*message, raw), nil
}

func (s *Service) inboundSMSProtocolError(raw string, status int, rpMR byte, sendRPError bool, protocolErr error) (inboundSIPResult, error) {
	response, responseErr := buildSIPRequestResponse(raw, status)
	if responseErr != nil {
		return inboundSIPResult{}, responseErr
	}
	result := inboundSIPResult{response: response}
	if sendRPError {
		result.afterReply = func() {
			s.sendRpAckWithRetry(raw, smscodec.BuildRPError(rpMR, rpCauseTemporaryFailure), rpMR, "")
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
	s.sendRpAckWithRetry(inbound, body, 0, "")
}

func (s *Service) buildInboundSMSControlRequest(inbound string, body []byte) (string, error) {
	remoteURI, err := resolveRpAckTarget(
		rawSIPHeaderValue(inbound, "Contact"), rawSIPHeaderValue(inbound, "From"),
	)
	if err != nil {
		return "", err
	}
	return s.buildSMSMESSAGE(remoteURI, body)
}

func normalizedContentType(value string) string {
	value, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(value)
}

type regInfoDocument struct {
	Registrations []struct {
		AOR      string `xml:"aor,attr"`
		Contacts []struct {
			ID    string `xml:"id,attr"`
			State string `xml:"state,attr"`
			Event string `xml:"event,attr"`
			URI   string `xml:"uri"`
		} `xml:"contact"`
	} `xml:"registration"`
}

func (s *Service) isMyContactTerminated(raw string) bool {
	if !strings.EqualFold(strings.TrimSpace(rawSIPHeaderValue(raw, "Event")), "reg") {
		return false
	}
	body, err := rawSIPBody(raw)
	if err != nil || len(body) == 0 {
		return false
	}
	var document regInfoDocument
	if err := xml.Unmarshal(body, &document); err != nil {
		return false
	}
	s.mu.RLock()
	contactID := ""
	publicID := ""
	if s.regSession != nil {
		contactID = strings.TrimSpace(s.regSession.contactUser)
		publicID = normalizeFragmentIdentity(s.regSession.publicID)
	}
	s.mu.RUnlock()
	for _, registration := range document.Registrations {
		if publicID != "" && normalizeFragmentIdentity(registration.AOR) != publicID {
			continue
		}
		for _, contact := range registration.Contacts {
			matches := contactID == "" || contact.ID == contactID || strings.Contains(contact.URI, contactID)
			if matches && strings.EqualFold(strings.TrimSpace(contact.State), "terminated") {
				return true
			}
		}
	}
	return false
}

func (s *Service) reRegisterAfterDelay(delay time.Duration) {
	if s == nil {
		return
	}
	if delay < 0 {
		delay = 0
	}
	s.networkDone.Add(1)
	go func() {
		defer s.networkDone.Done()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			_ = s.TriggerFastReconnect()
		case <-s.stop:
		}
	}()
}
