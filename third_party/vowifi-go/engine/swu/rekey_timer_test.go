package swu

import (
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

func TestRekeyTimerRetriesOnceThenSurfacesFailure(t *testing.T) {
	session := NewSession(&Config{})
	sentinel := errors.New("transient rekey failure")
	attempts := 0
	session.startRekeyTimer(rekeyTimerSpec{
		name: "test", interval: time.Millisecond, target: &session.ikeRekeyTimer,
		retryInterval: time.Millisecond, action: func() error {
			attempts++
			return sentinel
		},
	})
	waitForRekeyTimerFailure(t, session)
	if attempts != rekeyMaxFailures {
		t.Fatalf("attempts = %d, want %d", attempts, rekeyMaxFailures)
	}
	if !errors.Is(session.TerminalError(), sentinel) {
		t.Fatalf("terminal error = %v", session.TerminalError())
	}
}

func TestChildSANotFoundFailsTimerWithoutRetry(t *testing.T) {
	session := NewSession(&Config{})
	attempts := 0
	session.startRekeyTimer(rekeyTimerSpec{
		name: "CHILD_SA", interval: time.Millisecond, target: &session.childRekeyTimer,
		retryInterval: time.Millisecond, immediateFail: isChildSANotFoundError,
		action: func() error {
			attempts++
			return &createChildSARejectError{NotifyType: ikev2.CHILD_SA_NOT_FOUND}
		},
	})
	waitForRekeyTimerFailure(t, session)
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func waitForRekeyTimerFailure(t *testing.T, session *Session) {
	t.Helper()
	select {
	case <-session.done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for rekey timer failure")
	}
	session.rekeyTimerWG.Wait()
}
