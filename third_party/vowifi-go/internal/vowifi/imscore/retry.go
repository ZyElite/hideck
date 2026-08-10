package imscore

import (
	"errors"
	"fmt"
	"strings"
)

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
	code string
	err  error
}

// Error implements error.
func (e *outboundModeResolveError) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		return e.err.Error()
	}
	return e.code
}

// Unwrap returns the wrapped error, if any.
func (e *outboundModeResolveError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// newOutboundModeResolveError builds an outbound mode resolution error.
func newOutboundModeResolveError(code, format string, args ...interface{}) error {
	return &outboundModeResolveError{code: strings.TrimSpace(code), err: fmt.Errorf(format, args...)}
}

func outboundResolveErrorCode(err error) string {
	var resolveErr *outboundModeResolveError
	if !errors.As(err, &resolveErr) || resolveErr == nil {
		return ""
	}
	return strings.TrimSpace(resolveErr.code)
}
