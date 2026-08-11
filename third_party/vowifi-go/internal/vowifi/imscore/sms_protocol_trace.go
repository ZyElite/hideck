package imscore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

const smsProtocolTraceDeviceEnv = "VOHIVE_IMS_SMS_TRACE_DEVICE"

type inboundSMSProtocolTrace struct {
	callID, cseq, via, fromDomain, assertedDomain, contactDomain string
	bodyBytes                                                    int
}

func (s *Service) smsProtocolTraceEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	target := strings.TrimSpace(os.Getenv(smsProtocolTraceDeviceEnv))
	return target == "*" || target == strings.TrimSpace(s.cfg.DeviceID)
}

func smsTraceToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func smsTraceHeaderDomain(value string) string {
	value = firstSIPHeaderURI(value)
	if value == "" {
		return ""
	}
	var uri sip.Uri
	if err := sip.ParseUri(value, &uri); err != nil {
		return ""
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(uri.Host), "[]"))
}

func parseInboundSMSProtocolTrace(raw string) (inboundSMSProtocolTrace, error) {
	message, err := parseSIPMessage(raw)
	if err != nil {
		return inboundSMSProtocolTrace{}, err
	}
	request, ok := message.(*sip.Request)
	if !ok {
		return inboundSMSProtocolTrace{}, errExpectedSIPResponse
	}
	return inboundSMSProtocolTrace{
		callID:         smsTraceToken(sipkit.FirstHeaderValue(request, "Call-ID", true)),
		cseq:           sipkit.FirstHeaderValue(request, "CSeq", true),
		via:            smsTraceToken(sipkit.FirstHeaderValue(request, "Via", true)),
		fromDomain:     smsTraceHeaderDomain(sipkit.FirstHeaderValue(request, "From", true)),
		assertedDomain: smsTraceHeaderDomain(sipkit.FirstHeaderValue(request, "P-Asserted-Identity", true)),
		contactDomain:  smsTraceHeaderDomain(sipkit.FirstHeaderValue(request, "Contact", true)),
		bodyBytes:      len(request.Body()),
	}, nil
}

func (s *Service) logInboundSMSProtocolTrace(raw string) {
	if !s.smsProtocolTraceEnabled() {
		return
	}
	trace, err := parseInboundSMSProtocolTrace(raw)
	logging.Debug("IMS SMS protocol trace: inbound MESSAGE",
		"device", s.DeviceID(), "call_id_hash", trace.callID, "cseq", trace.cseq,
		"via_hash", trace.via, "from_domain", trace.fromDomain,
		"asserted_domain", trace.assertedDomain, "contact_domain", trace.contactDomain,
		"body_bytes", trace.bodyBytes, "parse_error", traceErrorText(err))
}

func (s *Service) logInboundSMSResponseTrace(raw, response string, writeErr error) {
	if !s.smsProtocolTraceEnabled() {
		return
	}
	requestTrace, requestErr := parseInboundSMSProtocolTrace(raw)
	parsedResponse, responseErr := parseSIPResponse(response)
	status, responseCallID, responseCSeq := responseTraceFields(parsedResponse)
	logging.Debug("IMS SMS protocol trace: inbound response write",
		"device", s.DeviceID(), "request_call_id_hash", requestTrace.callID,
		"request_cseq", requestTrace.cseq, "status", status,
		"response_call_id_hash", responseCallID, "response_cseq", responseCSeq,
		"response_bytes", len(response), "write_ok", writeErr == nil,
		"write_error", traceErrorText(writeErr), "request_parse_error", traceErrorText(requestErr),
		"response_parse_error", traceErrorText(responseErr))
}

func responseTraceFields(response *sipResponse) (int, string, string) {
	if response == nil {
		return 0, "", ""
	}
	return response.StatusCode, smsTraceToken(response.CallID), response.CSeq
}

func (s *Service) logRPReportProtocolTrace(
	request *sip.Request,
	modeCtx outboundModeContext,
	report rpReportRequest,
	sendErr error,
) {
	if !s.smsProtocolTraceEnabled() || request == nil {
		return
	}
	logging.Debug("IMS SMS protocol trace: RP report write",
		"device", s.DeviceID(), "call_id_hash", smsTraceToken(request.CallID().Value()),
		"cseq", sipkit.FirstHeaderValue(request, "CSeq", true),
		"in_reply_to_hash", smsTraceToken(sipkit.FirstHeaderValue(request, "In-Reply-To", true)),
		"inbound_call_id_hash", smsTraceToken(rawSIPHeaderValue(report.Inbound, "Call-ID")),
		"target_domain", strings.ToLower(strings.Trim(strings.TrimSpace(request.Recipient.Host), "[]")),
		"destination_hash", smsTraceToken(destinationFromContext(modeCtx)),
		"transport", strings.ToLower(strings.TrimSpace(modeCtx.Transport)),
		"rp_mr", int(report.RPMR), "write_ok", sendErr == nil,
		"write_error", traceErrorText(sendErr))
}

func traceErrorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
