package imsendpoint

import (
	"context"
	"time"

	"github.com/emiago/sipgo/sip"
)

// Endpoint is the runtime-owned IMS service surface needed by voice lifecycle binding.
// The full call and dialog contract is restored with imscore and voice.
type Endpoint interface {
	DeviceID() string
	IsRegistered() bool
	StartClientInvite(context.Context, ClientInviteOptions) (*ClientInviteResult, error)
	CancelClientInvite(context.Context, InviteHandle, ClientInviteCancelOptions) error
	RespondInboundRequest(context.Context, InboundRequestHandle, InboundResponseOptions) error
	AnswerServerInvite(context.Context, ServerInviteHandle, ServerInviteAnswerOptions) (DialogHandle, error)
	RejectServerInvite(context.Context, ServerInviteHandle, ServerInviteRejectOptions) error
}

// InviteHandle identifies a client INVITE transaction.
type InviteHandle interface {
	InviteID() string
}

// DialogHandle identifies an established SIP dialog.
type DialogHandle interface {
	DialogID() string
}

// InboundRequestHandle retains a received non-INVITE server transaction.
type InboundRequestHandle interface {
	RequestID() string
}

// ServerInviteHandle retains a received INVITE server transaction.
type ServerInviteHandle interface {
	InviteID() string
}

// ClientInviteOptions controls one client INVITE transaction.
type ClientInviteOptions struct {
	Request    *sip.Request
	Contact    *sip.ContactHeader
	Timeout    time.Duration
	OnStarted  func(InviteHandle) error
	OnResponse func(*sip.Response) error
}

// ClientInviteResult retains transaction context even when the INVITE fails.
type ClientInviteResult struct {
	InviteHandle InviteHandle
	Dialog       DialogHandle
	Response     *sip.Response
}

// ClientInviteCancelOptions controls the related CANCEL request.
type ClientInviteCancelOptions struct {
	Reason string
}

// InboundResponseOptions describes a response to an inbound request.
type InboundResponseOptions struct {
	Code    int
	Reason  string
	Body    []byte
	Headers []sip.Header
}

// ServerInviteAnswerOptions accepts an inbound INVITE.
type ServerInviteAnswerOptions struct {
	Response *sip.Response
	Contact  *sip.ContactHeader
}

// ServerInviteRejectOptions rejects an inbound INVITE.
type ServerInviteRejectOptions struct {
	Response *sip.Response
	Code     int
	Reason   string
	Body     []byte
	Header   []sip.Header
}
