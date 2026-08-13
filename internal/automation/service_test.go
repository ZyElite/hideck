package automation

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type serviceTestStore struct {
	mu        sync.Mutex
	tasks     map[uint64]Task
	nextRun   uint64
	finished  chan Run
	recovered []Run
}

func (s *serviceTestStore) SaveTask(_ context.Context, task Task) (Task, error) { return task, nil }
func (s *serviceTestStore) GetTask(_ context.Context, id uint64) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrNotFound
	}
	return task, nil
}
func (s *serviceTestStore) ListTasks(context.Context) ([]Task, error) { return nil, nil }
func (s *serviceTestStore) DeleteTask(context.Context, uint64) error  { return nil }
func (s *serviceTestStore) ClaimDueRuns(context.Context, time.Time, int) ([]Run, error) {
	return nil, nil
}
func (s *serviceTestStore) QueueRun(_ context.Context, taskID uint64, now time.Time) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRun++
	return Run{ID: s.nextRun, TaskID: taskID, DeviceID: s.tasks[taskID].DeviceID, ScheduledAt: now, Status: RunStatusQueued}, nil
}
func (s *serviceTestStore) UpdateRun(_ context.Context, run Run) error {
	if run.Status == RunStatusSuccess || run.Status == RunStatusFailed {
		s.finished <- run
	}
	return nil
}
func (s *serviceTestStore) RecoverRuns(context.Context, time.Time) ([]Run, error) {
	return s.recovered, nil
}
func (s *serviceTestStore) ListRuns(context.Context, uint64, int, int) ([]Run, int64, error) {
	return nil, 0, nil
}

type serialExecutor struct {
	active int32
	max    int32
}

func (e *serialExecutor) Execute(context.Context, Task, func(string) error) (string, error) {
	active := atomic.AddInt32(&e.active, 1)
	for {
		current := atomic.LoadInt32(&e.max)
		if active <= current || atomic.CompareAndSwapInt32(&e.max, current, active) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	atomic.AddInt32(&e.active, -1)
	return "done", nil
}

func TestServiceSerializesRunsPerDevice(t *testing.T) {
	store := &serviceTestStore{
		tasks: map[uint64]Task{
			1: {ID: 1, DeviceID: "wwan0"},
			2: {ID: 2, DeviceID: "wwan0"},
		},
		finished: make(chan Run, 2),
	}
	executor := &serialExecutor{}
	service := NewService(store, executor, Options{PollInterval: time.Hour})
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	if _, err := service.RunNow(context.Background(), 1); err != nil {
		t.Fatalf("RunNow(1): %v", err)
	}
	if _, err := service.RunNow(context.Background(), 2); err != nil {
		t.Fatalf("RunNow(2): %v", err)
	}
	for range 2 {
		select {
		case run := <-store.finished:
			if run.Status != RunStatusSuccess {
				t.Fatalf("run status = %s", run.Status)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for automatic task run")
		}
	}
	if got := atomic.LoadInt32(&executor.max); got != 1 {
		t.Fatalf("maximum concurrent executions = %d, want 1", got)
	}
}

func TestServiceStartDoesNotBlockOnRecoveredQueueCapacity(t *testing.T) {
	recovered := make([]Run, 0, deviceQueueSize+1)
	for id := 1; id <= deviceQueueSize+1; id++ {
		recovered = append(recovered, Run{ID: uint64(id), TaskID: 1, DeviceID: "wwan0", Status: RunStatusQueued})
	}
	store := &serviceTestStore{
		tasks:     map[uint64]Task{1: {ID: 1, DeviceID: "wwan0"}},
		finished:  make(chan Run, deviceQueueSize+1),
		recovered: recovered,
	}
	service := NewService(store, &serialExecutor{}, Options{PollInterval: time.Hour})
	startCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := service.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
