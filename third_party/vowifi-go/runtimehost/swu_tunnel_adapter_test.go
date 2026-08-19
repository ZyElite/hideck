package runtimehost

import (
	"context"
	"errors"
	"sync"
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

func TestSWUTunnelAdapterDoesNotRetryCanceledConnect(t *testing.T) {
	previous := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(previous) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if epdg.ShouldRetryFreshTunnel(ctx, epdg.ErrEstablishmentTimeout) {
		t.Fatal("canceled connect must not start a second tunnel")
	}
}

func TestSWUTunnelAdapterRetriesTimeoutWithFreshSession(t *testing.T) {
	manager := &retryEPDGManager{}
	adapter := newSWUTunnelAdapter(swuTunnelAdapterConfig{
		Manager: manager, DeviceID: "device-retry", SessionConfig: &swu.Config{},
		EstablishmentTimeout: time.Second,
	})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	starts, stops, sessions := manager.snapshot()
	if starts != 2 || stops != 1 {
		t.Fatalf("after retry starts=%d stops=%d, want 2/1", starts, stops)
	}
	if sessions[0] == sessions[1] || adapter.Session != sessions[1] {
		t.Fatal("retry did not install a fresh managed session")
	}
	adapter.Shutdown()
	adapter.Shutdown()
	_, stops, _ = manager.snapshot()
	if stops != 2 {
		t.Fatalf("shutdown stops=%d, want 2", stops)
	}
}

func TestSWUTunnelAdapterShutdownDuringRetryCleansSecondSession(t *testing.T) {
	manager := newBlockingRetryEPDGManager()
	adapter := newSWUTunnelAdapter(swuTunnelAdapterConfig{
		Manager: manager, DeviceID: "device-shutdown", SessionConfig: &swu.Config{},
		EstablishmentTimeout: time.Second,
	})
	connectDone := make(chan error, 1)
	go func() { connectDone <- adapter.Connect(context.Background()) }()
	<-manager.secondStart
	shutdownDone := make(chan struct{})
	go func() {
		adapter.Shutdown()
		close(shutdownDone)
	}()
	close(manager.releaseSecondStart)

	select {
	case err := <-connectDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Connect error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect did not stop after Shutdown")
	}
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not finish")
	}
	if starts, stops, _ := manager.snapshot(); starts != 2 || stops != 2 {
		t.Fatalf("starts=%d stops=%d, want 2/2", starts, stops)
	}
}

type retryEPDGManager struct {
	mu       sync.Mutex
	starts   int
	waits    int
	stops    int
	sessions []*swu.Session
}

func (manager *retryEPDGManager) Start(context.Context, string, *swu.Config) (*swu.Session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.starts++
	session := swu.NewSession(&swu.Config{})
	manager.sessions = append(manager.sessions, session)
	return session, nil
}

func (manager *retryEPDGManager) Wait(context.Context, string, time.Duration) (swu.SessionSnapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.waits++
	if manager.waits == 1 {
		return swu.SessionSnapshot{}, epdg.ErrEstablishmentTimeout
	}
	return swu.SessionSnapshot{Established: true}, nil
}

func (manager *retryEPDGManager) Stop(string) error {
	manager.mu.Lock()
	manager.stops++
	manager.mu.Unlock()
	return nil
}

func (manager *retryEPDGManager) snapshot() (int, int, []*swu.Session) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.starts, manager.stops, append([]*swu.Session(nil), manager.sessions...)
}

type blockingRetryEPDGManager struct {
	retryEPDGManager
	secondStart        chan struct{}
	releaseSecondStart chan struct{}
}

func newBlockingRetryEPDGManager() *blockingRetryEPDGManager {
	return &blockingRetryEPDGManager{
		secondStart: make(chan struct{}), releaseSecondStart: make(chan struct{}),
	}
}

func (manager *blockingRetryEPDGManager) Start(ctx context.Context, deviceID string, config *swu.Config) (*swu.Session, error) {
	session, err := manager.retryEPDGManager.Start(ctx, deviceID, config)
	manager.mu.Lock()
	starts := manager.starts
	manager.mu.Unlock()
	if starts == 2 {
		close(manager.secondStart)
		<-manager.releaseSecondStart
	}
	return session, err
}

func (manager *blockingRetryEPDGManager) Wait(ctx context.Context, deviceID string, timeout time.Duration) (swu.SessionSnapshot, error) {
	manager.mu.Lock()
	manager.waits++
	waits := manager.waits
	manager.mu.Unlock()
	if waits == 1 {
		return swu.SessionSnapshot{}, epdg.ErrEstablishmentTimeout
	}
	<-ctx.Done()
	return swu.SessionSnapshot{}, ctx.Err()
}
