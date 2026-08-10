package voice

import (
	"context"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const gatewayEntryQueueSize = 256

func (g *Gateway) startEntryWorkerLocked(deviceID string, agent *Agent) {
	if !g.running || agent == nil {
		return
	}
	previous := g.entryWorkers[deviceID]
	if previous != nil {
		previous.cancel()
		delete(g.entryWorkers, deviceID)
	}
	parent := g.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &gatewayEntryWorker{
		deviceID: deviceID, agent: agent,
		ch: make(chan gatewayEntryTask, gatewayEntryQueueSize), cancel: cancel,
		done: make(chan struct{}), previous: previous,
	}
	g.entryWorkers[deviceID] = worker
	go g.runEntryWorker(ctx, worker)
}

func stopGatewayEntryWorkerChain(worker *gatewayEntryWorker) {
	for current := worker; current != nil; current = current.previous {
		current.cancel()
	}
	for current := worker; current != nil; current = current.previous {
		<-current.done
	}
}

func (g *Gateway) runEntryWorker(ctx context.Context, worker *gatewayEntryWorker) {
	defer close(worker.done)
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-worker.ch:
			if ctx.Err() != nil {
				return
			}
			if task.fn == nil {
				continue
			}
			logging.RunDebug("voice gateway task", "device", worker.deviceID,
				"task", task.name, "queue_wait_ms", time.Since(task.enqueuedAt).Milliseconds())
			task.fn(worker.agent)
		}
	}
}

func (g *Gateway) enqueueDeviceTask(deviceID, name string, fn func(*Agent)) bool {
	if g == nil || fn == nil {
		return false
	}
	g.mu.RLock()
	worker := g.entryWorkers[deviceID]
	running := g.running
	g.mu.RUnlock()
	if !running || worker == nil {
		return false
	}
	task := gatewayEntryTask{name: name, enqueuedAt: time.Now(), fn: fn}
	select {
	case worker.ch <- task:
		return true
	default:
		logging.WarnRate("voice-gateway-queue-full:"+deviceID, voiceActorEventLogInterval,
			"voice gateway device queue is full", "device", deviceID, "task", name)
		return false
	}
}
