package imsendpoint

import (
	"context"
	"net"

	"github.com/emiago/sipgo/sip"
)

// DialogEndpoint is the v1.5.5 in-dialog signaling surface.
type DialogEndpoint interface {
	CloseDialog(context.Context, string, DialogHandle) error
	SendDialogRequest(context.Context, string, DialogHandle, *sip.Request, DialogRequestOptions) (*sip.Response, error)
}

// ClientDialogEndpoint is the v1.5.5 client and dialog signaling contract.
type ClientDialogEndpoint interface {
	DialogEndpoint
	AnswerServerInvite(context.Context, string, ServerInviteHandle, ServerInviteAnswerOptions) (DialogHandle, error)
	CancelClientInvite(context.Context, string, InviteHandle, ClientInviteCancelOptions) error
	DeviceID() string
	IsRegistered() bool
	NextCSeq() uint32
	RejectServerInvite(context.Context, string, ServerInviteHandle, ServerInviteRejectOptions) error
	SendReliableProvisionalPRACK(context.Context, string, ReliableProvisionalOptions) error
	Snapshot() Snapshot
	StartClientInvite(context.Context, string, ClientInviteOptions) (*ClientInviteResult, error)
}

// Endpoint is the runtime-owned IMS service surface needed by voice lifecycle binding.
// The full call and dialog contract is restored with imscore and voice.
type Endpoint interface {
	ClientDialogEndpoint
	RespondInboundRequest(context.Context, string, InboundRequestHandle, InboundResponseOptions) error
	Subscribe(EventSubscription, func(Event)) func()
}

// PacketListener opens IMS-bound packet sockets through the active dataplane.
type PacketListener interface {
	ListenPacket(context.Context, string, net.Addr) (net.PacketConn, error)
}

// RuntimeSnapshotSource exposes the immutable IMS runtime snapshot.
type RuntimeSnapshotSource interface {
	Snapshot() Snapshot
}

// EventSubscription controls asynchronous IMS signaling event delivery.
type EventSubscription struct {
	Name      string
	QueueSize int
	Workers   int
	Match     func(Event) bool
}

// Event projects one inbound SIP request or response.
type Event struct {
	DeviceID             string
	Kind                 string
	Method               string
	CSeqMethod           string
	CallID               string
	StatusCode           int
	Session              *Session
	Request              *sip.Request
	Response             *sip.Response
	InboundRequest       InboundRequestHandle
	ServerInvite         ServerInviteHandle
	ResponseAcknowledged bool
}

// Session is the immutable registration state attached to signaling events.
type Session struct {
	SignalingConn net.Conn
	LocalIP       string
	LocalPortC    int
	RemoteIP      string
	RemotePortS   int
	TransportMode string
	ServiceRoute  string
	Path          string
	SecVerify     string
	SecMode       string
	RouteSet      []string
	IMPU          string
	IMPI          string
	Domain        string
	Realm         string
	MSISDN        string
	Registered    bool
}

// Snapshot is the public IMS endpoint state used by voice and runtime hosts.
type Snapshot struct {
	IMPU               string
	Realm              string
	ContactID          string
	ServiceRoute       string
	SecVerify          string
	EffectiveSecMode   string
	PAccessNetworkInfo string
	UserAgent          string
	LocalAddr          string
	LocalPortC         int
	LocalPortS         int
	RemotePortC        int
	RemotePortS        int
	LocalSpiC          uint32
	LocalSpiS          uint32
	RemoteSpiC         uint32
	RemoteSpiS         uint32
	Transport          string
	IMEI               string
	PubGRUU            string
	Voice              VoiceProfile
}

// VoiceProfile contains the carrier-specific voice request headers.
type VoiceProfile struct {
	SupportedHeader   string
	AllowHeader       string
	AcceptContact     string
	PPreferredService string
	AccessType        string
	ContactParamOrder []string
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
