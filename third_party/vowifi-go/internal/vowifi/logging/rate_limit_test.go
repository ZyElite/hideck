package logging

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestShouldEmitRateLimitedNormalizesLevelAndKey(t *testing.T) {
	resetRateLimiter(t)
	interval := time.Hour
	if !shouldEmitRateLimited(" WARN ", " event ", interval) {
		t.Fatal("first emission was suppressed")
	}
	if shouldEmitRateLimited("warn", "event", interval) {
		t.Fatal("normalized duplicate was emitted")
	}
	if !shouldEmitRateLimited("info", "event", interval) {
		t.Fatal("different level shared the warn rate bucket")
	}
}

func TestShouldEmitRateLimitedBypassesInvalidBoundary(t *testing.T) {
	resetRateLimiter(t)
	for _, test := range []struct {
		level    string
		key      string
		interval time.Duration
	}{
		{level: "", key: "event", interval: time.Hour},
		{level: "warn", key: "", interval: time.Hour},
		{level: "warn", key: "event", interval: 0},
	} {
		if !shouldEmitRateLimited(test.level, test.key, test.interval) ||
			!shouldEmitRateLimited(test.level, test.key, test.interval) {
			t.Fatalf("invalid rate boundary was limited: %+v", test)
		}
	}
}

func TestShouldEmitRateLimitedPrunesStaleEntries(t *testing.T) {
	resetRateLimiter(t)
	rateLimiter.Lock()
	stale := time.Now().Add(-2 * rateLimitEntryLifetime)
	for index := 0; index <= maxRateLimitEntries; index++ {
		rateLimiter.lastEmission[fmt.Sprintf("warn:stale-%d", index)] = stale
	}
	rateLimiter.Unlock()

	if !shouldEmitRateLimited("warn", "fresh", time.Hour) {
		t.Fatal("fresh entry was suppressed")
	}
	rateLimiter.Lock()
	defer rateLimiter.Unlock()
	if len(rateLimiter.lastEmission) != 1 {
		t.Fatalf("rate entries after prune = %d, want 1", len(rateLimiter.lastEmission))
	}
}

func TestRateLogLegacyAndCurrentForms(t *testing.T) {
	resetRateLimiter(t)
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	WarnRate("legacy", time.Hour, "legacy-message", "attempt", 1)
	WarnRate("legacy", time.Hour, "suppressed-message")
	InfoRate("current", "current-message", "ready", true)
	logs := output.String()
	for _, expected := range []string{"legacy-message", `"attempt":1`, "current-message", `"ready":true`} {
		if !strings.Contains(logs, expected) {
			t.Errorf("rate log output missing %q: %s", expected, logs)
		}
	}
	if strings.Contains(logs, "suppressed-message") {
		t.Fatalf("duplicate rate log was emitted: %s", logs)
	}
}

func resetRateLimiter(t *testing.T) {
	t.Helper()
	rateLimiter.Lock()
	previous := rateLimiter.lastEmission
	rateLimiter.lastEmission = make(map[string]time.Time, 256)
	rateLimiter.Unlock()
	t.Cleanup(func() {
		rateLimiter.Lock()
		rateLimiter.lastEmission = previous
		rateLimiter.Unlock()
	})
}
