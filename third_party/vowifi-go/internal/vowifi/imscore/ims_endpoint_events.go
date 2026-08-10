package imscore

import (
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"go.uber.org/zap"
)

const (
	defaultIMSEventSubscriptionName = "ims_event_subscription"
	defaultIMSEventQueueSize        = 64
	defaultIMSEventWorkers          = 1
	imsInviteEventQueueWait         = 3 * time.Second
)

type imsEventSubscriptionSnapshot struct {
	Name         string `json:"name"`
	Workers      int    `json:"workers"`
	QueueLen     int    `json:"queue_len"`
	QueueCap     int    `json:"queue_cap"`
	Matched      uint64 `json:"matched"`
	Enqueued     uint64 `json:"enqueued"`
	QueueFull    uint64 `json:"queue_full"`
	Unsubscribed bool   `json:"unsubscribed"`
}

type imsEventSubscriber struct {
	id           uint64
	device       string
	spec         imsendpoint.EventSubscription
	handler      func(imsendpoint.Event)
	queue        chan imsendpoint.Event
	done         sync.WaitGroup
	queueMu      sync.RWMutex
	matched      atomic.Uint64
	enqueued     atomic.Uint64
	queueFull    atomic.Uint64
	unsubscribed atomic.Bool
}

type imsEventBus struct {
	device        string
	mu            sync.RWMutex
	nextID        uint64
	closed        bool
	subscribers   map[uint64]*imsEventSubscriber
	publishCounts map[string]uint64
}

type imsEventPublishReceipt struct {
	matched       int
	enqueued      int
	queueFull     int
	subscriptions map[string]int
}

func (r imsEventPublishReceipt) enqueuedFor(name string) bool {
	return r.subscriptions[strings.TrimSpace(name)] > 0
}

func newIMSEventBus(device string) *imsEventBus {
	return &imsEventBus{
		device:        strings.TrimSpace(device),
		subscribers:   make(map[uint64]*imsEventSubscriber),
		publishCounts: make(map[string]uint64),
	}
}

func normalizeIMSEventSubscription(spec imsendpoint.EventSubscription) imsendpoint.EventSubscription {
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		spec.Name = defaultIMSEventSubscriptionName
	}
	if spec.QueueSize < 1 {
		spec.QueueSize = defaultIMSEventQueueSize
	}
	if spec.Workers < 1 {
		spec.Workers = defaultIMSEventWorkers
	}
	return spec
}

func imsEventCounterKey(event imsendpoint.Event) string {
	kind := strings.TrimSpace(event.Kind)
	if kind == "" {
		kind = "unknown"
	}
	method := strings.ToUpper(strings.TrimSpace(event.Method))
	if method == "" {
		method = strings.ToUpper(strings.TrimSpace(event.CSeqMethod))
	}
	if method == "" {
		method = "UNKNOWN"
	}
	return kind + ":" + method
}

func (b *imsEventBus) subscribe(
	spec imsendpoint.EventSubscription,
	handler func(imsendpoint.Event),
) func() {
	if b == nil || handler == nil {
		return func() {}
	}
	spec = normalizeIMSEventSubscription(spec)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		logging.WarnRate("ims_event_bus_closed:"+b.device, 30*time.Second,
			"IMS event subscription rejected after close", "device", b.device, "name", spec.Name)
		return func() {}
	}
	b.nextID++
	subscriber := &imsEventSubscriber{
		id: b.nextID, device: b.device, spec: spec, handler: handler,
		queue: make(chan imsendpoint.Event, spec.QueueSize),
	}
	b.subscribers[subscriber.id] = subscriber
	b.mu.Unlock()
	for worker := 0; worker < spec.Workers; worker++ {
		subscriber.done.Add(1)
		go subscriber.runWorker(worker)
	}
	var once sync.Once
	return func() { once.Do(func() { b.unsubscribe(subscriber.id) }) }
}

func (b *imsEventBus) unsubscribe(id uint64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	subscriber := b.subscribers[id]
	delete(b.subscribers, id)
	if subscriber != nil {
		subscriber.stop()
	}
	b.mu.Unlock()
	if subscriber != nil {
		subscriber.done.Wait()
	}
}

func (b *imsEventBus) publish(event imsendpoint.Event) (matched, enqueued, queueFull int) {
	receipt := b.publishWithReceipt(event)
	return receipt.matched, receipt.enqueued, receipt.queueFull
}

func (b *imsEventBus) publishWithReceipt(event imsendpoint.Event) imsEventPublishReceipt {
	if b == nil {
		return imsEventPublishReceipt{}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return imsEventPublishReceipt{}
	}
	b.publishCounts[imsEventCounterKey(event)]++
	subscribers := make([]*imsEventSubscriber, 0, len(b.subscribers))
	for _, subscriber := range b.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	b.mu.Unlock()
	receipt := imsEventPublishReceipt{subscriptions: make(map[string]int)}
	for _, subscriber := range subscribers {
		if !subscriber.matches(event) {
			continue
		}
		receipt.matched++
		if subscriber.enqueue(event) {
			receipt.enqueued++
			receipt.subscriptions[subscriber.spec.Name]++
			continue
		}
		receipt.queueFull++
	}
	return receipt
}

func (b *imsEventBus) close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	subscribers := make([]*imsEventSubscriber, 0, len(b.subscribers))
	for id, subscriber := range b.subscribers {
		delete(b.subscribers, id)
		subscriber.stop()
		subscribers = append(subscribers, subscriber)
	}
	b.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber.done.Wait()
	}
}

func (b *imsEventBus) snapshot() (map[string]uint64, []imsEventSubscriptionSnapshot) {
	if b == nil {
		return map[string]uint64{}, nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	counts := make(map[string]uint64, len(b.publishCounts))
	for key, count := range b.publishCounts {
		counts[key] = count
	}
	subscriptions := make([]imsEventSubscriptionSnapshot, 0, len(b.subscribers))
	for _, subscriber := range b.subscribers {
		subscriptions = append(subscriptions, subscriber.snapshot())
	}
	return counts, subscriptions
}

func (b *imsEventBus) statusSnapshot() map[string]interface{} {
	counts, subscriptions := b.snapshot()
	return map[string]interface{}{
		"publish_counts": counts,
		"subscriptions":  subscriptions,
	}
}

func (s *imsEventSubscriber) matches(event imsendpoint.Event) (matched bool) {
	if s == nil || s.unsubscribed.Load() {
		return false
	}
	if s.spec.Match == nil {
		return true
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			zap.S().Errorw("IMS event matcher panic", "device", s.device,
				"subscriber", s.spec.Name, "panic", recovered)
			matched = false
		}
	}()
	return s.spec.Match(event)
}

func (s *imsEventSubscriber) enqueue(event imsendpoint.Event) bool {
	s.matched.Add(1)
	s.queueMu.RLock()
	defer s.queueMu.RUnlock()
	if s.unsubscribed.Load() {
		return false
	}
	select {
	case s.queue <- event:
		s.enqueued.Add(1)
		return true
	default:
	}
	if isInboundInviteEvent(event) {
		timer := time.NewTimer(imsInviteEventQueueWait)
		defer timer.Stop()
		select {
		case s.queue <- event:
			s.enqueued.Add(1)
			return true
		case <-timer.C:
		}
	}
	s.queueFull.Add(1)
	logging.WarnRate("ims_event_bus_queue_full:"+s.device+":"+s.spec.Name, 10*time.Second,
		"IMS event subscriber queue full", "device", s.device, "subscriber", s.spec.Name,
		"method", event.Method, "kind", event.Kind, "queue_len", len(s.queue), "queue_cap", cap(s.queue))
	return false
}

func isInboundInviteEvent(event imsendpoint.Event) bool {
	return strings.EqualFold(strings.TrimSpace(event.Kind), "request") &&
		strings.EqualFold(strings.TrimSpace(event.Method), "INVITE")
}

func (s *imsEventSubscriber) stop() {
	if s == nil || !s.unsubscribed.CompareAndSwap(false, true) {
		return
	}
	s.queueMu.Lock()
	close(s.queue)
	s.queueMu.Unlock()
}

func (s *imsEventSubscriber) snapshot() imsEventSubscriptionSnapshot {
	return imsEventSubscriptionSnapshot{
		Name: s.spec.Name, Workers: s.spec.Workers, QueueLen: len(s.queue), QueueCap: cap(s.queue),
		Matched: s.matched.Load(), Enqueued: s.enqueued.Load(), QueueFull: s.queueFull.Load(),
		Unsubscribed: s.unsubscribed.Load(),
	}
}

func (s *imsEventSubscriber) runWorker(worker int) {
	defer s.done.Done()
	for event := range s.queue {
		s.invokeHandler(worker, event)
	}
}

func (s *imsEventSubscriber) invokeHandler(worker int, event imsendpoint.Event) {
	defer func() {
		if recovered := recover(); recovered != nil {
			zap.S().Errorw("IMS event handler panic", "device", s.device,
				"subscriber", s.spec.Name, "worker", worker, "panic", recovered,
				"stack", string(debug.Stack()))
		}
	}()
	s.handler(event)
}
