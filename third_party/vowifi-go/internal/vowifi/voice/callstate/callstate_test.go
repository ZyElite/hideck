package callstate

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestStateString(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateIdle, "Idle"},
		{StateDialing, "Dialing"},
		{StateAlerting, "Alerting"},
		{StateConnecting, "Connecting"},
		{StateConnected, "Connected"},
		{StateDisconnected, "Disconnected"},
		{StateFailed, "Failed"},
		{StateEnded, "Ended"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", int(c.s), got, c.want)
		}
	}
}

func TestCanTransition(t *testing.T) {
	// Valid forward transitions.
	valid := [][2]State{
		{StateIdle, StateDialing},
		{StateIdle, StateAlerting},
		{StateDialing, StateAlerting},
		{StateDialing, StateConnecting},
		{StateAlerting, StateConnecting},
		{StateConnecting, StateConnected},
		{StateConnected, StateConnecting}, // re-INVITE media renegotiation
		{StateConnected, StateDisconnected},
		{StateDisconnected, StateEnded},
		{StateFailed, StateEnded},
	}
	for _, v := range valid {
		if !CanTransition(v[0], v[1]) {
			t.Errorf("CanTransition(%s, %s) = false, want true", v[0], v[1])
		}
	}
	// Invalid: backwards and skipping.
	invalid := [][2]State{
		{StateDialing, StateIdle},
		{StateIdle, StateConnected},
		{StateEnded, StateIdle},
		{StateAlerting, StateConnected},
	}
	for _, v := range invalid {
		if CanTransition(v[0], v[1]) {
			t.Errorf("CanTransition(%s, %s) = true, want false", v[0], v[1])
		}
	}
}

func TestIsTerminal(t *testing.T) {
	if IsTerminal(StateIdle) {
		t.Error("Idle should not be terminal")
	}
	if !IsTerminal(StateEnded) {
		t.Error("Ended should be terminal")
	}
}

func TestActorLifecycle(t *testing.T) {
	a := NewActor()
	a.Start(context.Background())
	defer a.Stop()

	var ran atomic.Int32
	done := make(chan struct{})
	a.Enqueue(func() {
		ran.Add(1)
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not run")
	}
	if ran.Load() != 1 {
		t.Errorf("ran = %d, want 1", ran.Load())
	}
}

func TestActorQueueLen(t *testing.T) {
	a := NewActor()
	a.Start(context.Background())
	defer a.Stop()
	for i := 0; i < 5; i++ {
		a.Enqueue(func() {})
	}
	// Tasks may drain concurrently; just verify it does not panic and
	// eventually returns to zero.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.QueueLen() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("queue did not drain, len = %d", a.QueueLen())
}

func TestActorStopIdempotent(t *testing.T) {
	a := NewActor()
	a.Stop() // stop before start: no-op
	a.Start(context.Background())
	a.Stop()
	a.Stop() // double stop: no-op
}

func TestDirectionString(t *testing.T) {
	if DirectionOutbound.String() != "outbound" {
		t.Errorf("outbound = %q", DirectionOutbound.String())
	}
	if DirectionInbound.String() != "inbound" {
		t.Errorf("inbound = %q", DirectionInbound.String())
	}
}
