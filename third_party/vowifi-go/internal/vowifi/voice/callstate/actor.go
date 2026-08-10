package callstate

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const (
	DefaultQueueCapacity = 128
	actorLogRateInterval = 10 * time.Second
)

// Task is one named unit of call work retained by the actor queue.
type Task struct {
	Name       string
	EnqueuedAt time.Time
	Fn         func()
}

// ActorConfig supplies the immutable identity and capacity of an Actor.
type ActorConfig struct {
	DeviceID      string
	TraceID       string
	QueueCapacity int
}

// Actor serializes call work on one goroutine.
type Actor struct {
	deviceID string
	traceID  string
	queueCap int

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan Task
	done   sync.WaitGroup

	lifecycle sync.Mutex
}

// NewActor retains the current zero-argument constructor with the original
// per-call queue capacity.
func NewActor() *Actor {
	return NewActorWithConfig(ActorConfig{})
}

// NewActorWithConfig creates a stopped actor with call logging context.
func NewActorWithConfig(config ActorConfig) *Actor {
	queueCapacity := config.QueueCapacity
	if queueCapacity == 0 {
		queueCapacity = DefaultQueueCapacity
	}
	return &Actor{
		deviceID: config.DeviceID,
		traceID:  config.TraceID,
		queueCap: queueCapacity,
	}
}

// Start launches the worker once. Stop must complete before the actor can be
// started again.
func (a *Actor) Start(ctx context.Context) {
	if a == nil {
		return
	}
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()

	a.mu.Lock()
	if a.queue != nil {
		a.mu.Unlock()
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	queue := make(chan Task, a.queueCap)
	a.ctx, a.cancel, a.queue = workerCtx, cancel, queue
	a.done.Add(1)
	a.mu.Unlock()
	go a.run(workerCtx, queue)
}

// Stop cancels the worker, clears the active queue, and waits for exit.
func (a *Actor) Stop() {
	if a == nil {
		return
	}
	a.lifecycle.Lock()
	defer a.lifecycle.Unlock()

	a.mu.Lock()
	cancel := a.cancel
	a.ctx, a.cancel, a.queue = nil, nil, nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.done.Wait()
}

// Enqueue submits named work without blocking. It returns false when the
// actor is stopped, canceled, or full; work is never run on the caller.
func (a *Actor) Enqueue(name string, fn func()) bool {
	if a == nil || fn == nil {
		return false
	}
	ctx, queue := a.snapshot()
	if ctx == nil || queue == nil || ctx.Err() != nil {
		return false
	}
	task := Task{Name: name, EnqueuedAt: time.Now(), Fn: fn}
	select {
	case queue <- task:
		return true
	default:
		a.logQueueFull(name, cap(queue))
		return false
	}
}

// QueueLen returns the number of queued tasks not yet accepted by the worker.
func (a *Actor) QueueLen() int {
	if a == nil {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.queue == nil {
		return 0
	}
	return len(a.queue)
}

func (a *Actor) snapshot() (context.Context, chan Task) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ctx, a.queue
}

func (a *Actor) run(ctx context.Context, queue <-chan Task) {
	defer a.done.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-queue:
			a.runTask(task)
		}
	}
}

func (a *Actor) runTask(task Task) {
	if task.Fn == nil {
		return
	}
	waitTime := time.Since(task.EnqueuedAt).Milliseconds()
	logging.RunDebug("Voice call-actor 任务执行",
		"trace_id", a.traceID, "device", a.deviceID, "task", task.Name,
		"queue_wait_ms", waitTime, "goroutine_inflight", runtime.NumGoroutine())
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("Voice call-actor 任务异常",
				"trace_id", a.traceID, "device", a.deviceID,
				"task", task.Name, "panic", recovered)
		}
	}()
	task.Fn()
}

func (a *Actor) logQueueFull(taskName string, queueCapacity int) {
	key := "voice_call_actor_queue_full:" + a.deviceID + ":" + taskName
	logging.WarnRate(key, actorLogRateInterval, "Voice call-actor 入队失败：队列已满",
		"trace_id", a.traceID, "device", a.deviceID, "task", taskName,
		"queue_cap", queueCapacity, "goroutine_inflight", runtime.NumGoroutine())
}
