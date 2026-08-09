package imsendpoint

import (
	"context"

	"github.com/emiago/sipgo/sip"
)

// DialogEndpoint is the v1.5.5 in-dialog signaling surface.
type DialogEndpoint interface {
	CloseDialog(context.Context, string, DialogHandle) error
	SendDialogRequest(context.Context, string, DialogHandle, *sip.Request, DialogRequestOptions) (*sip.Response, error)
}

// Endpoint is the runtime-owned IMS service surface needed by voice lifecycle binding.
// The full call and dialog contract is restored with imscore and voice.
type Endpoint interface {
	DialogEndpoint
	DeviceID() string
	IsRegistered() bool
	NextCSeq() uint32
	SendReliableProvisionalPRACK(context.Context, string, ReliableProvisionalOptions) error
	StartClientInvite(context.Context, string, ClientInviteOptions) (*ClientInviteResult, error)
	CancelClientInvite(context.Context, string, InviteHandle, ClientInviteCancelOptions) error
	RespondInboundRequest(context.Context, string, InboundRequestHandle, InboundResponseOptions) error
	AnswerServerInvite(context.Context, string, ServerInviteHandle, ServerInviteAnswerOptions) (DialogHandle, error)
	RejectServerInvite(context.Context, string, ServerInviteHandle, ServerInviteRejectOptions) error
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
	Timeout    int64
	OnStarted  func(InviteHandle) error
	OnResponse func(*sip.Response) error
}

// DialogRequestOptions controls one in-dialog transaction.
type DialogRequestOptions struct {
	Timeout int64
}

// RetryPolicy controls reliable-provisional retry timing.
type RetryPolicy struct {
	Initial int64
	Max     int64
	Count   int
}

// ReliableProvisionalOptions contains the context needed to construct PRACK.
type ReliableProvisionalOptions struct {
	Invite       InviteHandle
	Dialog       DialogHandle
	RSeq         string
	RAck         string
	Contact      string
	RecordRoutes []string
	Retry        RetryPolicy
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
