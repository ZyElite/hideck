package imscore

// RequestID returns the inbound request ID.
func (h *imscoreInboundRequestHandle) RequestID() string {
	if h == nil {
		return ""
	}
	return h.callID
}

// Sanitize redacts sensitive header values from the response memo.
func (m *inboundRequestResponseMemo) Sanitize() {
	if m == nil || m.headers == nil {
		return
	}
	// Never leak the digest challenge in a memo.
	delete(m.headers, "WWW-Authenticate")
	delete(m.headers, "Proxy-Authenticate")
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
