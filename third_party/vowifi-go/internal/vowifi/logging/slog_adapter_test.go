package logging

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSlogAdapterPreservesAttrsCallerChainAndGroup(t *testing.T) {
	observed, logs := observer.New(zapcore.DebugLevel)
	handler := NewSlogHandler(zap.New(observed))
	handler = handler.WithAttrs([]slog.Attr{
		slog.String("caller", "transport"),
		slog.String("caller", "transport"),
		slog.String("device", "00101"),
	}).(*SlogAdapter)
	handler = handler.WithGroup("ims").(*SlogAdapter)
	slog.New(handler).Info("registered", "caller", "register", "status", 200)

	entry := onlyObservedEntry(t, logs)
	if entry.LoggerName != "" {
		t.Errorf("legacy core write unexpectedly retained logger name %q", entry.LoggerName)
	}
	contextMap := entry.ContextMap()
	for key, want := range map[string]any{
		"device":       "00101",
		"status":       int64(200),
		"caller":       "register",
		"caller_chain": "transport -> register",
	} {
		if got := contextMap[key]; got != want {
			t.Errorf("field %q = %#v, want %#v", key, got, want)
		}
	}
}

func TestSlogAdapterUsesRecordPC(t *testing.T) {
	observed, logs := observer.New(zapcore.DebugLevel)
	logFromAdapterTest(slog.New(NewSlogHandler(zap.New(observed))))
	entry := onlyObservedEntry(t, logs)
	if !entry.Caller.Defined || !strings.HasSuffix(entry.Caller.File, "slog_adapter_test.go") {
		t.Fatalf("caller = %+v", entry.Caller)
	}
}

func TestSlogAdapterNormalizesKnownReadErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       string
		wantLevel zapcore.Level
	}{
		{name: "closed", err: "use of closed network connection", wantLevel: zapcore.DebugLevel},
		{name: "eof", err: "EOF", wantLevel: zapcore.DebugLevel},
		{name: "reset", err: "read: connection reset by peer", wantLevel: zapcore.WarnLevel},
		{name: "timeout", err: "i/o timeout", wantLevel: zapcore.WarnLevel},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed, logs := observer.New(zapcore.DebugLevel)
			logger := slog.New(NewSlogHandler(zap.New(observed)))
			logger.Error(" Read Error ", "error", errors.New(test.err))
			entry := onlyObservedEntry(t, logs)
			if entry.Level != test.wantLevel || entry.Message != normalizedReadErrorMsg {
				t.Fatalf("entry = level %s message %q", entry.Level, entry.Message)
			}
		})
	}
}

func TestSlogAdapterLeavesUnknownReadErrorUnchanged(t *testing.T) {
	observed, logs := observer.New(zapcore.DebugLevel)
	logger := slog.New(NewSlogHandler(zap.New(observed)))
	logger.Error(readErrorMessage, "error", errors.New("unexpected framing"))
	entry := onlyObservedEntry(t, logs)
	if entry.Level != zapcore.ErrorLevel || entry.Message != readErrorMessage {
		t.Fatalf("entry = level %s message %q", entry.Level, entry.Message)
	}
}

func TestSlogAdapterEnabledDefersFilteringToZap(t *testing.T) {
	observed, logs := observer.New(zapcore.ErrorLevel)
	handler := NewSlogHandler(zap.New(observed))
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Enabled rejected debug before zap filtering")
	}
	slog.New(handler).Debug("filtered")
	if logs.Len() != 0 {
		t.Fatalf("zap core did not filter debug: %v", logs.All())
	}
}

func TestCallerFromPCZeroAndLevelMapping(t *testing.T) {
	if caller := callerFromPC(0); caller.Defined {
		t.Fatalf("zero PC caller = %+v", caller)
	}
	levels := map[slog.Level]zapcore.Level{
		slog.Level(-8):  zapcore.DebugLevel,
		slog.LevelDebug: zapcore.DebugLevel,
		slog.Level(0):   zapcore.InfoLevel,
		slog.Level(4):   zapcore.WarnLevel,
		slog.Level(8):   zapcore.ErrorLevel,
	}
	for input, want := range levels {
		if got := toZapLevel(input); got != want {
			t.Errorf("toZapLevel(%d) = %s, want %s", input, got, want)
		}
	}
}

func TestDedupeNonEmpty(t *testing.T) {
	got := dedupeNonEmpty([]string{" a ", "", "b", "a", " b ", "c"})
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dedupeNonEmpty() = %v, want %v", got, want)
	}
}

func TestWithCallerOption(t *testing.T) {
	if !WithCaller(true).AddSource || WithCaller(false).AddSource {
		t.Fatal("WithCaller did not preserve AddSource")
	}
}

func logFromAdapterTest(logger *slog.Logger) {
	logger.LogAttrs(context.Background(), slog.LevelInfo, "source", slog.Time("at", time.Unix(1, 0)))
}

func onlyObservedEntry(t *testing.T, logs *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("observed %d entries, want 1: %v", len(entries), entries)
	}
	return entries[0]
}
