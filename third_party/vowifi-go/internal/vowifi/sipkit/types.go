package sipkit

import "github.com/emiago/sipgo/sip"

// RequestKind identifies the transaction context used to select IMS headers.
type RequestKind string

const (
	RequestKindRegister    RequestKind = "register"
	RequestKindOutOfDialog RequestKind = "out_of_dialog"
	RequestKindInDialog    RequestKind = "in_dialog"
)

// IMSRuntimeSnapshot is the immutable registration state consumed while a SIP
// request is built.
type IMSRuntimeSnapshot struct {
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
}

// DialogRequestOptions contains the headers owned by an in-dialog request.
type DialogRequestOptions struct {
	PAccessNetworkInfo string
	ForcePANI          bool
	PreferredIdentity  string
	SecurityVerify     string
	Protected          bool
	UserAgent          string
	ContentType        string
	Body               []byte
	Headers            []sip.Header
}

// IMSRequestOptions contains the complete transaction and IMS header context.
type IMSRequestOptions struct {
	Destination string
	Transport   string
	ViaHost     string
	ViaPort     int
	Branch      string

	FromURI sip.Uri
	FromTag string
	ToURI   sip.Uri
	ToTag   string
	CallID  string
	CSeq    uint32
	Routes  []string
	Contact *sip.ContactHeader
	Body    []byte

	Runtime      IMSRuntimeSnapshot
	Kind         RequestKind
	SecurityMode string

	RequireRoute        bool
	AddRPort            bool
	AddAlias            bool
	OmitURITransport    bool
	AddPreferredService bool
	AddAcceptContact    bool
	AddUserAgent        bool
	AddSupported        bool
	AddAllow            bool

	PreferredService   string
	AcceptContact      string
	Supported          string
	Allow              string
	PAccessNetworkInfo string
	PreferredIdentity  string
	SecurityClient     string
	SecurityVerify     string
	UserAgent          string
	ContentType        string
	Headers            []sip.Header
	MaxForwards        int
}
