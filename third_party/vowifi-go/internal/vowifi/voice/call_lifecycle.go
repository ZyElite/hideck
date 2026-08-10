package voice

import (
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

// Hangup preserves the recovered local resource-release method.
func (c *Call) Hangup() {
	if c == nil {
		return
	}
	if c.Cancel != nil {
		c.Cancel()
	}
	c.StopMedia()
	c.EnsureTimerStopped()
	c.StopPrackTimer()
}

// HangupCurrent retains the additive network-aware error API.
func (c *Call) HangupCurrent() error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if c.agent == nil {
		return errors.New("voice: call has no agent")
	}
	return c.agent.HangupCurrent(c.CallID())
}

// StartMedia records media establishment and enters the recovered Connected state.
func (c *Call) StartMedia() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.startTime = time.Now()
	c.transitionLocked(int(callstate.StateConnected))
	c.mu.Unlock()
}

// StartMediaCurrent starts the restored media resources and exposes failures.
func (c *Call) StartMediaCurrent() error {
	if err := c.startMediaResourcesCurrent(); err != nil {
		return err
	}
	c.StartMedia()
	return nil
}

func (c *Call) startMediaResourcesCurrent() error {
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
	if err := relay.StartCurrent(); err != nil {
		return err
	}
	if generator == nil {
		return nil
	}
	conn, remote := relay.GetIMSConnAndRemote()
	if err := generator.Start(conn, remote); err != nil {
		return errors.Join(err, relay.StopCurrent())
	}
	return nil
}

// StopMedia records termination and stops all media resources.
func (c *Call) StopMedia() {
	_ = c.stopMediaCurrent()
}

// StopMediaCurrent retains explicit cleanup error propagation.
func (c *Call) StopMediaCurrent() error {
	return c.stopMediaCurrent()
}

func (c *Call) stopMediaCurrent() error {
	if c == nil {
		return nil
	}
	c.StopPrackTimer()
	c.mu.Lock()
	c.endTime = time.Now()
	c.transitionLocked(int(callstate.StateTerminated))
	relay := c.rtpRelay
	generator := c.comfortNoise
	if generator != nil {
		generator.Stop()
	}
	if relay == nil {
		c.mu.Unlock()
		return nil
	}
	err := relay.StopCurrent()
	c.mu.Unlock()
	return err
}

// StartPCAP preserves the original directory-based capture API.
func (c *Call) StartPCAP(outputDir string) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	logging.Debug("开启媒体抓包", "call_id", c.CallID(), "output_dir", outputDir)
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	return relay.StartPCAP(outputDir)
}

// StartPCAPCurrent retains the additive path and injected-writer forms.
func (c *Call) StartPCAPCurrent(target any) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return errors.New("voice: no media relay")
	}
	return relay.StartPCAPCurrent(target)
}

// StopPCAP preserves the original void cleanup API.
func (c *Call) StopPCAP() {
	_ = c.StopPCAPCurrent()
}

// StopPCAPCurrent stops capture and exposes retained writer failures.
func (c *Call) StopPCAPCurrent() error {
	if c == nil {
		return nil
	}
	logging.Debug("结束媒体抓包", "call_id", c.CallID())
	c.mu.RLock()
	relay := c.rtpRelay
	c.mu.RUnlock()
	if relay == nil {
		return nil
	}
	return relay.StopPCAPCurrent()
}

// StartOutboundNoAnswerTimer preserves the recovered void timer API.
func (c *Call) StartOutboundNoAnswerTimer(timeout time.Duration) {
	_ = c.StartOutboundNoAnswerTimerCurrent(timeout)
}

// StartOutboundNoAnswerTimerCurrent retains explicit nil-call validation.
func (c *Call) StartOutboundNoAnswerTimerCurrent(timeout time.Duration) error {
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
	timer := time.AfterFunc(timeout, func() {
		if c.CallState() == callstate.StateCalling || c.CallState() == callstate.StateRinging {
			if c.agent != nil {
				c.agent.handleOutboundInviteNoAnswerTimeout(c)
				return
			}
			c.SetOutboundCancelReason("no_answer")
			_ = c.TransitionChecked(callstate.StateTerminating)
			c.CloseDone()
		}
	})
	c.noAnswerTimer = timer
	c.outboundNoAnswerStop = func() { timer.Stop() }
	c.mu.Unlock()
	return nil
}

// StopOutboundNoAnswerTimer preserves the recovered void callback API.
func (c *Call) StopOutboundNoAnswerTimer() {
	_ = c.StopOutboundNoAnswerTimerCurrent()
}

// StopOutboundNoAnswerTimerCurrent clears and executes the owned stop callback.
func (c *Call) StopOutboundNoAnswerTimerCurrent() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	stop := c.outboundNoAnswerStop
	timer := c.noAnswerTimer
	c.outboundNoAnswerStop = nil
	c.noAnswerTimer = nil
	c.mu.Unlock()
	if stop != nil {
		stop()
	} else if timer != nil {
		timer.Stop()
	}
	return nil
}

// EnsureTimerStopped preserves the recovered session-timer cleanup API.
func (c *Call) EnsureTimerStopped() {
	_ = c.stopSessionTimer()
}

// EnsureTimerStoppedCurrent cancels every call-owned timer.
func (c *Call) EnsureTimerStoppedCurrent() error {
	if c == nil {
		return nil
	}
	c.StopPrackTimer()
	c.CancelOutboundInviteTimer()
	return c.stopSessionTimer()
}

func (c *Call) stopSessionTimer() error {
	if c == nil {
		return nil
	}
	c.SessionTimerMu.Lock()
	if c.SessionTimer != nil {
		c.SessionTimer.Stop()
		c.SessionTimer = nil
	}
	c.SessionTimerMu.Unlock()
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
