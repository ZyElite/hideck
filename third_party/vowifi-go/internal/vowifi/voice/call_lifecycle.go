package voice

import (
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// Hangup ends the call: it transitions to Disconnected and emits the
// CallEnded event.
func (c *Call) Hangup() error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if c.agent == nil {
		return errors.New("voice: call has no agent")
	}
	return c.agent.Hangup(c.CallID())
}

// StartMedia starts the RTP relay for the call.
func (c *Call) StartMedia() error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	return relay.Start()
}

// StopMedia stops the RTP relay for the call.
func (c *Call) StopMedia() error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return nil
	}
	return relay.Stop()
}

// StartPCAP begins packet capture for the call.
func (c *Call) StartPCAP(f interface {
	Write([]byte) (int, error)
	Close() error
}) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	relay.StartPCAP(f)
	return nil
}

// StopPCAP stops packet capture for the call.
func (c *Call) StopPCAP() error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return nil
	}
	relay.StopPCAP()
	return nil
}

// StartOutboundNoAnswerTimer schedules the no-answer timeout.
func (c *Call) StartOutboundNoAnswerTimer(timeout time.Duration) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	go func() {
		time.Sleep(timeout)
		if c.GetState() == callstate.StateDialing || c.GetState() == callstate.StateAlerting {
			c.SetOutboundCancelReason("no_answer")
			_ = c.Transition(callstate.StateFailed)
			if c.agent != nil {
				c.agent.emitCallFailed(c, "no_answer")
				c.agent.finalizeActiveCall(c)
			}
		}
	}()
	return nil
}

// StopOutboundNoAnswerTimer is a no-op (the timer self-terminates).
func (c *Call) StopOutboundNoAnswerTimer() error {
	return nil
}

// StartSessionTimer schedules the RFC 4028 session refresh.
func (c *Call) StartSessionTimer(interval time.Duration) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if interval <= 0 {
		interval = 1800 * time.Second
	}
	go func() {
		time.Sleep(interval)
		if c.GetState() == callstate.StateConnected {
			// Session refresh would send a re-INVITE; the relay keeps
			// media alive, so this is a placeholder for the refresh.
		}
	}()
	return nil
}

// EnsureTimerStopped is a no-op (timers self-terminate).
func (c *Call) EnsureTimerStopped() error {
	return nil
}

// CloseDone is a no-op hook for call teardown completion.
func (c *Call) CloseDone() error {
	return nil
}
