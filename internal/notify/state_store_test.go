package notify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileRuntimeStateStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notification-state.json")
	store := NewFileRuntimeStateStore(path)
	state := newRuntimeState()
	state.Weixin.Token = "wx-secret"
	state.Weixin.ContextTokens = map[string]string{"user-1": "context-1"}
	state.WeComBot.AllowedUsers = []string{"wecom-user"}
	state.QQ.AdminOpenID = "qq-user"
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	state.Weixin.ContextTokens["user-1"] = "mutated"

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Weixin.Token != "wx-secret" || loaded.Weixin.ContextTokens["user-1"] != "context-1" {
		t.Fatalf("Load() = %+v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
}

func TestFileRuntimeStateStoreRejectsInvalidContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notification-state.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileRuntimeStateStore(path).Load(); err == nil {
		t.Fatal("Load() accepted invalid JSON")
	}
}

func TestFileRuntimeStateStoreReturnsEmptyStateWhenMissing(t *testing.T) {
	state, err := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "missing.json")).Load()
	if err != nil || state.Version != runtimeStateVersion {
		t.Fatalf("Load() = %+v, %v", state, err)
	}
}

func TestFileRuntimeStateStoreUpdatePreservesOtherChannels(t *testing.T) {
	store := NewFileRuntimeStateStore(filepath.Join(t.TempDir(), "state.json"))
	state := newRuntimeState()
	state.QQ.AdminOpenID = "qq-admin"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(current *RuntimeState) error {
		current.Weixin.DefaultTarget = "wx-user"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.QQ.AdminOpenID != "qq-admin" || loaded.Weixin.DefaultTarget != "wx-user" {
		t.Fatalf("updated state = %+v", loaded)
	}
}
