package imscore

import (
	"strings"
	"time"
)

const pendingSMSReportMatchWindow = 2 * time.Minute

func normalizeSMSCallID(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "<>\"")
	if separator := strings.IndexAny(value, "; \t\r\n"); separator >= 0 {
		value = value[:separator]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *Service) registerPendingSMS(callID string, pending *smsPendingInfo) {
	if s == nil || pending == nil {
		return
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if pending.CreatedAt.IsZero() {
		pending.CreatedAt = time.Now()
	}
	pending.CallID = callID
	pending.CallIDKey = normalizeSMSCallID(callID)
	s.outboundMu.Lock()
	s.smsPending[callID] = pending
	if pending.CallIDKey != "" {
		s.smsPendingNorm[pending.CallIDKey] = pending
	}
	s.outboundMu.Unlock()
}

func (s *Service) takePendingSMSByCallID(callID string) *smsPendingInfo {
	if s == nil || strings.TrimSpace(callID) == "" {
		return nil
	}
	s.outboundMu.Lock()
	pending := s.matchPendingByCallIDLocked(callID)
	s.removePendingSMSLocked(pending)
	s.outboundMu.Unlock()
	return pending
}

func (s *Service) matchPendingByCallIDLocked(callID string) *smsPendingInfo {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil
	}
	if pending := s.smsPending[callID]; pending != nil {
		return pending
	}
	return s.smsPendingNorm[normalizeSMSCallID(callID)]
}

func (s *Service) matchPendingByRPMRLocked(rpMR int, now time.Time) *smsPendingInfo {
	if rpMR < 0 {
		return nil
	}
	cutoff := now.Add(-pendingSMSReportMatchWindow)
	var newest *smsPendingInfo
	for _, pending := range s.smsPending {
		if pending == nil || pending.RPMR != rpMR || pending.CreatedAt.Before(cutoff) {
			continue
		}
		if newest == nil || pending.CreatedAt.After(newest.CreatedAt) {
			newest = pending
		}
	}
	return newest
}

func (s *Service) removePendingSMSLocked(pending *smsPendingInfo) {
	if pending == nil {
		return
	}
	if s.smsPending[pending.CallID] == pending {
		delete(s.smsPending, pending.CallID)
	}
	if s.smsPendingNorm[pending.CallIDKey] == pending {
		delete(s.smsPendingNorm, pending.CallIDKey)
	}
}

func (s *Service) completePendingSMSByReport(
	inReplyTo, callID string,
	rpMR int,
	result smsSendResult,
) (*smsPendingInfo, bool) {
	if s == nil {
		return nil, false
	}
	s.outboundMu.Lock()
	pending := s.matchPendingByCallIDLocked(inReplyTo)
	if pending == nil {
		pending = s.matchPendingByCallIDLocked(callID)
	}
	if pending == nil {
		pending = s.matchPendingByRPMRLocked(rpMR, time.Now())
	}
	s.removePendingSMSLocked(pending)
	s.outboundMu.Unlock()
	if pending == nil {
		return nil, false
	}
	if result.At.IsZero() {
		result.At = time.Now()
	}
	select {
	case pending.RespCh <- result:
	default:
	}
	return pending, true
}

func (s *Service) expirePendingSMSAfter(callID string, delay time.Duration) {
	if s == nil || strings.TrimSpace(callID) == "" {
		return
	}
	if delay <= 0 {
		s.takePendingSMSByCallID(callID)
		return
	}
	s.networkDone.Add(1)
	go func() {
		defer s.networkDone.Done()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.takePendingSMSByCallID(callID)
		case <-s.stop:
		}
	}()
}

func (s *Service) clearPendingSMS() {
	if s == nil {
		return
	}
	s.outboundMu.Lock()
	for _, pending := range s.smsPending {
		if pending == nil {
			continue
		}
		select {
		case pending.RespCh <- smsSendResult{Status: "stopped", Reason: "IMS service stopped", At: time.Now()}:
		default:
		}
	}
	s.smsPending = make(map[string]*smsPendingInfo)
	s.smsPendingNorm = make(map[string]*smsPendingInfo)
	s.outboundMu.Unlock()
}
