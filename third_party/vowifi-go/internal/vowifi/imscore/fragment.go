package imscore

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	fragmentAuditLimit = 24
	fragmentKeySep     = "\x00"
)

func (s *Service) handleSMSFragment(sender string, fragment *smsFragment) (string, bool, error) {
	if s == nil || fragment == nil {
		return "", false, errors.New("imscore: nil SMS fragment")
	}
	if fragment.Total < 2 || fragment.Seq < 1 || fragment.Seq > fragment.Total {
		return "", false, errors.New("imscore: invalid SMS fragment bounds")
	}
	if fragment.Time.IsZero() {
		fragment.Time = time.Now()
	}
	key := buildFragmentSessionKey(sender, fragment.ToURI, fragment.Ref, fragment.Total)
	s.fragmentCleanupOnce.Do(func() {
		s.networkDone.Add(1)
		go s.fragmentCacheCleanupLoop(inboundSMSFragmentTTL)
	})
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	s.fragmentArrivedTotal++
	if expiredAt, expired := s.fragmentRecentExpired[key]; expired {
		if time.Since(expiredAt) <= inboundSMSFragmentTTL {
			s.fragmentOrphanLate++
			return "", false, errors.New("imscore: late SMS fragment for expired session")
		}
		delete(s.fragmentRecentExpired, key)
	}
	fragments := s.fragmentCache[key]
	if reason, collision := detectFragmentKeyCollision(fragments, fragment); collision {
		delete(s.fragmentCache, key)
		s.appendFragmentAuditFailureLocked(fragmentAuditFailure{
			At: time.Now(), Key: key, Sender: normalizeFragmentIdentity(sender),
			Received: len(fragments), Total: fragment.Total,
			SeqList: fragmentSeqList(fragments), Reason: reason,
		})
		return "", false, fmt.Errorf("imscore: SMS fragment key collision: %s", reason)
	}
	for _, existing := range fragments {
		if existing != nil && existing.Seq == fragment.Seq {
			s.fragmentDup++
			return "", false, nil
		}
	}
	s.fragmentCache[key] = append(fragments, fragment)
	fragments = s.fragmentCache[key]
	if len(fragments) != fragment.Total || len(missingSMSSeqs(fragments, fragment.Total)) != 0 {
		return "", false, nil
	}
	content := assembleFragmentText(fragments)
	delete(s.fragmentCache, key)
	s.fragmentAssembledOK++
	return content, true, nil
}

func detectFragmentKeyCollision(fragments []*smsFragment, candidate *smsFragment) (string, bool) {
	if len(fragments) == 0 || candidate == nil {
		return "", false
	}
	if fragments[0] != nil && fragments[0].Total != candidate.Total {
		return "total_mismatch", true
	}
	for _, fragment := range fragments {
		if fragment == nil || fragment.Seq != candidate.Seq {
			continue
		}
		if strings.TrimSpace(fragment.Content) != strings.TrimSpace(candidate.Content) {
			return "sequence_content_mismatch", true
		}
	}
	return "", false
}

func (s *Service) fragmentCacheCleanupLoop(ttl time.Duration) {
	defer s.networkDone.Done()
	interval := fragmentCleanupInterval(ttl)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.cleanupExpiredFragments(ttl)
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

func (s *Service) cleanupExpiredFragments(ttl time.Duration) {
	if s == nil || ttl <= 0 {
		return
	}
	now := time.Now()
	cutoff := now.Add(-ttl)
	type degradedMessage struct {
		message  inboundSMS
		received int
		total    int
		missing  string
	}
	var degraded []degradedMessage
	s.fragmentMu.Lock()
	for key, fragments := range s.fragmentCache {
		_, latest := fragmentBounds(fragments)
		if latest.IsZero() || !latest.Before(cutoff) {
			continue
		}
		delete(s.fragmentCache, key)
		s.fragmentRecentExpired[key] = now
		s.fragmentTimeoutDegrade++
		total := fragmentTotal(fragments)
		s.appendFragmentAuditFailureLocked(fragmentAuditFailure{
			At: now, Key: key, Sender: senderFromFragmentKey(key),
			Received: len(fragments), Total: total,
			MissingSeq: missingSMSSeqs(fragments, total),
			SeqList:    fragmentSeqList(fragments), Reason: "timeout",
		})
		if first := firstSMSFragment(fragments); first != nil {
			degraded = append(degraded, degradedMessage{
				message: inboundSMS{
					sender: senderFromFragmentKey(key), targetURI: first.ToURI,
					content: assembleFragmentText(fragments), timestamp: latest,
				},
				received: len(fragments), total: total,
				missing: missingSMSSeqs(fragments, total),
			})
		}
	}
	for key, expiredAt := range s.fragmentRecentExpired {
		if expiredAt.Before(now.Add(-2 * ttl)) {
			delete(s.fragmentRecentExpired, key)
		}
	}
	s.fragmentMu.Unlock()
	for _, item := range degraded {
		if strings.TrimSpace(item.message.content) != "" {
			s.publishLogNotification(formatVoWiFiIncompleteSMSMessage(
				s.cfg.DeviceID, item.message.sender, item.message.content,
				item.message.timestamp, item.received, item.total, item.missing,
			))
			s.publishInboundSMS(item.message)
		}
	}
}

func (s *Service) markFragmentAcked(key, callID string, rpMR byte, at time.Time) bool {
	if s == nil {
		return false
	}
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	for _, fragment := range s.fragmentCache[key] {
		if fragment == nil || fragment.RpMr != rpMR ||
			(strings.TrimSpace(callID) != "" && !strings.EqualFold(fragment.CallID, callID)) {
			continue
		}
		fragment.AckSent = true
		fragment.AckSentAt = at
		return true
	}
	return false
}

func missingSMSSeqs(fragments []*smsFragment, total int) string {
	if total < 1 {
		return ""
	}
	present := make(map[int]bool, len(fragments))
	for _, fragment := range fragments {
		if fragment != nil {
			present[fragment.Seq] = true
		}
	}
	missing := make([]string, 0)
	for sequence := 1; sequence <= total; sequence++ {
		if !present[sequence] {
			missing = append(missing, strconv.Itoa(sequence))
		}
	}
	return strings.Join(missing, ",")
}

func fragmentSeqList(fragments []*smsFragment) string {
	sequences := make([]int, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment != nil {
			sequences = append(sequences, fragment.Seq)
		}
	}
	sort.Ints(sequences)
	values := make([]string, len(sequences))
	for index, sequence := range sequences {
		values[index] = strconv.Itoa(sequence)
	}
	return strings.Join(values, ",")
}

func ackedFragmentSeqs(fragments []*smsFragment) string {
	acked := make([]*smsFragment, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment != nil && fragment.AckSent {
			acked = append(acked, fragment)
		}
	}
	return fragmentSeqList(acked)
}

func fragmentCallIDList(fragments []*smsFragment) string {
	values := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment != nil && strings.TrimSpace(fragment.CallID) != "" {
			values = append(values, strings.TrimSpace(fragment.CallID))
		}
	}
	return strings.Join(values, ",")
}

func fragmentBounds(fragments []*smsFragment) (time.Time, time.Time) {
	var earliest, latest time.Time
	for _, fragment := range fragments {
		if fragment == nil || fragment.Time.IsZero() {
			continue
		}
		if earliest.IsZero() || fragment.Time.Before(earliest) {
			earliest = fragment.Time
		}
		if latest.IsZero() || fragment.Time.After(latest) {
			latest = fragment.Time
		}
	}
	return earliest, latest
}

func senderFromFragmentKey(key string) string {
	sender, _, _ := strings.Cut(key, fragmentKeySep)
	return sender
}

func buildFragmentSessionKey(sender, target string, reference, total int) string {
	return strings.Join([]string{
		normalizeFragmentIdentity(sender), normalizeFragmentIdentity(target),
		strconv.Itoa(reference), strconv.Itoa(total),
	}, fragmentKeySep)
}

func normalizeFragmentIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "<>\"")
	if strings.HasPrefix(value, "sip:") || strings.HasPrefix(value, "tel:") {
		value = value[4:]
	}
	if separator := strings.IndexAny(value, ";>"); separator >= 0 {
		value = value[:separator]
	}
	return strings.TrimSpace(value)
}

func assembleFragmentText(fragments []*smsFragment) string {
	ordered := append([]*smsFragment(nil), fragments...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left] == nil {
			return false
		}
		if ordered[right] == nil {
			return true
		}
		return ordered[left].Seq < ordered[right].Seq
	})
	var content strings.Builder
	for _, fragment := range ordered {
		if fragment != nil {
			content.WriteString(fragment.Content)
		}
	}
	return content.String()
}

func fragmentTotal(fragments []*smsFragment) int {
	for _, fragment := range fragments {
		if fragment != nil {
			return fragment.Total
		}
	}
	return 0
}

func firstSMSFragment(fragments []*smsFragment) *smsFragment {
	for _, fragment := range fragments {
		if fragment != nil {
			return fragment
		}
	}
	return nil
}
