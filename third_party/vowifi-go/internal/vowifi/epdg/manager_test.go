package epdg

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"go.uber.org/zap"
)

func TestManagerStartsSessionWithConfiguredEPDG(t *testing.T) {
	manager := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session, err := manager.Start(ctx, "device-1", &swu.Config{EPDGAddr: "epdg.example.test"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := session.SnapshotMap()["epdg"]; got != "epdg.example.test" {
		t.Fatalf("session ePDG = %v", got)
	}
	if _, exists := manager.Snapshot("device-1"); !exists {
		t.Fatal("started session missing from snapshot")
	}
	if err := manager.Stop("device-1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, exists := manager.Snapshot("device-1"); exists {
		t.Fatal("stopped session remained in snapshot")
	}
}

func TestManagerWaitReturnsSessionFailure(t *testing.T) {
	manager := newTestManager(t)
	if _, err := manager.Start(context.Background(), "broken", &swu.Config{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer manager.Stop("broken")
	_, err := manager.Wait(context.Background(), "broken", 2*time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "ePDG 会话失败: ") {
		t.Fatalf("Wait error = %v", err)
	}
}

func TestManagerWaitHonorsContextAndTimeout(t *testing.T) {
	manager := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Wait(ctx, "missing", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
	if _, err := manager.Wait(context.Background(), "missing", time.Millisecond); err == nil || err.Error() != "等待 ePDG 隧道建立超时" {
		t.Fatalf("timeout error = %v", err)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	previous := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(previous) })
	manager := New()
	if manager == nil || manager.mgr == nil {
		t.Fatal("New returned an uninitialized manager")
	}
	return manager
}
