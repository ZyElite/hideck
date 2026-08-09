package swu

import (
	"context"
	"testing"
	"time"
)

func TestLegacySessionManagerLifecycle(t *testing.T) {
	manager := NewSessionManager()
	if _, err := manager.Start(context.Background(), "", &Config{}); err == nil {
		t.Fatal("Start accepted an empty session ID")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := manager.Start(ctx, "device-1", &Config{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got, ok := manager.Get("device-1"); !ok || got != session {
		t.Fatal("Get did not return the managed session")
	}
	if _, err := manager.Start(ctx, "device-1", &Config{}); err == nil {
		t.Fatal("Start accepted a duplicate session ID")
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := session.WaitDoneContext(waitCtx); err != nil {
		t.Fatalf("failed managed session did not finish cleanup: %v", err)
	}
	if err := manager.Stop("device-1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, ok := manager.Get("device-1"); ok {
		t.Fatal("Stop retained the managed session")
	}
	if err := manager.Stop("device-1"); err == nil {
		t.Fatal("Stop accepted a missing session ID")
	}
}
