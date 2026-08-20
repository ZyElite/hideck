package imscore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/smscodec"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
	"github.com/warthog618/sms/encoding/tpdu"
)

const smsProtocolTraceDeviceEnv = "VOHIVE_IMS_SMS_TRACE_DEVICE"

type inboundSMSProtocolTrace struct {
	callID, cseq, via, fromDomain, assertedDomain, contactDomain string
	bodyBytes                                                    int
	rpKind                                                       string
	rpType, rpMR, rpCause, causeIEBytes                          int
	causeDiagnostic                                              string
	rpUserDataBytes, tpFCS                                       int
	tpSubmitReport                                               bool
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
	trace := inboundSMSProtocolTrace{
		callID:         smsTraceToken(sipkit.FirstHeaderValue(request, "Call-ID", true)),
		cseq:           sipkit.FirstHeaderValue(request, "CSeq", true),
		via:            smsTraceToken(sipkit.FirstHeaderValue(request, "Via", true)),
		fromDomain:     smsTraceHeaderDomain(sipkit.FirstHeaderValue(request, "From", true)),
		assertedDomain: smsTraceHeaderDomain(sipkit.FirstHeaderValue(request, "P-Asserted-Identity", true)),
		contactDomain:  smsTraceHeaderDomain(sipkit.FirstHeaderValue(request, "Contact", true)),
		bodyBytes:      len(request.Body()),
	}
	rpdu, err := smscodec.DecodeBodyMaybeHex(request.Body())
	if err != nil {
		return trace, nil
	}
	info := smscodec.ClassifyRPDU(rpdu)
	trace.rpKind = string(info.Kind)
	trace.rpType, trace.rpMR, trace.rpCause = int(info.RawType), int(info.MR), info.Cause
	trace.causeIEBytes, trace.causeDiagnostic = rpErrorDiagnosticTrace(rpdu)
	trace.rpUserDataBytes, trace.tpSubmitReport, trace.tpFCS = rpErrorSubmitReportTrace(rpdu)
	return trace, nil
}

func rpErrorDiagnosticTrace(rpdu []byte) (int, string) {
	if len(rpdu) < 4 || (rpdu[0] != 0x04 && rpdu[0] != 0x05) {
		return 0, ""
	}
	details, err := smscodec.ParseRPErrorDetails(rpdu)
	if err != nil {
		return int(rpdu[2]), "invalid"
	}
	return int(rpdu[2]), hex.EncodeToString(details.Diagnostics)
}

func rpErrorSubmitReportTrace(rpdu []byte) (int, bool, int) {
	details, err := smscodec.ParseRPErrorDetails(rpdu)
	if err != nil || len(details.UserData) == 0 {
		return 0, false, 0
	}
	report := tpdu.TPDU{Direction: tpdu.MT}
	if err := report.UnmarshalBinary(details.UserData); err != nil || report.SmsType() != tpdu.SmsSubmitReport {
		return len(details.UserData), false, 0
	}
	return len(details.UserData), true, int(report.FCS)
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
		"body_bytes", trace.bodyBytes, "rp_kind", trace.rpKind,
		"rp_type", trace.rpType, "rp_mr", trace.rpMR, "rp_cause", trace.rpCause,
		"rp_cause_ie_bytes", trace.causeIEBytes, "rp_cause_diagnostic", trace.causeDiagnostic,
		"rp_user_data_bytes", trace.rpUserDataBytes,
		"tp_submit_report", trace.tpSubmitReport, "tp_fcs", trace.tpFCS,
		"parse_error", traceErrorText(err))
}

func (s *Service) logRegisterSMSCapabilityTrace(response *sipResponse) {
	if !s.smsProtocolTraceEnabled() || response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	contacts := response.HeaderValues("Contact")
	contactText := strings.ToLower(strings.Join(contacts, ","))
	logging.Debug("IMS SMS protocol trace: REGISTER capability",
		"device", s.DeviceID(), "status", response.StatusCode,
		"contacts", len(contacts), "contact_smsip", strings.Contains(contactText, "+g.3gpp.smsip"),
		"contact_icsi_sms", strings.Contains(contactText, "3gpp-service.ims.icsi.sms"),
		"service_route", strings.TrimSpace(response.Header("Service-Route")) != "",
		"associated_uri", strings.TrimSpace(response.Header("P-Associated-URI")) != "")
}

func (s *Service) logMOSMSProtocolTrace(request *sip.Request) {
	if !s.smsProtocolTraceEnabled() || request == nil {
		return
	}
	fields := []any{
		"device", s.DeviceID(), "call_id_hash", smsTraceToken(outboundRequestCallID(request)),
		"target_scheme", strings.ToLower(strings.TrimSpace(request.Recipient.Scheme)),
		"target_domain", strings.ToLower(strings.Trim(strings.TrimSpace(request.Recipient.Host), "[]")),
		"body_bytes", len(request.Body()),
	}
	rpMR, _, rpDestination, encodedTPDU, err := smscodec.ParseRPDataWithAddresses(request.Body())
	if err != nil {
		logging.Debug("IMS SMS protocol trace: MO MESSAGE", append(fields, "parse_error", traceErrorText(err))...)
		return
	}
	message := tpdu.TPDU{Direction: tpdu.MO}
	if err := message.UnmarshalBinary(encodedTPDU); err != nil {
		logging.Debug("IMS SMS protocol trace: MO MESSAGE", append(fields,
			"rp_mr", int(rpMR), "rp_destination_hash", smsTraceToken(rpDestination),
			"tpdu_bytes", len(encodedTPDU), "parse_error", traceErrorText(err))...)
		return
	}
	fields = append(fields,
		"rp_mr", int(rpMR), "rp_destination_hash", smsTraceToken(rpDestination),
		"tpdu_bytes", len(encodedTPDU), "tp_first_octet", int(message.FirstOctet),
		"tp_mr", int(message.MR), "tp_srr", message.FirstOctet.SRR(),
		"tp_da_digits", len(strings.TrimPrefix(message.DA.Number(), "+")),
		"tp_da_ton", int(message.DA.TypeOfNumber()), "tp_da_npi", int(message.DA.NumberingPlan()),
		"tp_pid", int(message.PID), "tp_dcs", int(message.DCS), "tp_ud_bytes", len(message.UD),
		"parse_error", "")
	logging.Debug("IMS SMS protocol trace: MO MESSAGE", fields...)
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
	status int,
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
		"rp_mr", int(report.RPMR), "sip_status", status,
		"transaction_ok", sendErr == nil, "transaction_error", traceErrorText(sendErr))
}

func traceErrorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
