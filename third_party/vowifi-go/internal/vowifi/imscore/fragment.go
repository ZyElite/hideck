package imscore

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	fragmentAuditLimit = 24
	fragmentKeySep     = "\x00"
)

type fragmentSessionIdentity struct {
	Sender        string
	ServiceCenter string
	Local         string
	Reference     int
	RefBits       int
	Total         int
}

func (s *Service) handleSMSFragment(sender string, fragment *smsFragment) (string, bool, error) {
	if s == nil || fragment == nil {
		return "", false, errors.New("imscore: nil SMS fragment")
	}
	if fragment.Total < 2 || fragment.Seq < 1 || fragment.Seq > fragment.Total {
		return "", false, errors.New("imscore: invalid SMS fragment bounds")
	}
	fragment.Time = time.Now()
	identity := fragmentSessionIdentity{
		Sender: sender, ServiceCenter: fragment.ServiceCenter, Local: fragment.ToURI,
		Reference: fragment.Ref, RefBits: fragment.RefBits, Total: fragment.Total,
	}
	key := buildFragmentSessionKey(identity)
	interimKey := buildInterimFragmentSessionKey(identity)
	s.startFragmentCleanup()
	s.fragmentMu.Lock()
	defer s.fragmentMu.Unlock()
	s.fragmentArrivedTotal++
	if expiredAt, expired := s.fragmentRecentExpired[key]; expired {
		if time.Since(expiredAt) <= 2*inboundSMSFragmentTTL {
			s.fragmentOrphanLate++
			return "", false, errors.New("imscore: late SMS fragment for expired session")
		}
		delete(s.fragmentRecentExpired, key)
	}
	fragments := s.fragmentCache[key]
	if len(fragments) == 0 && fragment.Seq != 1 {
		deviceID := ""
		if s.cfg != nil {
			deviceID = s.cfg.DeviceID
		}
		logging.WarnRate("sms-fragment-out-of-order-first:"+deviceID, time.Duration(0),
			"IMS 长短信出现乱序首包（缓存为空时先收到非第1片）",
			"device", deviceID, "sender", normalizeFragmentIdentity(sender),
			"ref", fragment.Ref, "seq", fragment.Seq, "total", fragment.Total)
	}
	if reason, collision := detectFragmentKeyCollision(fragments, fragment); collision {
		delete(s.fragmentCache, key)
		s.appendFragmentAuditFailureLocked(fragmentAuditFailure{
			At: time.Now(), Key: key, InterimKey: interimKey,
			Sender: normalizeFragmentIdentity(sender), Received: len(fragments),
			Total: fragment.Total, SeqList: fragmentSeqList(fragments), Reason: reason,
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

func (s *Service) startFragmentCleanup() {
	if s == nil {
		return
	}
	s.fragmentCleanupOnce.Do(func() {
		s.networkDone.Add(1)
		go s.fragmentCacheCleanupLoop(inboundSMSFragmentTTL)
	})
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
	var degraded []degradedMessage
	s.fragmentMu.Lock()
	for key, fragments := range s.fragmentCache {
		firstSeen, latest := fragmentBounds(fragments)
		if latest.IsZero() || !latest.Before(cutoff) {
			continue
		}
		delete(s.fragmentCache, key)
		s.fragmentRecentExpired[key] = now
		s.fragmentTimeoutDegrade++
		total := fragmentTotal(fragments)
		first := firstSMSFragment(fragments)
		interimKey := ""
		if first != nil {
			interimKey = buildInterimFragmentSessionKey(fragmentSessionIdentity{
				Sender: senderFromFragmentKey(key), ServiceCenter: first.ServiceCenter, Local: first.ToURI,
				Reference: first.Ref, RefBits: first.RefBits, Total: total,
			})
		}
		s.appendFragmentAuditFailureLocked(fragmentAuditFailure{
			At: now, Key: key, InterimKey: interimKey, Sender: senderFromFragmentKey(key),
			Received: len(fragments), Total: total,
			MissingSeq: missingSMSSeqs(fragments, total),
			SeqList:    fragmentSeqList(fragments), Reason: "timeout_degraded", InterimReason: "timeout",
		})
		if first != nil {
			missing := missingSMSSeqs(fragments, total)
			degraded = append(degraded, degradedMessage{
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
		logging.WarnRate("sms-fragment-timeout:"+item.key, time.Duration(0),
			"IMS 长短信分片超时（审计模式，不发送 480）",
			"device", s.cfg.DeviceID, "key", item.key, "fragments", item.received,
			"expected_total", item.total, "received_seq", item.seqList,
			"missing_seq", item.missing, "first_seen", item.first, "last_seen", item.latest,
			"age_ms", time.Since(item.latest).Milliseconds(), "acked_seq", item.ackedSeq)
		if strings.TrimSpace(item.message.content) != "" {
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
	if strings.HasPrefix(key, "sender=") {
		sender, _, _ := strings.Cut(strings.TrimPrefix(key, "sender="), "|")
		return sender
	}
	sender, _, _ := strings.Cut(key, fragmentKeySep)
	return sender
}

func buildFragmentSessionKey(identity fragmentSessionIdentity) string {
	bits := identity.RefBits
	if bits != 16 {
		bits = 8
	}
	return fmt.Sprintf("sender=%s|ref=%d|bits=%d|sc=%s|local=%s",
		normalizeFragmentIdentity(identity.Sender), identity.Reference, bits,
		normalizeFragmentIdentity(identity.ServiceCenter), normalizeFragmentIdentity(identity.Local))
}

func buildInterimFragmentSessionKey(identity fragmentSessionIdentity) string {
	reference := identity.Reference + identity.RefBits<<16
	return strings.Join([]string{
		normalizeFragmentIdentity(identity.Sender), normalizeFragmentIdentity(identity.Local),
		strconv.Itoa(reference), strconv.Itoa(identity.Total),
	}, fragmentKeySep)
}

func normalizeFragmentIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "<>\"")
	if strings.HasPrefix(value, "sip:") || strings.HasPrefix(value, "tel:") {
		value = value[4:]
	}
	if separator := strings.IndexAny(value, ";>@"); separator >= 0 {
		value = value[:separator]
	}
	return strings.TrimSpace(value)
}

func formatIncompleteFragmentContent(content string, received, total int, missing string) string {
	return fmt.Sprintf("[incomplete %d/%d missing=%s] %s", received, total, missing, content)
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
