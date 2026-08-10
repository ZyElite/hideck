package ussi

import (
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

func logUSSISIPRaw(deviceID, phase, direction string, message sip.Message) {
	if message == nil {
		return
	}
	raw := strings.TrimSpace(message.String())
	if raw == "" {
		return
	}
	method, callID := ussiSIPRawLogMethodAndCallID(message)
	logging.RunDebug("IMS USSD SIP 原文",
		"device", strings.TrimSpace(deviceID),
		"phase", strings.TrimSpace(phase),
		"direction", strings.TrimSpace(direction),
		"method", method,
		"call_id", callID,
		"bytes", len(raw),
		"raw", logging.RedactSIPRaw(raw),
	)
}

func ussiSIPRawLogMethodAndCallID(message sip.Message) (string, string) {
	if message == nil {
		return "", ""
	}
	method := ""
	switch value := message.(type) {
	case *sip.Request:
		method = strings.TrimSpace(value.Method.String())
	case *sip.Response:
		if cseq := value.CSeq(); cseq != nil {
			method = strings.TrimSpace(cseq.MethodName.String())
		}
	}
	callID := ""
	if header := message.CallID(); header != nil {
		callID = strings.TrimSpace(header.Value())
	}
	return method, callID
}

func (s *Service) logInboundMismatch(phase, reason string, request *sip.Request) {
	method := ""
	if request != nil {
		method = strings.TrimSpace(request.Method.String())
	}
	deviceID := ""
	if s != nil {
		deviceID = strings.TrimSpace(s.deviceID)
	}
	logging.RunDebug("IMS USSD 入站未匹配",
		"device", deviceID,
		"phase", strings.TrimSpace(phase),
		"reason", strings.TrimSpace(reason),
		"method", method,
		"call_id", requestHeader(request, "Call-ID"),
		"content_type", requestHeader(request, "Content-Type"),
	)
}
