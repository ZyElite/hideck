package runtimehost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"go.uber.org/zap"
)

func TestSWUTunnelAdapterUsesEPDGManagerLifecycle(t *testing.T) {
	previous := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(previous) })
	manager := epdg.New()
	adapter := newSWUTunnelAdapter(swuTunnelAdapterConfig{
		Manager: manager, DeviceID: "device-1",
		SessionConfig:        &swu.Config{EPDGAddr: "epdg.example.test"},
		EstablishmentTimeout: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Connect(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect error = %v", err)
	}
	if adapter.Session == nil || adapter.Session.SnapshotMap()["epdg"] != "epdg.example.test" {
		t.Fatalf("managed session = %+v", adapter.Session)
	}
	if _, exists := manager.Snapshot("device-1"); exists {
		t.Fatal("failed connect remained registered")
	}
	adapter.Shutdown()
}
