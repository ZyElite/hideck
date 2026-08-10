package imscore

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo/sip"
)

type outboundMessageResult struct {
	DispatchSeq uint64
	SIPCode     int
}

type outboundMessageReply struct {
	result outboundMessageResult
	err    error
}

type outboundDispatchOptions struct {
	Context   context.Context
	Flow      string
	Request   *sip.Request
	Timeout   time.Duration
	Callbacks sipTransactionCallbacks
}

type outboundMessageTask struct {
	ctx       context.Context
	flow      string
	req       *sip.Request
	timeout   int64
	callbacks sipTransactionCallbacks
	done      chan outboundMessageReply
}

type outboundRequestReply struct {
	res         *sip.Response
	err         error
	dispatchSeq uint64
}

type outboundRequestTask struct {
	ctx         context.Context
	flow        string
	req         *sip.Request
	modeCtx     outboundModeContext
	timeout     int64
	callbacks   sipTransactionCallbacks
	dispatchSeq uint64
	enqueuedAt  time.Time
	done        chan outboundRequestReply
}

type smsPendingInfo struct {
	MessageID string
	PartNo    int
	PartTotal int
	CallID    string
	CallIDKey string
	RPMR      int
	To        string
	TargetURI string
	TextLen   int
	CSeq      uint32
	CreatedAt time.Time
	RespCh    chan smsSendResult
}

type smsSendResult struct {
	Code              int
	Status            string
	Reason            string
	Warning           string
	Server            string
	RetryAfter        string
	WWWAuthenticate   string
	ProxyAuthenticate string
	Body              []byte
	At                time.Time
}

type messagingRuntime struct {
	smsSendMu sync.Mutex

	outboundMu        sync.Mutex
	outboundMsgCh     chan outboundMessageTask
	outboundReqShards []chan outboundRequestTask

	smsPending     map[string]*smsPendingInfo
	smsPendingNorm map[string]*smsPendingInfo

	inboundSeenMu  sync.Mutex
	inboundSeen    map[string]time.Time
	inboundSeenRsp map[string]inboundRequestResponseMemo

	mtSMSSeenMu sync.Mutex
	mtSMSSeen   map[string]time.Time

	moSelfHealLastAt atomic.Int64
	lastMTAckMu      sync.Mutex
}

type observability struct {
	mtSMSDedupHit           atomic.Int64
	lastMTAckTraceID        string
	lastMTAckTarget         string
	lastMTAckDestination    string
	lastMTAckTransport      string
	lastMTAckCallID         string
	lastMTAckRPMR           int
	lastMTAckFingerprint    string
	lastMTAckErr            string
	lastMTAckAt             time.Time
	outboundDispatchSeq     atomic.Uint64
	mtAckSendOK             atomic.Int64
	mtAckSendErr            atomic.Int64
	mtAckSIPNon2xx          atomic.Int64
	moRPErrorCause28        atomic.Int64
	moRPErrorCause30        atomic.Int64
	moRPErrorCause38        atomic.Int64
	outboundQueueReject     atomic.Int64
	bypassSuppressedSMS     atomic.Int64
	inboundUDPSocketRead    atomic.Uint64
	inboundUDPSocketBytes   atomic.Uint64
	inboundTCPAccept        atomic.Uint64
	inboundTCPSocketRead    atomic.Uint64
	inboundTCPSocketBytes   atomic.Uint64
	inboundSIPParsedMessage atomic.Uint64
	inboundSIPParsedRequest atomic.Uint64
	inboundSIPParsedResp    atomic.Uint64
	lastRegisterTraceID     string
	lastRegisterAttemptAt   time.Time
	lastRegisterOKAt        time.Time
	lastRegisterErr         string
	lastSMSSendTraceID      string
	lastSMSSendAt           time.Time
	lastSMSSendErr          string
}

type smsFragment struct {
	Ref           int
	RefBits       int
	Total         int
	Seq           int
	Content       string
	Time          time.Time
	RpMr          uint8
	CallID        string
	ToURI         string
	ServiceCenter string
	AckSent       bool
	AckSentAt     time.Time
	DegradedAt    time.Time
}

type fragmentAuditFailure struct {
	At            time.Time `json:"at"`
	Key           string    `json:"key"`
	Sender        string    `json:"sender"`
	Received      int       `json:"received"`
	Total         int       `json:"total"`
	MissingSeq    string    `json:"missing_seq"`
	SeqList       string    `json:"seq_list"`
	Reason        string    `json:"reason"`
	InterimKey    string    `json:"-"`
	InterimReason string    `json:"-"`
}

type completedSMSFragment struct {
	Content string
	RPMR    uint8
	Total   int
}

type completedSMSFragmentSession struct {
	At    time.Time
	Parts map[int]completedSMSFragment
}

type outboundSMSAudit struct {
	At      time.Time `json:"at"`
	TraceID string    `json:"trace_id"`
	CallID  string    `json:"call_id"`
	To      string    `json:"to"`
	Len     int       `json:"len"`
}

type fragmentState struct {
	fragmentCache          map[string][]*smsFragment
	fragmentMu             sync.Mutex
	fragmentPersistMu      sync.Mutex
	fragmentCleanupOnce    sync.Once
	fragmentArrivedTotal   int64
	fragmentAssembledOK    int64
	fragmentTimeoutDegrade int64
	fragmentOrphanLate     int64
	fragmentDup            int64
	fragmentRecentExpired  map[string]time.Time
	fragmentRecentComplete map[string]completedSMSFragmentSession
	fragmentAuditFailures  []fragmentAuditFailure
	outboundSMSAudits      []outboundSMSAudit
}
