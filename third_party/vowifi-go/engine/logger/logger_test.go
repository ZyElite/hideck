package logger

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestInitConsoleEncodingAndCaller(t *testing.T) {
	resetLoggerState()
	output := captureStderr(t, func() {
		if err := Init("debug", "console"); err != nil {
			t.Fatalf("Init: %v", err)
		}
		Info("packet trace", zap.ByteString("packet", []byte("IKE")))
	})

	if !regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]`).MatchString(output) {
		t.Fatalf("console time layout mismatch: %q", output)
	}
	for _, want := range []string{ansiBlue + "INFO " + ansiReset, "logger_test.go", "packet trace", `"packet": "IKE"`} {
		if !strings.Contains(output, want) {
			t.Errorf("console output missing %q: %q", want, output)
		}
	}
	if strings.Contains(output, "logger/logger.go") {
		t.Fatalf("wrapper caller was not skipped: %q", output)
	}
}

func TestInitJSONEncodingAndLevel(t *testing.T) {
	resetLoggerState()
	output := captureStderr(t, func() {
		if err := Init("warn", "json"); err != nil {
			t.Fatalf("Init: %v", err)
		}
		Debug("suppressed")
		Warn("packet trace", zap.Binary("packet", []byte{1, 2}))
	})

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("decode JSON log %q: %v", output, err)
	}
	if entry["level"] != "warn" || entry["msg"] != "packet trace" || entry["packet"] != "AQI=" {
		t.Fatalf("unexpected JSON entry: %#v", entry)
	}
	if _, ok := entry["time"].(string); !ok {
		t.Fatalf("JSON time is not ISO8601 text: %#v", entry["time"])
	}
	if _, exists := entry["ts"]; exists {
		t.Fatalf("legacy JSON output retained ts instead of time: %#v", entry)
	}
}

func TestErrorIncludesStacktrace(t *testing.T) {
	resetLoggerState()
	output := captureStderr(t, func() {
		if err := Init("error", "json"); err != nil {
			t.Fatalf("Init: %v", err)
		}
		Error("failed packet")
	})

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("decode JSON log %q: %v", output, err)
	}
	stacktrace, ok := entry["stacktrace"].(string)
	if !ok || !strings.Contains(stacktrace, "TestErrorIncludesStacktrace") {
		t.Fatalf("missing caller stacktrace: %#v", entry)
	}
}

func TestWithAddsStructuredFields(t *testing.T) {
	resetLoggerState()
	output := captureStderr(t, func() {
		if err := Init("info", "json"); err != nil {
			t.Fatalf("Init: %v", err)
		}
		With(zap.String("session", "swu-1")).Info("established")
	})

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("decode JSON log %q: %v", output, err)
	}
	if entry["session"] != "swu-1" || entry["msg"] != "established" {
		t.Fatalf("child logger fields missing: %#v", entry)
	}
}

func TestInitRunsOnlyOnce(t *testing.T) {
	resetLoggerState()
	if err := Init("info", "json"); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	first := L()
	if err := Init("debug", "console"); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if L() != first {
		t.Fatal("second Init replaced the process logger")
	}
	if L().Core().Enabled(zapcore.DebugLevel) {
		t.Fatal("second Init changed the configured level")
	}
}

func TestLazyInitializationIsConcurrentSafe(t *testing.T) {
	resetLoggerState()
	_ = captureStderr(t, func() {
		var group sync.WaitGroup
		group.Add(64)
		for range 64 {
			go func() {
				defer group.Done()
				Debug("suppressed by default info level")
			}()
		}
		group.Wait()
	})
	if global.Load() == nil || globalSugar.Load() == nil {
		t.Fatal("lazy initialization did not install both loggers")
	}
}

func TestInitFilePreservesAdditiveFileOutput(t *testing.T) {
	resetLoggerState()
	path := filepath.Join(t.TempDir(), "engine.log")
	if err := InitFile(zapcore.DebugLevel, path); err != nil {
		t.Fatalf("InitFile: %v", err)
	}
	Debug("file packet", zap.String("direction", "out"))
	if err := L().Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(content), "file packet") || !strings.Contains(string(content), "direction") {
		t.Fatalf("file output missing fields: %q", content)
	}
}

func TestFixedWidthColorLevelEncoder(t *testing.T) {
	tests := map[zapcore.Level]string{
		zapcore.DebugLevel:  ansiMagenta + "DEBUG" + ansiReset,
		zapcore.InfoLevel:   ansiBlue + "INFO " + ansiReset,
		zapcore.WarnLevel:   ansiYellow + "WARN " + ansiReset,
		zapcore.ErrorLevel:  ansiRed + "ERROR" + ansiReset,
		zapcore.DPanicLevel: ansiBrightRed + "DPANIC" + ansiReset,
	}
	config := zap.NewDevelopmentEncoderConfig()
	config.EncodeLevel = fixedWidthColorLevelEncoder
	encoder := zapcore.NewConsoleEncoder(config)
	for level, want := range tests {
		line, err := encoder.Clone().EncodeEntry(zapcore.Entry{Level: level}, nil)
		if err != nil {
			t.Fatalf("encode %s: %v", level, err)
		}
		if !strings.Contains(line.String(), want) {
			t.Errorf("%s output = %q, want %q", level, line.String(), want)
		}
		line.Free()
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]zapcore.Level{
		"debug": zapcore.DebugLevel,
		"info":  zapcore.InfoLevel,
		"warn":  zapcore.WarnLevel,
		"error": zapcore.ErrorLevel,
		"DEBUG": zapcore.InfoLevel,
		"":      zapcore.InfoLevel,
	}
	for input, want := range tests {
		if got := parseLevel(input); got != want {
			t.Errorf("parseLevel(%q) = %s, want %s", input, got, want)
		}
	}
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	previous := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = previous }()

	run()
	if logger := global.Load(); logger != nil {
		_ = logger.Sync()
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	_ = reader.Close()
	return string(output)
}

func resetLoggerState() {
	initOnce = sync.Once{}
	global.Store(nil)
	globalSugar.Store(nil)
}
