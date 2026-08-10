package imscore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

type degradedFragmentMessage struct {
	message  inboundSMS
	key      string
	received int
	total    int
	missing  string
	seqList  string
	callIDs  string
	ackedSeq string
	first    time.Time
	latest   time.Time
}

func (s *Service) startFragmentCleanup() {
	if s == nil {
		return
	}
	s.fragmentCleanupOnce.Do(func() {
		s.networkDone.Add(1)
		go s.fragmentCacheCleanupLoop(inboundSMSFragmentTTL)
	})
}

func (s *Service) fragmentCacheCleanupLoop(ttl time.Duration) {
	defer s.networkDone.Done()
	ticker := time.NewTicker(fragmentCleanupInterval(ttl))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.cleanupExpiredFragments(ttl); err != nil {
				logging.WarnRate("sms-fragment-cleanup:"+s.cfg.DeviceID, time.Minute,
					"IMS SMS fragment cleanup failed", "device", s.cfg.DeviceID, "error", err)
			}
		case <-s.stop:
			return
		}
	}
}

func fragmentCleanupInterval(ttl time.Duration) time.Duration {
	switch {
	case ttl < 30*time.Second:
		return time.Second
	case ttl < 2*time.Minute:
		return 5 * time.Second
	default:
		return 30 * time.Second
	}
}

func (s *Service) cleanupExpiredFragments(ttl time.Duration) error {
	if s == nil || ttl <= 0 {
		return nil
	}
	now := time.Now()
	var degraded []degradedFragmentMessage
	var failures []fragmentAuditFailure
	var cleanupErrors []error
	s.fragmentPersistMu.Lock()
	defer s.fragmentPersistMu.Unlock()
	s.fragmentMu.Lock()
	for key, fragments := range s.fragmentCache {
		_, latest := fragmentBounds(fragments)
		if fragmentDegradedAt(fragments).IsZero() &&
			(latest.IsZero() || !latest.Before(now.Add(-ttl))) {
			continue
		}
		item, failure, err := s.cleanupFragmentSessionLocked(
			key, fragments, now, s.inboundFragmentLifecycleStore(),
		)
		if err != nil {
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
		if item != nil {
			degraded = append(degraded, *item)
		}
		if failure != nil {
			failures = append(failures, *failure)
		}
	}
	s.clearOldFragmentTombstonesLocked(now, ttl)
	s.fragmentMu.Unlock()
	s.publishFragmentCleanupResults(failures, degraded)
	return errors.Join(cleanupErrors...)
}

func (s *Service) clearOldFragmentTombstonesLocked(now time.Time, ttl time.Duration) {
	for key, expiredAt := range s.fragmentRecentExpired {
		if expiredAt.Before(now.Add(-2 * ttl)) {
			delete(s.fragmentRecentExpired, key)
		}
	}
	for key, completed := range s.fragmentRecentComplete {
		if completed.At.Before(now.Add(-2 * ttl)) {
			delete(s.fragmentRecentComplete, key)
		}
	}
}

func (s *Service) cleanupFragmentSessionLocked(
	key string,
	fragments []*smsFragment,
	now time.Time,
	lifecycle SMSInboundFragmentLifecycleStore,
) (*degradedFragmentMessage, *fragmentAuditFailure, error) {
	if degradedAt := fragmentDegradedAt(fragments); !degradedAt.IsZero() {
		if now.Before(degradedAt.Add(inboundSMSLateReassemblyTTL)) {
			return nil, nil, nil
		}
		if err := s.deletePersistedFragments(key); err != nil {
			return nil, nil, err
		}
		delete(s.fragmentCache, key)
		s.fragmentRecentExpired[key] = now
		failure := buildFragmentCleanupFailure(key, fragments, now, "late_reassembly_expired")
		return nil, &failure, nil
	}
	if err := s.markOrDeleteDegradedSessionLocked(key, fragments, now, lifecycle); err != nil {
		return nil, nil, err
	}
	s.fragmentTimeoutDegrade++
	failure := buildFragmentCleanupFailure(key, fragments, now, "timeout_degraded")
	return buildDegradedFragmentMessage(key, fragments), &failure, nil
}

func (s *Service) markOrDeleteDegradedSessionLocked(
	key string,
	fragments []*smsFragment,
	now time.Time,
	lifecycle SMSInboundFragmentLifecycleStore,
) error {
	if lifecycle == nil {
		if err := s.deletePersistedFragments(key); err != nil {
			return err
		}
		delete(s.fragmentCache, key)
		s.fragmentRecentExpired[key] = now
		return nil
	}
	if err := lifecycle.MarkInboundFragmentsDegraded(s.inboundFragmentScope(key), now); err != nil {
		return fmt.Errorf("mark SMS fragment session degraded: %w", err)
	}
	for _, fragment := range fragments {
		if fragment != nil {
			fragment.DegradedAt = now
		}
	}
	return nil
}

func buildFragmentCleanupFailure(
	key string,
	fragments []*smsFragment,
	at time.Time,
	reason string,
) fragmentAuditFailure {
	total := fragmentTotal(fragments)
	first := firstSMSFragment(fragments)
	interimKey := ""
	if first != nil {
		interimKey = buildInterimFragmentSessionKey(fragmentSessionIdentity{
			Sender: senderFromFragmentKey(key), ServiceCenter: first.ServiceCenter, Local: first.ToURI,
			Reference: first.Ref, RefBits: first.RefBits, Total: total,
		})
	}
	interimReason := "timeout"
	if reason != "timeout_degraded" {
		interimReason = reason
	}
	return fragmentAuditFailure{
		At: at, Key: key, InterimKey: interimKey, Sender: senderFromFragmentKey(key),
		Received: len(fragments), Total: total, MissingSeq: missingSMSSeqs(fragments, total),
		SeqList: fragmentSeqList(fragments), Reason: reason, InterimReason: interimReason,
	}
}

func buildDegradedFragmentMessage(key string, fragments []*smsFragment) *degradedFragmentMessage {
	first := firstSMSFragment(fragments)
	if first == nil {
		return nil
	}
	firstSeen, latest := fragmentBounds(fragments)
	total := fragmentTotal(fragments)
	missing := missingSMSSeqs(fragments, total)
	return &degradedFragmentMessage{
		message: inboundSMS{
			sender: senderFromFragmentKey(key), targetURI: first.ToURI,
			content: formatIncompleteFragmentContent(
				assembleFragmentText(fragments), len(fragments), total, missing,
			),
			timestamp: latest,
		},
		key: key, received: len(fragments), total: total, missing: missing,
		seqList: fragmentSeqList(fragments), callIDs: fragmentCallIDList(fragments),
		ackedSeq: ackedFragmentSeqs(fragments), first: firstSeen, latest: latest,
	}
}

func (s *Service) publishFragmentCleanupResults(
	failures []fragmentAuditFailure,
	degraded []degradedFragmentMessage,
) {
	for _, failure := range failures {
		s.appendFragmentAuditFailure(failure)
	}
	for _, item := range degraded {
		logging.WarnRate("sms-fragment-timeout:"+item.key, time.Duration(0),
			"IMS 长短信分片超时（审计模式，不发送 480）",
			"device", s.cfg.DeviceID, "key", item.key, "fragments", item.received,
			"expected_total", item.total, "received_seq", item.seqList,
			"missing_seq", item.missing, "first_seen", item.first, "last_seen", item.latest,
			"age_ms", time.Since(item.latest).Milliseconds(), "acked_seq", item.ackedSeq)
		if strings.TrimSpace(item.message.content) == "" {
			continue
		}
		logging.WarnRate("sms-fragment-degraded:"+item.key, time.Duration(0),
			"IMS 长短信超时降级拼接并入库",
			"device", s.cfg.DeviceID, "sender", item.message.sender, "key", item.key,
			"received", item.received, "total", item.total, "missing_seq", item.missing,
			"seq_list", item.seqList, "call_ids", item.callIDs, "acked_seq", item.ackedSeq)
		s.publishLogNotification(formatVoWiFiIncompleteSMSMessage(
			s.cfg.DeviceID, item.message.sender, item.message.content,
			item.message.timestamp, item.received, item.total, item.missing,
		))
		s.publishInboundSMS(item.message)
	}
}
