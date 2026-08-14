package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsTelegramRecordingModeToVoice(t *testing.T) {
	path := writeTempConfig(t, "server:\n  port: 7575\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.RecordingMode != TelegramRecordingModeVoice {
		t.Fatalf("recording mode = %q, want voice", cfg.Telegram.RecordingMode)
	}
}

func TestLoadRejectsUnknownTelegramRecordingMode(t *testing.T) {
	path := writeTempConfig(t, "telegram:\n  recording_mode: document\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "voice|audio") {
		t.Fatalf("Load() error = %v", err)
	}
}
