package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestStructuredLogWrappers(t *testing.T) {
	previous := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	Info("info-event", "count", 2)
	Debug("debug-event", "ready", true)
	logs := output.String()
	for _, expected := range []string{`"msg":"info-event"`, `"count":2`, `"msg":"debug-event"`, `"ready":true`} {
		if !strings.Contains(logs, expected) {
			t.Errorf("structured log output missing %q: %s", expected, logs)
		}
	}
}

func TestRunLogWrappersOnlyEmitInGoRunMode(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		runModeOnce, goRunMode = sync.Once{}, false
	})

	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	setDetectedRunMode(true)
	RunInfo("run-info")
	RunDebug("run-debug")
	setDetectedRunMode(false)
	RunInfo("binary-info")
	RunDebug("binary-debug")

	logs := output.String()
	if !strings.Contains(logs, `"msg":"run-info"`) || !strings.Contains(logs, `"msg":"run-debug"`) {
		t.Fatalf("go run logs missing: %s", logs)
	}
	if strings.Contains(logs, "binary-info") || strings.Contains(logs, "binary-debug") {
		t.Fatalf("binary-only run logs were not suppressed: %s", logs)
	}
}

func setDetectedRunMode(enabled bool) {
	runModeOnce = sync.Once{}
	goRunMode = false
	runModeOnce.Do(func() { goRunMode = enabled })
}
