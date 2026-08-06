package imscore

import (
	"errors"
	"time"
)

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

// registerRetryPolicy drives registration retry backoff (RFC 3261 §10.2).
type registerRetryPolicy struct {
	attempt int
}

// ShouldRetryDefaultInitial reports whether the first registration attempt
// should retry after a transient failure.
func (p *registerRetryPolicy) ShouldRetryDefaultInitial() bool {
	if p == nil {
		return true
	}
	return p.attempt < 3
}

// nextDelay returns the backoff delay for the current attempt.
func (p *registerRetryPolicy) nextDelay() time.Duration {
	if p == nil {
		return 5 * time.Second
	}
	switch p.attempt {
	case 0:
		return time.Second
	case 1:
		return 5 * time.Second
	default:
		return 15 * time.Second
	}
}

// noopDeliveryStore is a delivery store that discards all writes.
type noopDeliveryStore struct{}

// CreateSMSDelivery discards the write.
func (noopDeliveryStore) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error {
	return nil
}

// UpsertSMSDeliveryPart discards the write.
func (noopDeliveryStore) UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error {
	return nil
}

// MarkSMSDeliveryPartReport reports no match.
func (noopDeliveryStore) MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode int, rpCause int, errText string, at time.Time) (DeliveryPartMatch, error) {
	return DeliveryPartMatch{MessageID: inReplyTo, PartNo: rpMR, State: state, Matched: false}, nil
}

// RecomputeSMSDelivery is a no-op.
func (noopDeliveryStore) RecomputeSMSDelivery(messageID string, at time.Time) error {
	return nil
}

// UpdateSMSDeliveryState is a no-op.
func (noopDeliveryStore) UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error {
	return nil
}

// GetSMSDeliveryStatus reports that no delivery exists.
func (noopDeliveryStore) GetSMSDeliveryStatus(messageID string) (*DeliveryStatus, error) {
	return nil, errors.New("imscore: no delivery record")
}
