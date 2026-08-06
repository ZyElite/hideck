// Package messaging defines the SMS delivery and USSD result types shared
// between the IMS host and the vohive delivery store.
//
// Reconstructed from the decompiled engine/runtimehost/messaging.
package messaging

import (
	"context"
	"errors"
	"time"
)

// ErrDeliveryNotFound is returned when a delivery record does not exist.
var ErrDeliveryNotFound = errors.New("messaging: delivery not found")

// DeliveryStatus is the delivery status of an SMS.
type DeliveryStatus struct {
	MessageID  string
	IMSI       string
	DeviceID   string
	Peer       string
	Content    string
	PartsTotal int
	Acks       int
	State      string
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Parts      []DeliveryPartStatus
}

// DeliveryPartStatus is the status of one SMS part.
type DeliveryPartStatus struct {
	PartNo      int
	CallID      string
	InReplyTo   string
	RPMR        int
	State       string
	SIPCode     int
	RPCause     int
	RPCauseText string
	ErrorText   string
	SentAt      time.Time
	ReportAt    *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DeliveryPartMatch is the result of matching a delivery report to a part.
type DeliveryPartMatch struct {
	MessageID string
	PartNo    int
	State     string
	Matched   bool
}

// SendOptions carries optional SMS delivery parameters.
type SendOptions struct {
	SuppressSendTGSuccess bool
	Encoding              string
}

// SendOutcome is the result of an SMS send.
type SendOutcome struct {
	Ref           string
	Err           error
	MessageID     string
	PartsTotal    int
	DeliveryState string
}

// USSDResult is the result of a USSD operation.
type USSDResult struct {
	Code    string
	Message string
}

// WithSuppressSendTGSuccess returns a context carrying the suppress-success
// option for SMS sends.
func WithSuppressSendTGSuccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressSendTGSuccessKey{}, true)
}

type suppressSendTGSuccessKey struct{}

// SuppressSendTGSuccess reports whether the context suppresses the success
// notification.
func SuppressSendTGSuccess(ctx context.Context) bool {
	v, _ := ctx.Value(suppressSendTGSuccessKey{}).(bool)
	return v
}

// RPCauseText maps an RP cause code to its 3GPP TS 24.011 text.
func RPCauseText(rpCause int) string {
	switch rpCause {
	case 0:
		return "normal"
	case 1:
		return "unassigned number"
	case 8:
		return "operator determined barring"
	case 10:
		return "call barred"
	case 21:
		return "short message transfer rejected"
	case 29:
		return "facility rejected"
	case 38:
		return "network out of order"
	case 41:
		return "temporary failure"
	case 69:
		return "insufficient resources"
	case 95:
		return "semantically incorrect message"
	case 111:
		return "protocol error"
	default:
		return "unknown"
	}
}

// DeliveryStore persists SMS delivery state.
type DeliveryStore interface {
	CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error
	UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error
	MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode int, rpCause int, errText string, at time.Time) (DeliveryPartMatch, error)
	RecomputeSMSDelivery(messageID string, at time.Time) error
	UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error
	GetSMSDeliveryStatus(messageID string) (*DeliveryStatus, error)
}

// ServiceStatus is the IMS service registration status.
type ServiceStatus struct {
	Registered bool
	State      string
	RegState   string
}

// IsRegistered reports whether the service is registered.
func (s *ServiceStatus) IsRegistered() bool {
	return s != nil && s.Registered
}
