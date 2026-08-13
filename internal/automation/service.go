package automation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultPollInterval = 5 * time.Second
	defaultRunTimeout   = 8 * time.Minute
	deviceQueueSize     = 100
	retryDelayUnit      = 5 * time.Second
)

type Store interface {
	SaveTask(context.Context, Task) (Task, error)
	GetTask(context.Context, uint64) (Task, error)
	ListTasks(context.Context) ([]Task, error)
	DeleteTask(context.Context, uint64) error
	ClaimDueRuns(context.Context, time.Time, int) ([]Run, error)
	QueueRun(context.Context, uint64, time.Time) (Run, error)
	UpdateRun(context.Context, Run) error
	RecoverRuns(context.Context, time.Time) ([]Run, error)
	ListRuns(context.Context, uint64, int, int) ([]Run, int64, error)
}

type Executor interface {
	Execute(context.Context, Task, func(string) error) (string, error)
}

type Options struct {
	PollInterval time.Duration
	RunTimeout   time.Duration
	Now          func() time.Time
	OnError      func(error)
	Notify       func(context.Context, Task, Run) error
}

type Service struct {
	store    Store
	executor Executor
	options  Options

	mu      sync.Mutex
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
	queues  map[string]chan Run
	wg      sync.WaitGroup
}

func NewService(store Store, executor Executor, options Options) *Service {
	if options.PollInterval <= 0 {
		options.PollInterval = defaultPollInterval
	}
	if options.RunTimeout <= 0 {
		options.RunTimeout = defaultRunTimeout
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{store: store, executor: executor, options: options, queues: make(map[string]chan Run)}
}

func (s *Service) Start(parent context.Context) error {
	if s == nil || s.store == nil || s.executor == nil {
		return errors.New("automatic task service dependencies are unavailable")
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(parent)
	s.started = true
	s.queues = make(map[string]chan Run)
	s.mu.Unlock()

	recovered, err := s.store.RecoverRuns(s.ctx, s.options.Now().UTC())
	if err != nil {
		_ = s.Stop(context.Background())
		return fmt.Errorf("recover automatic task runs: %w", err)
	}
	recoveryDone := make(chan struct{})
	if len(recovered) > 0 {
		s.wg.Add(1)
		go s.enqueueRecovered(recovered, recoveryDone)
	} else {
		close(recoveryDone)
	}
	s.wg.Add(1)
	go s.scheduleLoop(recoveryDone)
	return nil
}

func (s *Service) enqueueRecovered(runs []Run, done chan<- struct{}) {
	defer s.wg.Done()
	defer close(done)
	for _, run := range runs {
		if err := s.enqueue(run); err != nil {
			if !errors.Is(err, ErrNotStarted) {
				s.report(fmt.Errorf("restore queued automatic task run %d: %w", run.ID, err))
			}
			return
		}
	}
}

func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop automatic task service: %w", ctx.Err())
	}
}

func (s *Service) SaveTask(ctx context.Context, input Task) (Task, error) {
	if s == nil || s.store == nil {
		return Task{}, errors.New("automatic task store is unavailable")
	}
	normalized, err := NormalizeTask(input, s.options.Now())
	if err != nil {
		return Task{}, fmt.Errorf("%w: %v", ErrInvalidTask, err)
	}
	return s.store.SaveTask(ctx, normalized)
}

func (s *Service) GetTask(ctx context.Context, id uint64) (Task, error) {
	if s == nil || s.store == nil {
		return Task{}, errors.New("automatic task store is unavailable")
	}
	return s.store.GetTask(ctx, id)
}

func (s *Service) ListTasks(ctx context.Context) ([]Task, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("automatic task store is unavailable")
	}
	return s.store.ListTasks(ctx)
}

func (s *Service) DeleteTask(ctx context.Context, id uint64) error {
	if s == nil || s.store == nil {
		return errors.New("automatic task store is unavailable")
	}
	return s.store.DeleteTask(ctx, id)
}

func (s *Service) RunNow(ctx context.Context, id uint64) (Run, error) {
	if !s.isStarted() {
		return Run{}, ErrNotStarted
	}
	run, err := s.store.QueueRun(ctx, id, s.options.Now().UTC())
	if err != nil {
		return Run{}, err
	}
	if err := s.enqueue(run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Service) ListRuns(ctx context.Context, taskID uint64, limit, offset int) ([]Run, int64, error) {
	if s == nil || s.store == nil {
		return nil, 0, errors.New("automatic task store is unavailable")
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListRuns(ctx, taskID, limit, offset)
}

func (s *Service) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *Service) report(err error) {
	if err != nil && s.options.OnError != nil {
		s.options.OnError(err)
	}
}
