package imscore

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

func buildInboundResponseFromRequest(
	request *sip.Request,
	code int,
	reason string,
	body []byte,
	headers []sip.Header,
) *sip.Response {
	if code < 1 {
		code = 200
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = SIPStatusText(code)
	}
	if reason == "" {
		reason = "OK"
	}
	response := sip.NewResponseFromRequest(request, code, reason, append([]byte(nil), body...))
	if to := response.To(); to != nil {
		if _, exists := to.Params.Get("tag"); !exists {
			to.Params.Add("tag", sip.GenerateTagN(10))
		}
	}
	for _, header := range headers {
		if header != nil {
			response.AppendHeader(sip.HeaderClone(header))
		}
	}
	return response
}
