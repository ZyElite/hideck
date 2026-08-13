// Package voice implements the VoWiFi voice plane: the call state machine,
// the agent that owns calls, the gateway that bridges the local client, and
// SDP negotiation (RFC 3261, RFC 4566, 3GPP TS 24.229).
//
// Reconstructed from the decompiled internal/vowifi/voice.
package voice

import (
	"context"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/dialog"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

// Agent owns the voice calls for one device. It serializes call work on an
// actor goroutine and publishes call events on the IMS event bus.
type Agent struct {
	mu       sync.RWMutex
	deviceID string
	ims      *imscore.Service
	endpoint imsendpoint.Endpoint
	bus      *imscore.EventBus
	gateway  *Gateway
	actor    *callstate.Actor
	dialog   *dialog.Controller

	calls           map[string]*Call // keyed by call ID
	activeCall      *Call
	notifierMu      sync.Mutex
	notifier        func(events.Event)
	incomingHandler func(IncomingCall)
	newMediaRelay   func(string) (*media.RTPRelay, error)
	started         bool
	ctx             context.Context
	cancel          context.CancelFunc

	clientAdapter   voiceclient.Adapter
	clientBridge    *client.Bridge
	eventDispatcher interface{ Dispatch(interface{}) }
	imsUnsubscribe  func()

	outboundCancelSettle time.Duration
}

// Call is one voice call (inbound or outbound).
type Call struct {
	DeviceID  string
	Direction int
	State     int
	TraceID   string
	Done      chan struct{}
	doneOnce  sync.Once
	Ctx       context.Context
	Cancel    func()

	outboundRuntimeCancel func()
	outboundNoAnswerStop  func()
	mu                    sync.RWMutex
	startTime             time.Time
	endTime               time.Time
	callstate.DialogState
	callstate.MediaState
	callstate.Timers
	actor *callstate.Actor

	agent *Agent

	callID       string
	clientCallID string
	peer         string
	callee       string

	noAnswerTimer *time.Timer
	prackTimer    *time.Timer

	prackGeneration uint64
	prackRetransmit func()
	prackDeadline   time.Time

	imsDialog         *imscore.DialogHandle
	imsInvite         *imscore.InviteHandle
	imsServerInvite   imsendpoint.ServerInviteHandle
	imsInviteRequest  *sip.Request
	routeSet          []string
	rtpRelay          *media.RTPRelay
	comfortNoise      *media.ComfortNoiseGenerator
	sipDialog         *voiceSIPDialog
	inboundResponder  imscore.InboundVoiceResponder
	remoteSDP         string
	clientRemoteSDP   string
	clientLocalSDP    string
	imsLocalSDP       string
	outboundInvite    string
	inboundDecisionMu sync.Mutex

	ackSent              bool
	inviteFinalSeen      bool
	inviteProvisional    bool
	localCancelSent      bool
	reliableProvisional  bool
	outboundCancelReason string
	clientInviteRequest  *sip.Request
	clientInviteResponse *sip.Response
	inboundClientStarted bool
	inboundPrepared      bool
	inboundClientBridge  *client.Bridge
	clientCancelSent     bool
	clientByeSent        bool
	clientFinalSent      bool
	terminalFinalized    bool
	captureBasePath      string
	cleanupOnce          sync.Once
	finalizedEventOnce   sync.Once
	cleanupErr           error
}

// Gateway bridges the local client (LAN side) to the IMS network. It owns
// the client-facing SIP endpoint and forwards requests/responses.
type Gateway struct {
	notifier        CallNotifier
	eventDispatcher events.EventDispatcher
	clientAdapter   voiceclient.Adapter
	mu              sync.RWMutex
	agents          map[string]*Agent
	entryWorkers    map[string]*gatewayEntryWorker
	running         bool
	epoch           uint64
	ctx             context.Context
	cancel          context.CancelFunc
	incomingSeen    map[string]struct{}
}

// CallNotifier is the v1.5.5 incoming-call notification contract.
type CallNotifier interface {
	NotifyIncomingCall(deviceID, caller, callee string)
}

type gatewayEntryTask struct {
	name       string
	enqueuedAt time.Time
	fn         func(*Agent)
}

type gatewayEntryWorker struct {
	deviceID string
	agent    *Agent
	ch       chan gatewayEntryTask
	cancel   context.CancelFunc
	done     chan struct{}
	previous *gatewayEntryWorker
}

// SDPInfo is the recovered v1.5.5 audio-session projection.
type SDPInfo struct {
	ConnectionIP string
	MediaPort    int
	MediaType    string
	Codecs       []CodecInfo
	RawSDP       []byte
}

// CodecInfo is one recovered RTP codec declaration.
type CodecInfo struct {
	PayloadType int
	Name        string
	ClockRate   int
	Channels    int
	Fmtp        string
}

// SDPInfoCurrent retains the displaced multi-section parser projection.
type SDPInfoCurrent struct {
	Origin      string
	SessionName string
	Connection  string
	Media       []MediaInfo
}

// MediaInfo is one additive m= line projection.
type MediaInfo struct {
	Type       string // "audio" / "video"
	Port       int
	Protocol   string
	Formats    []int
	Codecs     []CodecInfoCurrent
	Connection string
}

// CodecInfoCurrent retains the displaced codec projection.
type CodecInfoCurrent struct {
	PayloadType int
	Encoding    string
	ClockRate   int
	Channels    int
	Fmtp        string
}

// firePool runs fire-and-forget goroutines with a bounded pool.
type firePool struct {
	mu      sync.Mutex
	sem     chan struct{}
	done    chan struct{}
	started bool
}

// CallSnapshot is a point-in-time view of a call.
type CallSnapshot struct {
	CallID    string
	State     string
	Direction string
	Peer      string
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
	ClientSDP string
}

// AgentSnapshot is a point-in-time view of the agent.
type AgentSnapshot struct {
	DeviceID   string
	ActiveCall *CallSnapshot
	Calls      []*CallSnapshot
	Busy       bool
}

// IncomingCall is the business-facing view of a pending IMS call. OfferSDP
// points at the client side of the allocated RTP relay.
type IncomingCall struct {
	DeviceID   string
	CallID     string
	Caller     string
	Callee     string
	OfferSDP   string
	ReceivedAt time.Time
	State      string
}

// InboundAnswer describes the established inbound call.
type InboundAnswer struct {
	CallID   string
	OfferSDP string
	State    string
}

// SimulateCallRequest is the recovered v1.5.5 timed-call request.
type SimulateCallRequest struct {
	Callee          string `json:"callee"`
	HoldSeconds     int    `json:"hold_seconds,omitempty"`
	OnConnected     func() `json:"-" binding:"-"`
	CaptureBasePath string `json:"-" binding:"-"`
}

// SimulateCallResult is the recovered v1.5.5 timed-call outcome.
type SimulateCallResult struct {
	Success    bool   `json:"success"`
	DurationMs int64  `json:"duration_ms"`
	Reason     string `json:"reason"`
	PCAPPath   string `json:"pcap_path,omitempty"`
	AudioPath  string `json:"audio_path,omitempty"`
	AudioCodec string `json:"audio_codec,omitempty"`
}

type simulateInviteRuntimeResult struct {
	response *sip.Response
	err      error
}

// Go runs fn in the fire pool with a bounded concurrency semaphore.
func (p *firePool) Go(fn func()) {
	if p == nil || fn == nil {
		return
	}
	p.mu.Lock()
	if p.sem == nil {
		p.sem = make(chan struct{}, 16)
	}
	p.mu.Unlock()
	p.sem <- struct{}{}
	go func() {
		defer func() { <-p.sem }()
		fn()
	}()
}
