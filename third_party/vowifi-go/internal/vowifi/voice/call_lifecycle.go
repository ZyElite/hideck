package voice

import (
	"context"
	"errors"
	"fmt"
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
	c.mu.Lock()
	if c.noAnswerTimer != nil {
		c.noAnswerTimer.Stop()
	}
	c.noAnswerTimer = time.AfterFunc(timeout, func() {
		if c.GetState() == callstate.StateDialing || c.GetState() == callstate.StateAlerting {
			cause := errors.New("voice: no answer")
			if c.agent != nil {
				if err := c.agent.sendIMSDialogRequest(BuildIMSCancel(c.agent, c)); err != nil {
					cause = errors.Join(cause, fmt.Errorf("send CANCEL: %w", err))
				}
				c.SetOutboundCancelReason(cause.Error())
				_ = c.agent.failOutboundCall(c, cause)
				return
			}
			c.SetOutboundCancelReason(cause.Error())
			_ = c.Transition(callstate.StateFailed)
			_ = c.CloseDone()
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

// StartSessionTimer schedules the RFC 4028 session refresh.
func (c *Call) StartSessionTimer(interval time.Duration) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if c.agent == nil || c.agent.ims == nil {
		return errors.New("voice: session timer has no IMS agent")
	}
	if interval <= 0 {
		interval = 1800 * time.Second
	}
	c.mu.Lock()
	if c.sessionTimer != nil {
		c.sessionTimer.Stop()
	}
	c.sessionTimer = time.AfterFunc(interval, func() {
		if c.GetState() == callstate.StateConnected {
			ctx, cancel := context.WithTimeout(context.Background(), voiceInviteTimeout)
			err := c.agent.refreshVoiceSession(ctx, c)
			cancel()
			if err != nil {
				_ = c.agent.failOutboundCall(c, err)
				return
			}
			_ = c.StartSessionTimer(interval)
		}
	})
	c.mu.Unlock()
	return nil
}

// EnsureTimerStopped cancels every call-owned timer.
func (c *Call) EnsureTimerStopped() error {
	return errors.Join(c.StopOutboundNoAnswerTimer(), c.StopPrackTimer(), c.stopSessionTimer())
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

// CloseDone signals call teardown completion once.
func (c *Call) CloseDone() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.done == nil {
		c.done = make(chan struct{})
	}
	done := c.done
	c.mu.Unlock()
	c.doneOnce.Do(func() { close(done) })
	return nil
}
