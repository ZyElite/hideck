package imscore

import (
	"strings"
	"time"
)

func (s *Service) recordOutboundSMSAudit(traceID, callID, to string, textLength int) {
	if s == nil {
		return
	}
	s.fragmentMu.Lock()
	s.outboundSMSAudits = append(s.outboundSMSAudits, outboundSMSAudit{
		At: time.Now(), TraceID: strings.TrimSpace(traceID),
		CallID: strings.TrimSpace(callID), To: strings.TrimSpace(to), Len: textLength,
	})
	if len(s.outboundSMSAudits) > fragmentAuditLimit {
		s.outboundSMSAudits = append([]outboundSMSAudit(nil), s.outboundSMSAudits[len(s.outboundSMSAudits)-fragmentAuditLimit:]...)
	}
	s.fragmentMu.Unlock()
}

func (s *Service) latestOutboundSMSAudit(maxAge time.Duration) (outboundSMSAudit, bool) {
	if s == nil {
		return outboundSMSAudit{}, false
	}
	cutoff := time.Now().Add(-maxAge)
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	for index := len(s.outboundSMSAudits) - 1; index >= 0; index-- {
		if s.outboundSMSAudits[index].At.After(cutoff) {
			return s.outboundSMSAudits[index], true
		}
	}
	return outboundSMSAudit{}, false
}

func (s *Service) appendFragmentAuditFailure(failure fragmentAuditFailure) {
	if s == nil {
		return
	}
	s.fragmentMu.Lock()
	s.appendFragmentAuditFailureLocked(failure)
	s.fragmentMu.Unlock()
}

func (s *Service) appendFragmentAuditFailureLocked(failure fragmentAuditFailure) {
	if failure.At.IsZero() {
		failure.At = time.Now()
	}
	s.fragmentAuditFailures = append(s.fragmentAuditFailures, failure)
	if len(s.fragmentAuditFailures) > fragmentAuditLimit {
		s.fragmentAuditFailures = append([]fragmentAuditFailure(nil), s.fragmentAuditFailures[len(s.fragmentAuditFailures)-fragmentAuditLimit:]...)
	}
}

func (s *Service) fragmentAuditSnapshot() map[string]interface{} {
	if s == nil {
		return map[string]interface{}{}
	}
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	failures := append([]fragmentAuditFailure(nil), s.fragmentAuditFailures...)
	interimFailures := append([]fragmentAuditFailure(nil), failures...)
	for index := range interimFailures {
		if interimFailures[index].InterimKey != "" {
			interimFailures[index].Key = interimFailures[index].InterimKey
		}
		if interimFailures[index].InterimReason != "" {
			interimFailures[index].Reason = interimFailures[index].InterimReason
		}
	}
	outbound := append([]outboundSMSAudit(nil), s.outboundSMSAudits...)
	return map[string]interface{}{
		"arrived_total":        s.fragmentArrivedTotal,
		"assembled_ok":         s.fragmentAssembledOK,
		"timeout_degraded":     s.fragmentTimeoutDegrade,
		"orphan_late_fragment": s.fragmentOrphanLate,
		"dup_fragment":         s.fragmentDup,
		"recent_failures":      failures,
		"recent_outbound_sms":  outbound,

		// Preserve the interim restoration names for additive compatibility.
		"timeout_degrade":     s.fragmentTimeoutDegrade,
		"orphan_late":         s.fragmentOrphanLate,
		"fragment_dup":        s.fragmentDup,
		"audit_failures":      interimFailures,
		"outbound_sms_audits": append([]outboundSMSAudit(nil), outbound...),
	}
}
