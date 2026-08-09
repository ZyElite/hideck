package imscore

import (
	"errors"
	"strings"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

type registerFailureOutcome struct {
	reason       string
	kind         int32
	nextRegister time.Time
	retryDelay   time.Duration
}

func decideRegisterFailureOutcome(
	now time.Time,
	result registerAttemptResult,
	registerPolicy policy.IMSRegisterPolicy,
	transportFailure bool,
) registerFailureOutcome {
	delay := temporaryRegisterRetryInterval(registerPolicy)
	outcome := registerFailureOutcome{
		reason: "network", kind: registrationRejectedTemporary,
		nextRegister: now.Add(delay), retryDelay: delay,
	}
	if result.retryAfter > 0 {
		outcome.reason = "retry_after"
		outcome.retryDelay = result.retryAfter
		outcome.nextRegister = now.Add(result.retryAfter)
		return outcome
	}
	if result.statusCode == 423 && result.minExpires > 0 {
		outcome.reason = "min_expires"
		return outcome
	}
	if transportFailure {
		outcome.retryDelay = 5 * time.Second
		outcome.nextRegister = now.Add(outcome.retryDelay)
		return outcome
	}
	if isTemporaryRegisterSIPResponse(registerPolicy, result.statusCode) {
		outcome.reason = "temporary"
		return outcome
	}
	if isForbiddenRegisterSIPResponse(registerPolicy, result.statusCode) {
		return permanentRegisterFailure(now, "forbidden", 5*time.Minute)
	}
	if result.statusCode >= 400 {
		return permanentRegisterFailure(now, "permanent", 10*time.Minute)
	}
	return outcome
}

func permanentRegisterFailure(now time.Time, reason string, delay time.Duration) registerFailureOutcome {
	return registerFailureOutcome{
		reason: reason, kind: registrationRejectedPermanent,
		nextRegister: now.Add(delay), retryDelay: delay,
	}
}

func nextRegisterAtAfterSuccess(now time.Time, delay time.Duration) time.Time {
	return now.Add(delay)
}

func (s *Service) handleRegisterAPDUBusy(err error) error {
	if s == nil || !errors.Is(err, enginesim.ErrAPDUBusy) {
		return err
	}
	s.mu.Lock()
	s.nextRegister = time.Now().Add(3 * time.Second)
	s.lastError = err.Error()
	s.lastRegisterErr = err.Error()
	s.mu.Unlock()
	s.reRegisterPending.Store(false)
	s.transitionRegStatus(registrationRejectedTemporary)
	return err
}

func (s *Service) triggerRegisterReconnect() {
	if s == nil || s.OnReconnectNeeded == nil || !s.reconnectTriggering.CompareAndSwap(false, true) {
		return
	}
	callback := s.OnReconnectNeeded
	go func() {
		defer s.reconnectTriggering.Store(false)
		callback()
	}()
}

type registerRetryPolicy struct {
	maxAuthChallenges             int
	defaultInitialRetryStatusCode map[int]struct{}
}

func defaultRegisterRetryPolicy(registerPolicy policy.IMSRegisterPolicy) registerRetryPolicy {
	normalized := normalizedRegisterPolicy(registerPolicy)
	statusCodes := make(map[int]struct{}, len(normalized.InitialRejectFallbackStatusCodes))
	for _, statusCode := range normalized.InitialRejectFallbackStatusCodes {
		statusCodes[statusCode] = struct{}{}
	}
	return registerRetryPolicy{maxAuthChallenges: 3, defaultInitialRetryStatusCode: statusCodes}
}

func (p registerRetryPolicy) ShouldRetryDefaultInitial(statusCode int, warning, body string) bool {
	if strings.TrimSpace(warning) != "" || strings.TrimSpace(body) != "" {
		return false
	}
	_, retry := p.defaultInitialRetryStatusCode[statusCode]
	return retry
}
