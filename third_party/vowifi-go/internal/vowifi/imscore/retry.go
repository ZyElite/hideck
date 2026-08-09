package imscore

import "strings"

// RequestID returns the inbound request ID.
func (h *imscoreInboundRequestHandle) RequestID() string {
	if h == nil {
		return ""
	}
	return h.id
}

// Sanitize normalizes a memo before it is replayed.
func (m *inboundRequestResponseMemo) Sanitize() (int, string) {
	code := m.Code
	if code < 1 {
		code = 200
	}
	reason := strings.TrimSpace(m.Reason)
	if reason == "" {
		reason = "OK"
	}
	return code, reason
}

// outboundModeResolveError is returned when the outbound mode cannot be
// resolved for a registration.
type outboundModeResolveError struct {
	reason string
}

// Error implements error.
func (e *outboundModeResolveError) Error() string {
	if e == nil {
		return "imscore: cannot resolve outbound mode"
	}
	return "imscore: cannot resolve outbound mode: " + e.reason
}

// Unwrap returns the wrapped error, if any.
func (e *outboundModeResolveError) Unwrap() error {
	return nil
}

// newOutboundModeResolveError builds an outbound mode resolution error.
func newOutboundModeResolveError(reason string) error {
	return &outboundModeResolveError{reason: reason}
}
