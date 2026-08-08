package logging

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	defaultRateLimitInterval = 5 * time.Second
	maxRateLimitEntries      = 4096
	rateLimitEntryLifetime   = 24 * time.Hour
)

var rateLimiter = struct {
	sync.Mutex
	lastEmission map[string]time.Time
}{
	lastEmission: make(map[string]time.Time, 256),
}

// WarnRate writes at warn level at most once per interval and normalized key.
// The current two-string form remains accepted and uses its historical
// five-second interval: WarnRate(key, message, fields...).
func WarnRate(key string, intervalOrMessage any, values ...any) {
	interval, msg, args := parseRateLogCall(intervalOrMessage, values)
	if shouldEmitRateLimited("warn", key, interval) {
		slog.Warn(msg, args...)
	}
}

// InfoRate writes at info level at most once per interval and normalized key.
// The current two-string form remains accepted and uses its historical
// five-second interval: InfoRate(key, message, fields...).
func InfoRate(key string, intervalOrMessage any, values ...any) {
	interval, msg, args := parseRateLogCall(intervalOrMessage, values)
	if shouldEmitRateLimited("info", key, interval) {
		slog.Info(msg, args...)
	}
}

func parseRateLogCall(intervalOrMessage any, values []any) (time.Duration, string, []any) {
	switch value := intervalOrMessage.(type) {
	case time.Duration:
		if len(values) == 0 {
			panic("logging: rate-limited log call is missing its message")
		}
		msg, ok := values[0].(string)
		if !ok {
			panic(fmt.Sprintf("logging: rate-limited log message has type %T", values[0]))
		}
		return value, msg, values[1:]
	case string:
		return defaultRateLimitInterval, value, values
	default:
		panic(fmt.Sprintf("logging: rate interval or message has type %T", intervalOrMessage))
	}
}

func shouldEmitRateLimited(level, key string, interval time.Duration) bool {
	level = strings.ToLower(strings.TrimSpace(level))
	key = strings.TrimSpace(key)
	if level == "" || key == "" || interval < 1 {
		return true
	}

	now := time.Now()
	compoundKey := level + ":" + key
	rateLimiter.Lock()
	defer rateLimiter.Unlock()
	if last, ok := rateLimiter.lastEmission[compoundKey]; ok && now.Sub(last) < interval {
		return false
	}
	rateLimiter.lastEmission[compoundKey] = now
	pruneRateLimitEntries(now)
	return true
}

func pruneRateLimitEntries(now time.Time) {
	if len(rateLimiter.lastEmission) <= maxRateLimitEntries {
		return
	}
	cutoff := now.Add(-rateLimitEntryLifetime)
	for key, emittedAt := range rateLimiter.lastEmission {
		if emittedAt.Before(cutoff) {
			delete(rateLimiter.lastEmission, key)
		}
	}
}
