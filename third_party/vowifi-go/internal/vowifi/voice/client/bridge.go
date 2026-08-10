// Package client implements the local SIP client bridge used by the voice
// agent to forward requests to the LAN-side voice client.
package client

import (
	"context"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

const (
	writeWorkerCount = 4
	writeQueueSize   = 256
	writeTimeout     = 2 * time.Second
)

// Bridge is the client-facing structured SIP bridge. The field order is the
// v1.5.5 layout recovered from the original binary.
type Bridge struct {
	deviceID string
	adapter  voiceclient.Adapter
	client   *sipgo.Client
	ua       *sipgo.UserAgent
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	writeCh  chan writeTask
	wg       sync.WaitGroup
}

type writeTask struct {
	flow       string
	req        *sip.Request
	enqueuedAt time.Time
	done       chan error
}

// NewBridge constructs the v1.5.5 local voice bridge.
func NewBridge(deviceID string, adapter voiceclient.Adapter) *Bridge {
	bridge := &Bridge{deviceID: deviceID, adapter: adapter}
	if adapter != nil {
		bridge.client = adapter.GetClient()
		bridge.ua = adapter.GetUA()
	}
	return bridge
}

// Start launches the fixed-size client writer pool. Repeated starts are
// idempotent until Stop has completed.
func (b *Bridge) Start(ctx context.Context) {
	if b == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	if b.writeCh != nil {
		b.mu.Unlock()
		return
	}
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.writeCh = make(chan writeTask, writeQueueSize)
	workerCtx, writeCh := b.ctx, b.writeCh
	for workerID := 0; workerID < writeWorkerCount; workerID++ {
		b.wg.Add(1)
		go b.runWriteWorker(workerCtx, workerID, writeCh)
	}
	b.mu.Unlock()
}

// Stop cancels all writer workers and waits until they have released their
// references before making the bridge restartable.
func (b *Bridge) Stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	b.wg.Wait()
	b.mu.Lock()
	b.writeCh = nil
	b.ctx = nil
	b.cancel = nil
	b.mu.Unlock()
}

// Contact delegates contact selection to the injected local client adapter.
// The original phone-number argument is retained although v1.5.5 does not use
// it when resolving the per-device contact.
func (b *Bridge) Contact(_ []string) (string, string, string, error) {
	if b == nil || b.adapter == nil {
		return "", "", "", errAdapterUninitialized
	}
	return b.adapter.GetClientContact(b.deviceID)
}

// SendPush delegates the original four-field notification to the adapter.
func (b *Bridge) SendPush(title, body, category, callID string) error {
	if b == nil || b.adapter == nil {
		return errAdapterUninitialized
	}
	return b.adapter.SendPushNotification(title, body, category, callID)
}
