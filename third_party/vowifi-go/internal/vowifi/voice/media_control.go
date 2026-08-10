package voice

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	defaultDTMFDuration = 200 * time.Millisecond
	mediaInactivity     = 30 * time.Second
)

// SendDTMF emits one negotiated RFC 4733 event on this call.
func (c *Call) SendDTMF(digit string) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	digit = strings.TrimSpace(digit)
	runes := []rune(strings.ToUpper(digit))
	if len(runes) != 1 {
		return errors.New("voice: DTMF must contain exactly one digit")
	}
	relay := c.RTPRelay()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	return relay.SendDTMF(runes[0], defaultDTMFDuration)
}

// SendDTMF locates the real call context by Call-ID and sends an RTP event.
func (a *Agent) SendDTMF(callID, digit string) error {
	if a == nil {
		return errors.New("voice: nil agent")
	}
	call := a.callByID(strings.TrimSpace(callID))
	if call == nil {
		return errors.New("voice: call not found")
	}
	return call.SendDTMF(digit)
}

// StartPCAP starts capture on the active call.
func (a *Agent) StartPCAP(target string) error {
	call := a.currentCall()
	if call == nil {
		return errors.New("voice: no active call")
	}
	return call.StartPCAP(target)
}

// StopPCAP stops capture on the active call.
func (a *Agent) StopPCAP() error {
	call := a.currentCall()
	if call == nil {
		return errors.New("voice: no active call")
	}
	return call.StopPCAP()
}

func (a *Agent) currentCall() *Call {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeCall
}

func (a *Agent) enableMediaMonitor(call *Call) {
	if a == nil || call == nil || call.RTPRelay() == nil {
		return
	}
	relay := call.RTPRelay()
	relay.EnableMonitor(mediaInactivity, func() { a.handleMediaTimeout(call) })
	relay.SetOneWayTimeoutHandler(func(direction string) {
		logging.WarnRate("voice-media-one-way:"+call.CallID()+":"+direction, mediaInactivity,
			"RTP one-way media timeout", "device", a.deviceID, "call_id", call.CallID(), "direction", direction)
		a.emitCallMediaUpdated(call)
	})
}

func (a *Agent) handleMediaTimeout(call *Call) {
	if a == nil || call == nil || a.currentCall() != call {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	if err := a.HangupContext(ctx, call.CallID()); err != nil && !call.IsTerminalState() {
		a.forceReleaseCall(call, errors.Join(errors.New("voice: RTP media timeout"), err))
	}
}

// SendDTMF forwards an event to the agent-owned real call.
func (g *Gateway) SendDTMF(callID, digit string) error {
	if g == nil || g.agent == nil {
		return errors.New("voice: no agent")
	}
	return g.agent.SendDTMF(callID, digit)
}

// StartPCAP accepts the original device/path pair or a per-agent path.
func (g *Gateway) StartPCAP(args ...string) error {
	if g == nil || g.agent == nil {
		return errors.New("voice: no agent")
	}
	if len(args) == 0 {
		return errors.New("voice: PCAP output is required")
	}
	return g.agent.StartPCAP(args[len(args)-1])
}

// StopPCAP accepts an optional original device identifier.
func (g *Gateway) StopPCAP(_ ...string) error {
	if g == nil || g.agent == nil {
		return errors.New("voice: no agent")
	}
	return g.agent.StopPCAP()
}
