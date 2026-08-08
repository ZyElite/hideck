package voice

import (
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	prackInitialRetry    = 500 * time.Millisecond
	prackMaximumRetry    = 4 * time.Second
	prackRetryExpiration = 64 * prackInitialRetry
)

func (c *Call) configurePrackRetransmission(retry func() error) error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	if retry == nil {
		return errors.New("voice: PRACK retransmission callback is unavailable")
	}
	c.mu.Lock()
	c.prackRetransmit = retry
	c.prackDeadline = time.Now().Add(prackRetryExpiration)
	c.mu.Unlock()
	return nil
}

// StartPrackRuntimeRetransmission starts the recovered SIP T1 backoff loop.
func (c *Call) StartPrackRuntimeRetransmission() error {
	if c == nil {
		return errors.New("voice: nil call")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prackRetransmit == nil {
		return errors.New("voice: PRACK retransmission context is unavailable")
	}
	c.cancelPrackTimerLocked()
	c.prackGeneration++
	c.schedulePrackRetryLocked(c.prackGeneration, prackInitialRetry)
	return nil
}

// StopPrackTimer stops retransmission as soon as PRACK receives a final response.
func (c *Call) StopPrackTimer() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.cancelPrackTimerLocked()
	c.prackGeneration++
	c.prackRetransmit = nil
	c.prackDeadline = time.Time{}
	c.mu.Unlock()
	return nil
}

func (c *Call) schedulePrackRetryLocked(generation uint64, delay time.Duration) {
	c.prackTimer = time.AfterFunc(delay, func() {
		c.runPrackRetry(generation, delay)
	})
}

func (c *Call) runPrackRetry(generation uint64, delay time.Duration) {
	retry, deadline, active := c.prackRetrySnapshot(generation)
	if !active {
		return
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		_ = c.StopPrackTimer()
		logging.WarnRate("ims-prack-retry-expired", "IMS PRACK 重传已到截止时间")
		return
	}
	if err := retry(); err != nil {
		logging.WarnRate("ims-prack-retry-failed", "IMS PRACK 重传失败", "err", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.prackGeneration || c.prackRetransmit == nil {
		return
	}
	next := delay * 2
	if next > prackMaximumRetry {
		next = prackMaximumRetry
	}
	c.schedulePrackRetryLocked(generation, next)
}

func (c *Call) prackRetrySnapshot(generation uint64) (func() error, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.prackGeneration || c.prackRetransmit == nil {
		return nil, time.Time{}, false
	}
	c.prackTimer = nil
	return c.prackRetransmit, c.prackDeadline, true
}

func (c *Call) cancelPrackTimerLocked() {
	if c.prackTimer == nil {
		return
	}
	c.prackTimer.Stop()
	c.prackTimer = nil
}
