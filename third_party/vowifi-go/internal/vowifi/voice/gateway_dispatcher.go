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
	if previous := g.entryWorkers[deviceID]; previous != nil {
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
	}
	g.entryWorkers[deviceID] = worker
	go g.runEntryWorker(ctx, worker)
}

func (g *Gateway) runEntryWorker(ctx context.Context, worker *gatewayEntryWorker) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-worker.ch:
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
