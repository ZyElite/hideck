package swu

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

const natKeepaliveDPDMargin = 150 * time.Second

type kernelOutboundActivity interface {
	LastOutboundUse() (time.Time, error)
}

func (s *Session) beginNATKeepalive() bool {
	s.timersMu.Lock()
	defer s.timersMu.Unlock()
	if s.natKeepaliveStarted || s.ctx.Err() != nil {
		return false
	}
	s.natKeepaliveStarted = true
	return true
}

func (s *Session) scheduleNATKeepalive(interval, delay time.Duration) {
	s.armTimer(&s.natKeepalive, delay, func() {
		idle := s.outboundIdleDuration()
		next, sendKeepalive, sendDPD := natKeepaliveDecision(interval, idle)
		if sendDPD {
			if err := s.DPDProbe(); err != nil {
				s.failEstablishedControl(fmt.Errorf("swu: NAT keepalive DPD failed: %w", err))
				return
			}
		}
		if sendKeepalive {
			if err := s.sendNATKeepalive(); err != nil {
				s.failEstablishedControl(fmt.Errorf("swu: NAT keepalive failed: %w", err))
				return
			}
		}
		s.scheduleNATKeepalive(interval, next)
	})
}

func natKeepaliveDecision(interval, idle time.Duration) (
	next time.Duration,
	sendKeepalive bool,
	sendDPD bool,
) {
	if idle > interval+natKeepaliveDPDMargin {
		return interval, false, true
	}
	if idle >= interval {
		return interval, true, false
	}
	return interval - idle, false, false
}

func (s *Session) initializeInboundActivity() {
	s.activityMu.Lock()
	if s.lastInboundTime.IsZero() {
		s.lastInboundTime = time.Now()
	}
	s.activityMu.Unlock()
}

func (s *Session) initializeOutboundActivity() {
	s.activityMu.Lock()
	if s.lastOutboundTime.IsZero() {
		s.lastOutboundTime = time.Now()
	}
	s.activityMu.Unlock()
}

func (s *Session) markInboundActivity() {
	s.activityMu.Lock()
	s.lastInboundTime = time.Now()
	s.activityMu.Unlock()
}

func (s *Session) markOutboundActivity() {
	s.markOutboundActivityAt(time.Now())
}

func (s *Session) markOutboundActivityAt(at time.Time) {
	s.activityMu.Lock()
	s.lastOutboundTime = at
	s.activityMu.Unlock()
}

func (s *Session) inboundIdleDuration() time.Duration {
	s.activityMu.RLock()
	last := s.lastInboundTime
	s.activityMu.RUnlock()
	return nonNegativeIdle(last)
}

func (s *Session) outboundIdleDuration() time.Duration {
	return nonNegativeIdle(s.latestOutboundActivity())
}

func (s *Session) latestOutboundActivity() time.Time {
	s.activityMu.RLock()
	last := s.lastOutboundTime
	s.activityMu.RUnlock()
	activity, ok := s.currentKernelDataPlane().(kernelOutboundActivity)
	if !ok {
		return last
	}
	kernelLast, err := activity.LastOutboundUse()
	if err != nil {
		s.Logger.Warn("read XFRM outbound activity failed", zap.Error(err))
		return last
	}
	if kernelLast.After(last) {
		s.markOutboundActivityAt(kernelLast)
		return kernelLast
	}
	return last
}

func nonNegativeIdle(last time.Time) time.Duration {
	if last.IsZero() {
		return 0
	}
	idle := time.Since(last)
	if idle < 0 {
		return 0
	}
	return idle
}
