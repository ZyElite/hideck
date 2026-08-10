package imscore

import (
	"sync"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

// runtimeEventSubscriber receives host-facing runtime events.
type runtimeEventSubscriber interface {
	OnIMSEvent(ev events.Event)
}

// EventBus is the additive host-facing event bus used by runtime consumers.
type EventBus = runtimeEventBus

type runtimeEventBus struct {
	mu          sync.RWMutex
	subscribers []runtimeEventSubscriber
}

func newRuntimeEventBus() *runtimeEventBus {
	return &runtimeEventBus{}
}

// NewEventBus creates an exported event bus.
func NewEventBus() *EventBus {
	return newRuntimeEventBus()
}

// Subscribe registers a subscriber.
func (b *runtimeEventBus) Subscribe(sub runtimeEventSubscriber) {
	if b == nil || sub == nil {
		return
	}
	b.mu.Lock()
	b.subscribers = append(b.subscribers, sub)
	b.mu.Unlock()
}

// Unsubscribe removes a subscriber.
func (b *runtimeEventBus) Unsubscribe(sub runtimeEventSubscriber) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subscribers {
		if s == sub {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			return
		}
	}
}

// Publish delivers an event to all subscribers.
func (b *runtimeEventBus) Publish(ev events.Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	subs := append([]runtimeEventSubscriber{}, b.subscribers...)
	b.mu.RUnlock()
	for _, sub := range subs {
		if sub != nil {
			sub.OnIMSEvent(ev)
		}
	}
}

// Snapshot returns the subscriber count.
func (b *runtimeEventBus) Snapshot() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
