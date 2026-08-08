package swu

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

const (
	defaultIKERekeyInterval   = 5000 * time.Second
	defaultChildRekeyInterval = 1800 * time.Second
	childRekeyStartOffset     = 30 * time.Second
	ikeRekeyJitterSeconds     = 5
	childRekeyJitterSeconds   = 10
	rekeyMaxFailures          = 2
	rekeyRetryInterval        = 60 * time.Second
)

type rekeyTimerSpec struct {
	name          string
	interval      time.Duration
	jitterSeconds int64
	reset         <-chan struct{}
	target        **time.Timer
	action        func() error
	immediateFail func(error) bool
	retryInterval time.Duration
}

func (s *Session) rekeyIntervals() (time.Duration, time.Duration) {
	s.mu.RLock()
	lifetime := s.authLifetime
	s.mu.RUnlock()
	var ikeInterval, childInterval time.Duration
	if s.cfg != nil {
		ikeInterval = s.cfg.RekeyIKESeconds
		childInterval = s.cfg.RekeyChildSeconds
	}
	if ikeInterval <= 0 {
		ikeInterval = defaultIKERekeyInterval
		if lifetime > 0 {
			ikeInterval = time.Duration(lifetime) * time.Second * 4 / 5
		}
	}
	if childInterval > 0 {
		return ikeInterval, childInterval
	}
	childInterval = defaultChildRekeyInterval
	if lifetime > 0 {
		childInterval = time.Duration(lifetime) * time.Second * 7 / 8
	}
	return ikeInterval, childInterval + childRekeyStartOffset
}

// startIKESARekeyTimer restores the original interval-taking timer API.
func (s *Session) startIKESARekeyTimer(interval time.Duration) {
	reset := make(chan struct{}, 1)
	s.mu.Lock()
	s.rekeyResetCh = reset
	s.mu.Unlock()
	s.startRekeyTimer(rekeyTimerSpec{
		name: "IKE SA", interval: interval, jitterSeconds: ikeRekeyJitterSeconds,
		reset: reset, target: &s.ikeRekeyTimer, action: s.RekeyIKESA,
	})
}

// startChildSARekeyTimer restores the original interval-taking timer API.
func (s *Session) startChildSARekeyTimer(interval time.Duration) {
	reset := make(chan struct{}, 1)
	s.mu.Lock()
	s.childRekeyResetCh = reset
	s.mu.Unlock()
	s.startRekeyTimer(rekeyTimerSpec{
		name: "CHILD_SA", interval: interval, jitterSeconds: childRekeyJitterSeconds,
		reset: reset, target: &s.childRekeyTimer, action: s.RekeyChildSA,
		immediateFail: isChildSANotFoundError,
	})
}

func isChildSANotFoundError(err error) bool {
	var rejection *createChildSARejectError
	return errors.As(err, &rejection) && rejection.NotifyType == ikev2.CHILD_SA_NOT_FOUND
}

func (s *Session) startRekeyTimer(spec rekeyTimerSpec) {
	if spec.interval <= 0 || spec.action == nil || spec.target == nil || s.ctx.Err() != nil {
		return
	}
	timer := time.NewTimer(rekeyDelay(spec.interval, spec.jitterSeconds))
	s.timersMu.Lock()
	if previous := *spec.target; previous != nil {
		previous.Stop()
	}
	*spec.target = timer
	s.timersMu.Unlock()
	s.rekeyTimerWG.Add(1)
	go func() {
		defer s.rekeyTimerWG.Done()
		s.runRekeyTimer(timer, spec)
	}()
}

func (s *Session) runRekeyTimer(timer *time.Timer, spec rekeyTimerSpec) {
	failures := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-spec.reset:
			failures = 0
			if !s.resetRekeyTimer(timer, rekeyDelay(spec.interval, spec.jitterSeconds)) {
				return
			}
		case <-timer.C:
			err := spec.action()
			if err == nil {
				failures = 0
				if !s.resetRekeyTimer(timer, rekeyDelay(spec.interval, spec.jitterSeconds)) {
					return
				}
				continue
			}
			failures++
			if failures >= rekeyMaxFailures || spec.immediateFail != nil && spec.immediateFail(err) {
				s.failEstablishedControl(fmt.Errorf("swu: %s rekey failed: %w", spec.name, err))
				return
			}
			retryInterval := spec.retryInterval
			if retryInterval <= 0 {
				retryInterval = rekeyRetryInterval
			}
			if !s.resetRekeyTimer(timer, retryInterval) {
				return
			}
		}
	}
}

func (s *Session) resetRekeyTimer(timer *time.Timer, delay time.Duration) bool {
	s.timersMu.Lock()
	defer s.timersMu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
	return true
}

func rekeyDelay(interval time.Duration, jitterSeconds int64) time.Duration {
	maximumJitter := time.Duration(jitterSeconds) * time.Second
	if jitterSeconds <= 0 || interval <= maximumJitter {
		return interval
	}
	return interval - time.Duration(rand.Int63n(jitterSeconds))*time.Second
}
