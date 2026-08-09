package imscore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	mtSMSFingerprintTTL     = 30 * time.Minute
	mtSMSFingerprintMaxSize = 8192
)

func buildMTSMSFingerprint(message inboundSMS, raw string) string {
	contentDigest := sha256.Sum256([]byte(strings.TrimSpace(message.content)))
	timestamp := ""
	if !message.timestamp.IsZero() {
		timestamp = message.timestamp.Truncate(time.Second).UTC().Format(time.RFC3339)
	}
	parts := []string{
		normalizeFragmentIdentity(message.sender),
		normalizeFragmentIdentity(message.targetURI),
		normalizeFragmentIdentity(firstSIPHeaderURI(rawSIPHeaderValue(raw, "From"))),
		normalizeFragmentIdentity(firstSIPHeaderURI(rawSIPHeaderValue(raw, "To"))),
		normalizeFragmentIdentity(firstSIPHeaderURI(rawSIPHeaderValue(raw, "P-Asserted-Identity"))),
		hex.EncodeToString(contentDigest[:]), timestamp,
		fmt.Sprintf("%d/%d/%d/%d/%d", message.rpMR, message.concatRef, message.refBits, message.total, message.partNo),
	}
	fingerprint := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(fingerprint[:])
}

func (s *Service) reserveMTSMSFingerprint(fingerprint string, now time.Time) bool {
	if s == nil || strings.TrimSpace(fingerprint) == "" {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mtSMSSeenMu.Lock()
	defer s.mtSMSSeenMu.Unlock()
	if s.mtSMSSeen == nil {
		s.mtSMSSeen = make(map[string]time.Time, 128)
	}
	if seenAt, exists := s.mtSMSSeen[fingerprint]; exists && now.Sub(seenAt) < mtSMSFingerprintTTL {
		s.mtSMSDedupHit.Add(1)
		return false
	}
	s.mtSMSSeen[fingerprint] = now
	if len(s.mtSMSSeen) > mtSMSFingerprintMaxSize {
		cutoff := now.Add(-mtSMSFingerprintTTL)
		for key, seenAt := range s.mtSMSSeen {
			if seenAt.Before(cutoff) {
				delete(s.mtSMSSeen, key)
			}
		}
	}
	return true
}

func (s *Service) shouldDispatchMTSMS(message inboundSMS, raw string) bool {
	fingerprint := buildMTSMSFingerprint(message, raw)
	return s.reserveMTSMSFingerprint(fingerprint, time.Now())
}
