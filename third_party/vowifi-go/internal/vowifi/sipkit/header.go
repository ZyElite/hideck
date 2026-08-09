package sipkit

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

// FirstHeaderValue returns the first matching header value.
func FirstHeaderValue(message sip.Message, name string, trim bool) string {
	if message == nil {
		return ""
	}
	headers := message.GetHeaders(name)
	if len(headers) == 0 {
		return ""
	}
	return HeaderValue(headers[0], trim)
}

// HeaderValue returns a header value with optional surrounding-space removal.
func HeaderValue(header sip.Header, trim bool) string {
	if header == nil {
		return ""
	}
	value := header.Value()
	if trim {
		return strings.TrimSpace(value)
	}
	return value
}
