// Package voice implements the VoWiFi voice plane: the call state machine,
// the agent that owns calls, the gateway that bridges the local client, and
// SDP negotiation (RFC 3261, RFC 4566, 3GPP TS 24.229).
//
// Reconstructed from the decompiled internal/vowifi/voice.
package voice

import (
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

// Agent owns the voice calls for one device. It serializes call work on an
// actor goroutine and publishes call events on the IMS event bus.
type Agent struct {
	mu       sync.RWMutex
	deviceID string
	ims      *imscore.Service
	bus      *imscore.EventBus
	actor    *callstate.Actor

	calls      map[string]*Call // keyed by call ID
	activeCall *Call
	notifier   func(events.Event)
	started    bool

	clientAdapter   interface{}
	eventDispatcher interface{ Dispatch(interface{}) }
}

// Call is one voice call (inbound or outbound).
type Call struct {
	mu        sync.RWMutex
	agent     *Agent
	state     callstate.State
	direction callstate.Direction

	callID       string
	clientCallID string
	peer         string
	callee       string

	startTime time.Time
	endTime   time.Time

	imsDialog *imscore.DialogHandle
	imsInvite *imscore.InviteHandle
	routeSet  []string
	rtpRelay  *media.RTPRelay
	sipDialog *voiceSIPDialog

	ackSent              bool
	inviteFinalSeen      bool
	inviteProvisional    bool
	localCancelSent      bool
	reliableProvisional  bool
	outboundCancelReason string
}

// Gateway bridges the local client (LAN side) to the IMS network. It owns
// the client-facing SIP endpoint and forwards requests/responses.
type Gateway struct {
	mu       sync.RWMutex
	agent    *Agent
	notifier func(events.Event)
	started  bool
}

// SDPInfo is a parsed SDP session description (RFC 4566).
type SDPInfo struct {
	Origin      string
	SessionName string
	Connection  string
	Media       []MediaInfo
}

// MediaInfo is one m= line in an SDP description.
type MediaInfo struct {
	Type       string // "audio" / "video"
	Port       int
	Protocol   string
	Formats    []int
	Codecs     []CodecInfo
	Connection string
}

// CodecInfo describes one codec (rtpmap).
type CodecInfo struct {
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
}

// AgentSnapshot is a point-in-time view of the agent.
type AgentSnapshot struct {
	DeviceID   string
	ActiveCall *CallSnapshot
	Calls      []*CallSnapshot
	Busy       bool
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
