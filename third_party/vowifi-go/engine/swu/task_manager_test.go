package swu

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

// newTestTaskManager builds a TaskManager with a fast retransmit poll so tests
// don't wait 500 ms.
func newTestTaskManager(t *testing.T, send func(uint32, []byte) error, config *RetransmitConfig, window int) *TaskManager {
	t.Helper()
	testConfig := *config
	testConfig.PollInterval = 10 * time.Millisecond
	return NewRawTaskManager(context.Background(), send, &testConfig, window)
}

func TestTaskManagerRequestResponse(t *testing.T) {
	var sends int32
	tm := newTestTaskManager(t, func(id uint32, msg []byte) error {
		atomic.AddInt32(&sends, 1)
		return nil
	}, &RetransmitConfig{MaxRetries: 5, InitialDelay: time.Hour}, 4)
	defer tm.Stop()

	ch := tm.EnqueueRawRequest(1, []byte("request"))
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
	ch1 := tm.EnqueueRawRequest(1, []byte("a"))
	// Second request must queue (window full): not sent yet.
	ch2 := tm.EnqueueRawRequest(2, []byte("b"))
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

	tm.EnqueueRawRequest(1, []byte("a"))
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

	ch := tm.EnqueueRawRequest(1, []byte("a"))
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
	ch := tm.EnqueueRawRequest(1, []byte("a"))
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

func TestDefaultRetryConfigMatchesLegacyPolicy(t *testing.T) {
	config := DefaultRetryConfig()
	if config.MaxRetries != 5 || config.InitialTimeout != 4*time.Second ||
		config.MaxTimeout != 0 || config.BackoffFactor != 1.8 {
		t.Fatalf("default retry config = %+v", config)
	}
}

func TestLegacyTaskManagerSendsPacketSetAndReturnsRawResponse(t *testing.T) {
	var sent [][]byte
	tm := NewTaskManager(
		context.Background(), "device-1", &RetryConfig{InitialTimeout: time.Hour}, 5,
		func(packets [][]byte) error {
			sent = clonePacketSet(packets)
			return nil
		},
	)
	defer tm.Stop()
	packets := [][]byte{[]byte("fragment-1"), []byte("fragment-2")}
	completion := tm.EnqueueRequest(7, ikev2.CREATE_CHILD_SA, nil, packets)
	packets[0][0] = 'X'
	if len(sent) != 2 || string(sent[0]) != "fragment-1" || string(sent[1]) != "fragment-2" {
		t.Fatalf("sent packet set = %q", sent)
	}
	if !tm.HandleResponse(7, []byte("response")) {
		t.Fatal("legacy response was not matched")
	}
	if response := <-completion; string(response) != "response" {
		t.Fatalf("legacy response = %q", response)
	}
}

func TestLegacyTaskManagerTimeoutClosesCompletion(t *testing.T) {
	config := &RetryConfig{MaxRetries: 0, InitialTimeout: time.Millisecond, BackoffFactor: 1}
	tm := newTaskManager(context.Background(), "", config, 1, func([][]byte) error { return nil }, nil, time.Millisecond)
	defer tm.Stop()
	completion := tm.EnqueueRequest(1, ikev2.IKE_SA_INIT, nil, [][]byte{[]byte("request")})
	select {
	case _, ok := <-completion:
		if ok {
			t.Fatal("legacy timeout returned data instead of closing")
		}
	case <-time.After(time.Second):
		t.Fatal("legacy completion was not closed on timeout")
	}
}

func TestTaskManagerRejectsDuplicateAndPostStopRequests(t *testing.T) {
	tm := newTestTaskManager(t, func(uint32, []byte) error { return nil },
		&RetransmitConfig{MaxRetries: 5, InitialDelay: time.Hour}, 1)
	first := tm.EnqueueRawRequest(9, []byte("first"))
	duplicate := tm.EnqueueRawRequest(9, []byte("duplicate"))
	if result := <-duplicate; !errors.Is(result.Err, ErrDuplicateMessageID) {
		t.Fatalf("duplicate error = %v", result.Err)
	}
	tm.Stop()
	if result := <-first; !errors.Is(result.Err, ErrTaskManagerStopped) {
		t.Fatalf("first request stop error = %v", result.Err)
	}
	if result := <-tm.EnqueueRawRequest(10, []byte("late")); !errors.Is(result.Err, ErrTaskManagerStopped) {
		t.Fatalf("post-stop error = %v", result.Err)
	}
	tm.Stop()
}

func TestTaskManagerSurfacesLastSendFailure(t *testing.T) {
	sendErr := errors.New("socket unavailable")
	tm := newTestTaskManager(t, func(uint32, []byte) error { return sendErr },
		&RetransmitConfig{MaxRetries: 0, InitialDelay: time.Millisecond, Backoff: 1}, 1)
	defer tm.Stop()
	result := <-tm.EnqueueRawRequest(3, []byte("request"))
	if !errors.Is(result.Err, ErrTaskTimeout) || !strings.Contains(result.Err.Error(), sendErr.Error()) {
		t.Fatalf("send failure result = %v", result.Err)
	}
	if !errors.Is(result.Err, sendErr) {
		t.Fatalf("send failure does not wrap transport error: %v", result.Err)
	}
}

func TestTaskManagerCapsBackoffAndMatchesExchange(t *testing.T) {
	config := &RetryConfig{
		MaxRetries: 2, InitialTimeout: 10 * time.Millisecond,
		MaxTimeout: 15 * time.Millisecond, BackoffFactor: 10,
	}
	tm := newTaskManager(
		context.Background(), "", config, 1, func([][]byte) error { return nil }, nil, time.Hour,
	)
	defer tm.Stop()
	completion := tm.EnqueueRequest(4, ikev2.IKE_SA_INIT, nil, [][]byte{[]byte("request")})
	tm.mu.Lock()
	tm.pending[4].Deadline = time.Now().Add(-time.Millisecond)
	tm.mu.Unlock()
	tm.checkTimeouts()
	tm.mu.Lock()
	nextTimeout := tm.pending[4].NextTimeout
	tm.mu.Unlock()
	if nextTimeout != config.MaxTimeout {
		t.Fatalf("capped timeout = %s, want %s", nextTimeout, config.MaxTimeout)
	}
	if tm.handleResponseForExchange(4, ikev2.IKE_AUTH, nil) {
		t.Fatal("response with wrong exchange was matched")
	}
	if !tm.handleResponseForExchange(4, ikev2.IKE_SA_INIT, []byte("response")) {
		t.Fatal("response with matching exchange was rejected")
	}
	if response := <-completion; string(response) != "response" {
		t.Fatalf("response = %q", response)
	}
}
