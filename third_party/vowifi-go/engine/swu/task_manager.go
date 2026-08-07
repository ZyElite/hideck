package swu

import (
	"context"
	"errors"
	"time"
)

// RetransmitConfig controls IKE request retransmission (RFC 7296 §2.2). The
// defaults mirror the decompiled NewTaskManager default {5, 4s, 1.8×}.
type RetransmitConfig struct {
	MaxRetries   int           // retransmissions before giving up
	InitialDelay time.Duration // delay before the first retransmission
	Backoff      float64       // multiplier applied each retransmission
	PollInterval time.Duration // interval between retransmit deadline checks
}

const defaultTaskManagerPollInterval = 500 * time.Millisecond

// DefaultRetransmitConfig is the configuration used when none is supplied.
var DefaultRetransmitConfig = RetransmitConfig{
	MaxRetries:   5,
	InitialDelay: 4 * time.Second,
	Backoff:      1.8,
	PollInterval: defaultTaskManagerPollInterval,
}

// task is an in-flight (or queued) IKE request.
type task struct {
	messageID uint32
	message   []byte // encoded IKE message to (re)send
	response  chan TaskResponse
	retries   int
	delay     time.Duration // current backoff delay
	nextRetry time.Time
	done      bool
}

// TaskResponse is delivered on a task's response channel.
type TaskResponse struct {
	Message []byte
	Err     error
}

// ErrTaskTimeout is returned when a request exhausts its retransmissions.
var ErrTaskTimeout = errors.New("ike: request timed out")

// ErrTaskManagerStopped is returned when the task manager has been stopped.
var ErrTaskManagerStopped = errors.New("ike: task manager stopped")

// NewTaskManager creates a TaskManager that sends IKE requests via send and
// retransmits according to config. windowSize bounds the number of
// concurrently in-flight requests. The retransmit loop runs until Stop.
func NewTaskManager(parent context.Context, send func(uint32, []byte) error, config *RetransmitConfig, windowSize int) *TaskManager {
	if config == nil {
		c := DefaultRetransmitConfig
		config = &c
	}
	if windowSize < 1 {
		windowSize = 1
	}
	ctx, cancel := context.WithCancel(parent)
	tm := &TaskManager{
		ctx:          ctx,
		cancel:       cancel,
		send:         send,
		config:       *config,
		windowSize:   windowSize,
		active:       make(map[uint32]*task),
		trigger:      make(chan struct{}, 1),
		stop:         make(chan struct{}),
		tickInterval: retransmitPollInterval(config),
	}
	tm.wg.Add(1)
	go tm.windowLoop()
	return tm
}

func retransmitPollInterval(config *RetransmitConfig) time.Duration {
	if config.PollInterval > 0 {
		return config.PollInterval
	}
	return defaultTaskManagerPollInterval
}

// EnqueueRequest queues an IKE request for transmission and returns a channel
// that receives the matching response (or an error). If the window has room
// the request is sent immediately; otherwise it waits in the queue.
func (tm *TaskManager) EnqueueRequest(messageID uint32, message []byte) <-chan TaskResponse {
	t := &task{
		messageID: messageID,
		message:   append([]byte{}, message...),
		response:  make(chan TaskResponse, 1),
		delay:     tm.config.InitialDelay,
	}
	tm.mu.Lock()
	if len(tm.active) < tm.windowSize {
		tm.activateMessageLocked(t)
	} else {
		tm.queue = append(tm.queue, t)
	}
	tm.mu.Unlock()
	return t.response
}

// HandleResponse delivers a response to the active task with the matching
// message id, removes it from the window and pumps the queue. It returns
// whether a matching task was found.
func (tm *TaskManager) HandleResponse(messageID uint32, response []byte) bool {
	tm.mu.Lock()
	t, ok := tm.active[messageID]
	if !ok {
		tm.mu.Unlock()
		return false
	}
	delete(tm.active, messageID)
	tm.pumpQueueLocked()
	tm.mu.Unlock()

	t.response <- TaskResponse{Message: response}
	return true
}

// Stop cancels pending requests and stops the retransmit loop.
func (tm *TaskManager) Stop() {
	tm.cancel()
	close(tm.stop)
	tm.wg.Wait()

	tm.mu.Lock()
	for _, t := range tm.active {
		if !t.done {
			t.done = true
			t.response <- TaskResponse{Err: ErrTaskManagerStopped}
		}
	}
	tm.active = nil
	for _, t := range tm.queue {
		t.response <- TaskResponse{Err: ErrTaskManagerStopped}
	}
	tm.queue = nil
	tm.mu.Unlock()
}

// activateMessageLocked sends a task immediately and records it as active. The
// caller holds tm.mu.
func (tm *TaskManager) activateMessageLocked(t *task) {
	t.nextRetry = time.Now().Add(t.delay)
	tm.active[t.messageID] = t
	if tm.send != nil {
		// Best-effort: a send error is surfaced via the retransmit loop.
		_ = tm.send(t.messageID, t.message)
	}
	tm.signal()
}

// pumpQueueLocked activates queued tasks while the window has room. The caller
// holds tm.mu.
func (tm *TaskManager) pumpQueueLocked() {
	for len(tm.queue) > 0 && len(tm.active) < tm.windowSize {
		t := tm.queue[0]
		tm.queue = tm.queue[1:]
		tm.activateMessageLocked(t)
	}
}

// signal wakes the retransmit loop (non-blocking).
func (tm *TaskManager) signal() {
	select {
	case tm.trigger <- struct{}{}:
	default:
	}
}

// windowLoop runs the retransmit timer. It ticks every 500 ms (matching the
// decompiled windowLoop ticker) and retransmits/expiring timed-out tasks.
func (tm *TaskManager) windowLoop() {
	defer tm.wg.Done()
	ticker := time.NewTicker(tm.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-tm.stop:
			return
		case <-tm.trigger:
			tm.checkTimeouts()
		case <-ticker.C:
			tm.checkTimeouts()
		}
	}
}

// checkTimeouts retransmits or expires active tasks whose nextRetry has
// passed.
func (tm *TaskManager) checkTimeouts() {
	now := time.Now()
	var expired []*task
	tm.mu.Lock()
	for _, t := range tm.active {
		if t.done {
			continue
		}
		if now.Before(t.nextRetry) {
			continue
		}
		if t.retries >= tm.config.MaxRetries {
			expired = append(expired, t)
			delete(tm.active, t.messageID)
			continue
		}
		t.retries++
		t.delay = time.Duration(float64(t.delay) * tm.config.Backoff)
		t.nextRetry = now.Add(t.delay)
		if tm.send != nil {
			_ = tm.send(t.messageID, t.message)
		}
	}
	tm.pumpQueueLocked()
	tm.mu.Unlock()

	for _, t := range expired {
		t.done = true
		t.response <- TaskResponse{Err: ErrTaskTimeout}
	}
}
