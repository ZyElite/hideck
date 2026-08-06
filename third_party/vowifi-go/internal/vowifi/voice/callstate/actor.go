package callstate

import (
	"context"
	"sync"
)

// Task is a unit of work executed on the actor's single goroutine.
type Task struct {
	// Fn is the work to run.
	Fn func()
}

// Actor serializes call work on a single goroutine. All call state
// transitions must be enqueued on the actor so they never race.
type Actor struct {
	mu      sync.Mutex
	queue   chan Task
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// NewActor creates a stopped actor.
func NewActor() *Actor {
	return &Actor{}
}

// Start launches the actor's worker goroutine. It is idempotent.
func (a *Actor) Start(ctx context.Context) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.queue = make(chan Task, 64)
	a.started = true
	a.wg.Add(1)
	go a.run()
}

// Stop cancels the actor and waits for the worker to exit.
func (a *Actor) Stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	a.started = false
	a.cancel()
	a.mu.Unlock()
	a.wg.Wait()
}

// Enqueue schedules fn on the actor's goroutine. It is a no-op if the
// actor is not running.
func (a *Actor) Enqueue(fn func()) {
	if a == nil || fn == nil {
		return
	}
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return
	}
	select {
	case a.queue <- Task{Fn: fn}:
	default:
		// Queue full: run synchronously to avoid dropping work.
		a.mu.Unlock()
		fn()
		return
	}
	a.mu.Unlock()
}

// QueueLen returns the number of pending tasks.
func (a *Actor) QueueLen() int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.queue == nil {
		return 0
	}
	return len(a.queue)
}

// run drains the task queue until the context is canceled.
func (a *Actor) run() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case t := <-a.queue:
			if t.Fn != nil {
				t.Fn()
			}
		}
	}
}
