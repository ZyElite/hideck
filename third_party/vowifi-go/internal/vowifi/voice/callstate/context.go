package callstate

import (
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

// DialogState retains the complete SIP transaction and dialog context owned
// by a call in v1.5.5.
type DialogState struct {
	IMSSession        *imsendpoint.Session
	CallerID          string
	CalleeID          string
	CallID            string
	FromTag           string
	ToTag             string
	OutboundIMSCallID string
	IMSCallID         string
	IMSFromTag        string
	IMSToTag          string
	IMSBranch         string
	IMSCSeq           int
	ACKSent           bool
	InviteProvisional bool
	LocalCancelSent   bool
	LocalCancelReason string
	InviteFinalSeen   bool
	ErrorACKSent      bool
	ClientFromTag     string
	ClientToTag       string
	ClientCallID      string
	ClientDest        string
	ClientLocalIP     string
	ClientTx          sip.ServerTransaction
	OriginalRequest   *sip.Request
	IMSResponseCh     chan *sip.Response
	IMSDialog         imsendpoint.DialogHandle
	IMSInviteHandle   imsendpoint.InviteHandle
	ServerInvite      imsendpoint.ServerInviteHandle
	IMSContact        string
	RouteSet          []string
	OutboundTxBranch  string
	OutboundCSeq      int
	ServerTx          sip.ServerTransaction
}

// MediaState retains negotiated SDP and the live media resources for a call.
type MediaState struct {
	IMSSDP          []byte
	ClientSDP       []byte
	RTPRelay        *media.RTPRelay
	MediaManager    *media.MediaSessionManager
	PreconditionMet bool
}

// Timers retains the RFC 4028 and reliable-provisional timer state.
type Timers struct {
	SessionExpires       int
	SessionTimer         *time.Timer
	SessionTimerMu       sync.Mutex
	PrackTimer           *time.Timer
	PrackTimerMu         sync.Mutex
	PrackTimerGeneration uint64
	RSeq                 uint32
}
