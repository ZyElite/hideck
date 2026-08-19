package runtimecore

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
)

func TestStartAndWaitEPDGRetriesTimeoutWithFreshSession(t *testing.T) {
	manager := &retrySessionManager{}
	session, snapshot, err := StartAndWaitEPDG(
		context.Background(), "device-retry", "trace-retry", &swu.Config{}, manager,
	)
	if err != nil {
		t.Fatalf("StartAndWaitEPDG: %v", err)
	}
	if manager.starts != 2 || manager.waits != 2 || manager.stops != 1 {
		t.Fatalf("starts=%d waits=%d stops=%d, want 2/2/1",
			manager.starts, manager.waits, manager.stops)
	}
	if manager.sessions[0] == manager.sessions[1] || session != manager.sessions[1] {
		t.Fatal("timeout retry did not return a fresh session")
	}
	if !snapshot.Established {
		t.Fatal("successful retry snapshot is not established")
	}
}

type retrySessionManager struct {
	starts   int
	waits    int
	stops    int
	sessions []*swu.Session
}

func (manager *retrySessionManager) Start(context.Context, string, *swu.Config) (*swu.Session, error) {
	manager.starts++
	session := swu.NewSession(&swu.Config{})
	manager.sessions = append(manager.sessions, session)
	return session, nil
}

func (manager *retrySessionManager) Wait(context.Context, string, time.Duration) (swu.SessionSnapshot, error) {
	manager.waits++
	if manager.waits == 1 {
		return swu.SessionSnapshot{}, epdg.ErrEstablishmentTimeout
	}
	return swu.SessionSnapshot{Established: true}, nil
}

func (manager *retrySessionManager) Stop(string) error {
	manager.stops++
	return nil
}
