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
	if !c.IsConnected() {
		return errors.New("voice: call is not connected")
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
	call := a.activeCallByHandle(callID)
	if call == nil {
		return errors.New("voice: active call not found")
	}
	return call.SendDTMF(digit)
}

func (a *Agent) activeCallByHandle(callID string) *Call {
	if a == nil {
		return nil
	}
	callID = strings.TrimSpace(callID)
	a.mu.RLock()
	call := a.activeCall
	a.mu.RUnlock()
	if !callMatchesID(call, callID) {
		return nil
	}
	return call
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
	return call.StopPCAPCurrent()
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
	if g == nil {
		return errors.New("voice: nil gateway")
	}
	g.mu.RLock()
	agents := make([]*Agent, 0, len(g.agents))
	for _, agent := range g.agents {
		agents = append(agents, agent)
	}
	g.mu.RUnlock()
	for _, agent := range agents {
		if agent.activeCallByHandle(callID) != nil {
			return agent.SendDTMF(callID, digit)
		}
	}
	return errors.New("voice: call not found")
}

// StartPCAP starts capture for the active call on one device.
func (g *Gateway) StartPCAP(deviceID, target string) error {
	agent := g.GetAgent(deviceID)
	if agent == nil {
		return errors.New("voice: agent not found for device " + strings.TrimSpace(deviceID))
	}
	return agent.StartPCAP(target)
}

// StopPCAP stops capture for the active call on one device.
func (g *Gateway) StopPCAP(deviceID string) error {
	agent := g.GetAgent(deviceID)
	if agent == nil {
		return errors.New("voice: agent not found for device " + strings.TrimSpace(deviceID))
	}
	return agent.StopPCAP()
}
