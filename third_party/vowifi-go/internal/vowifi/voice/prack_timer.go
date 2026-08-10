package voice

import (
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	prackInitialRetry    = 500 * time.Millisecond
	prackMaximumRetry    = 4 * time.Second
	prackRetryExpiration = 64 * prackInitialRetry
)

// StartPrackRuntimeRetransmission starts the recovered SIP T1 backoff loop.
func (c *Call) StartPrackRuntimeRetransmission(retry func()) {
	c.startPrackRuntimeRetransmission(prackInitialRetry, retry)
}

func (c *Call) startPrackRuntimeRetransmission(initial time.Duration, retry func()) {
	if c == nil {
		return
	}
	c.StopPrackTimer()
	if retry == nil || initial <= 0 {
		return
	}
	c.mu.Lock()
	c.prackGeneration++
	c.prackRetransmit = retry
	c.prackDeadline = time.Now().Add(prackRetryExpiration)
	c.schedulePrackRetryLocked(c.prackGeneration, initial)
	c.mu.Unlock()
}

// StopPrackTimer stops retransmission as soon as PRACK receives a final response.
func (c *Call) StopPrackTimer() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cancelPrackTimerLocked()
	c.prackGeneration++
	c.prackRetransmit = nil
	c.prackDeadline = time.Time{}
	c.mu.Unlock()
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
		c.StopPrackTimer()
		logging.WarnRate("ims-prack-retry-expired", "IMS PRACK 重传已到截止时间")
		return
	}
	retry()
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

func (c *Call) prackRetrySnapshot(generation uint64) (func(), time.Time, bool) {
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
