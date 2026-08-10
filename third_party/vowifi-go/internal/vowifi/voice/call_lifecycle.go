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
	generator := c.comfortNoise
	c.mu.RUnlock()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	if err := relay.Start(); err != nil {
		return err
	}
	if generator == nil {
		return nil
	}
	conn, remote := relay.GetIMSConnAndRemote()
	if err := generator.Start(conn, remote); err != nil {
		return errors.Join(err, relay.Stop())
	}
	return nil
}

// StopMedia stops the RTP relay for the call.
func (c *Call) StopMedia() error {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	relay := c.rtpRelay
	generator := c.comfortNoise
	c.mu.RUnlock()
	if generator != nil {
		generator.Stop()
	}
	if relay == nil {
		return nil
	}
	return relay.Stop()
}

// StartPCAP begins packet capture to a path/directory or an injected writer.
func (c *Call) StartPCAP(target any) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	return relay.StartPCAP(target)
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
	return relay.StopPCAP()
}

// StartOutboundNoAnswerTimer schedules the no-answer timeout.
func (c *Call) StartOutboundNoAnswerTimer(timeout time.Duration) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	c.mu.Lock()
	if c.noAnswerTimer != nil {
		c.noAnswerTimer.Stop()
	}
	c.noAnswerTimer = time.AfterFunc(timeout, func() {
		if c.CallState() == callstate.StateDialing || c.CallState() == callstate.StateAlerting {
			if c.agent != nil {
				c.agent.handleOutboundInviteNoAnswerTimeout(c)
				return
			}
			c.SetOutboundCancelReason("no_answer")
			_ = c.TransitionChecked(callstate.StateFailed)
			c.CloseDone()
		}
	})
	c.mu.Unlock()
	return nil
}

// StopOutboundNoAnswerTimer cancels the pending no-answer timer.
func (c *Call) StopOutboundNoAnswerTimer() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.noAnswerTimer != nil {
		c.noAnswerTimer.Stop()
		c.noAnswerTimer = nil
	}
	c.mu.Unlock()
	return nil
}

// EnsureTimerStopped cancels every call-owned timer.
func (c *Call) EnsureTimerStopped() error {
	c.StopPrackTimer()
	return errors.Join(c.StopOutboundNoAnswerTimer(), c.stopSessionTimer())
}

func (c *Call) stopSessionTimer() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.sessionTimer != nil {
		c.sessionTimer.Stop()
		c.sessionTimer = nil
	}
	c.mu.Unlock()
	return nil
}

// CloseDone retains the recovered idempotent completion signal.
func (c *Call) CloseDone() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.Done == nil {
		c.Done = make(chan struct{})
	}
	done := c.Done
	c.mu.Unlock()
	c.doneOnce.Do(func() { close(done) })
	return
}

// CloseDoneChecked preserves the additive error-returning helper.
func (c *Call) CloseDoneChecked() error {
	c.CloseDone()
	return nil
}
