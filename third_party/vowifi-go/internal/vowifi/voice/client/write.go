package client

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
)

const defaultWriteFlow = "client_request"

// WriteRequest serializes direct client writes through the bounded worker
// pool and returns the actual sipgo write result to the caller.
func (b *Bridge) WriteRequest(ctx context.Context, flow string, req *sip.Request) error {
	if req == nil {
		return errSIPRequestEmpty
	}
	if b == nil || b.client == nil {
		return errClientUninitialized
	}
	flow = normalizedFlow(flow, defaultWriteFlow)
	ctx, writeCh := b.writeContext(ctx)
	if writeCh == nil {
		return errWriteQueueUninitialized
	}
	task := writeTask{flow: flow, req: req, enqueuedAt: time.Now(), done: make(chan error, 1)}
	select {
	case writeCh <- task:
	case <-ctx.Done():
		return ctx.Err()
	default:
		logging.WarnRate("voice_client_write_queue_full:"+b.deviceID+":"+flow, 10*time.Second,
			"voice client write queue full", "device", b.deviceID, "flow", flow,
			"queue_cap", cap(writeCh), "line", req.StartLine())
		return ErrVoiceClientWriteQueueFull
	}
	return b.waitWriteResult(ctx, task)
}

func (b *Bridge) writeContext(ctx context.Context) (context.Context, chan writeTask) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx == nil {
		ctx = b.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx, b.writeCh
}

func (b *Bridge) waitWriteResult(ctx context.Context, task writeTask) error {
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	select {
	case err := <-task.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		logging.WarnRate("voice_client_send_timeout:"+b.deviceID+":"+task.flow, 10*time.Second,
			"voice client send timeout", "device", b.deviceID, "flow", task.flow,
			"line", task.req.StartLine())
		return fmt.Errorf("%w: flow=%s line=%s", ErrVoiceClientSendTimeout, task.flow, task.req.StartLine())
	}
}

func (b *Bridge) runWriteWorker(ctx context.Context, workerID int, writeCh <-chan writeTask) {
	defer b.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-writeCh:
			b.executeWriteTask(workerID, task)
		}
	}
}

func (b *Bridge) executeWriteTask(workerID int, task writeTask) {
	if task.req == nil {
		nonBlockingResult(task.done, errWriteRequestEmpty)
		return
	}
	startedAt := time.Now()
	err := b.client.WriteRequest(task.req)
	if err == nil {
		logging.RunDebug("voice client SIP request sent", "device", b.deviceID,
			"flow", task.flow, "worker", workerID, "line", task.req.StartLine(),
			"queue_wait_ms", time.Since(task.enqueuedAt).Milliseconds(),
			"write_ms", time.Since(startedAt).Milliseconds())
	}
	nonBlockingResult(task.done, err)
}

func nonBlockingResult(done chan error, err error) {
	if done == nil {
		return
	}
	select {
	case done <- err:
	default:
	}
}

func normalizedFlow(flow, fallback string) string {
	if strings.TrimSpace(flow) == "" {
		return fallback
	}
	return flow
}
