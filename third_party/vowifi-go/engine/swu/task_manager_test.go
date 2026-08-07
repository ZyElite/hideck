package swu

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestTaskManager builds a TaskManager with a fast retransmit poll so tests
// don't wait 500 ms.
func newTestTaskManager(t *testing.T, send func(uint32, []byte) error, config *RetransmitConfig, window int) *TaskManager {
	t.Helper()
	tm := NewTaskManager(context.Background(), send, config, window)
	tm.tickInterval = 10 * time.Millisecond
	return tm
}

func TestTaskManagerRequestResponse(t *testing.T) {
	var sends int32
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}, &RetransmitConfig{MaxRetries: 5, InitialDelay: time.Hour}, 4)
	defer tm.Stop()

	ch := tm.EnqueueRequest(1, []byte("request"))
	// The request is sent immediately.
	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Errorf("sends = %d, want 1", got)
	}
	// Deliver the matching response.
	if !tm.HandleResponse(1, []byte("response")) {
		t.Fatal("HandleResponse reported no matching task")
	}
	select {
	case r := <-ch:
		if string(r.Message) != "response" {
			t.Errorf("response = %q", r.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("no response received")
	}
}

func TestTaskManagerWindowing(t *testing.T) {
	var mu sync.Mutex
	sentIDs := map[uint32]int{}
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error {
		mu.Lock()
		sentIDs[id]++
		mu.Unlock()
		return nil
	}, &RetransmitConfig{MaxRetries: 5, InitialDelay: time.Hour}, 1)
	defer tm.Stop()

	// First request fills the window of 1.
	ch1 := tm.EnqueueRequest(1, []byte("a"))
	// Second request must queue (window full): not sent yet.
	ch2 := tm.EnqueueRequest(2, []byte("b"))
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if sentIDs[2] != 0 {
		t.Errorf("queued request 2 sent before window opened: %d", sentIDs[2])
	}
	mu.Unlock()

	// Complete request 1; request 2 should now be activated.
	tm.HandleResponse(1, []byte("r1"))
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("request 1 not completed")
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	if sentIDs[2] == 0 {
		t.Error("queued request 2 was not sent after window opened")
	}
	mu.Unlock()
	tm.HandleResponse(2, []byte("r2"))
	select {
	case <-ch2:
	case <-time.After(time.Second):
		t.Fatal("request 2 not completed")
	}
}

func TestTaskManagerRetransmit(t *testing.T) {
	var sends int32
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}, &RetransmitConfig{MaxRetries: 3, InitialDelay: 5 * time.Millisecond, Backoff: 1.0}, 4)
	defer tm.Stop()

	tm.EnqueueRequest(1, []byte("a"))
	// Wait long enough for the retransmit poll to fire several times.
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&sends); got < 2 {
		t.Errorf("sends = %d, expected at least one retransmission", got)
	}
}

func TestTaskManagerTimeout(t *testing.T) {
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error { return nil },
		&RetransmitConfig{MaxRetries: 1, InitialDelay: 5 * time.Millisecond, Backoff: 1.0}, 4)
	defer tm.Stop()

	ch := tm.EnqueueRequest(1, []byte("a"))
	select {
	case r := <-ch:
		if !errors.Is(r.Err, ErrTaskTimeout) {
			t.Errorf("err = %v, want ErrTaskTimeout", r.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not time out")
	}
}

func TestTaskManagerStopCancelsPending(t *testing.T) {
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error { return nil },
		&RetransmitConfig{MaxRetries: 5, InitialDelay: time.Hour}, 1)
	ch := tm.EnqueueRequest(1, []byte("a"))
	tm.Stop()
	select {
	case r := <-ch:
		if !errors.Is(r.Err, ErrTaskManagerStopped) {
			t.Errorf("err = %v, want ErrTaskManagerStopped", r.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending request not cancelled on Stop")
	}
}

func TestTaskManagerHandleResponseNoMatch(t *testing.T) {
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error { return nil },
		&RetransmitConfig{MaxRetries: 5, InitialDelay: time.Hour}, 4)
	defer tm.Stop()
	if tm.HandleResponse(999, nil) {
		t.Error("HandleResponse should report no match for unknown id")
	}
}
