package imscore

import (
	"sync"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

// imsEventSubscriber receives IMS events.
type imsEventSubscriber interface {
	OnIMSEvent(ev events.Event)
}

// EventBus is the exported IMS event bus used by the voice layer.
type EventBus = imsEventBus

// imsEventBus is the in-process IMS event bus.
type imsEventBus struct {
	mu          sync.RWMutex
	subscribers []imsEventSubscriber
}

// newIMSEventBus creates an event bus.
func newIMSEventBus() *imsEventBus {
	return &imsEventBus{}
}

// NewEventBus creates an exported event bus.
func NewEventBus() *EventBus {
	return newIMSEventBus()
}

// Subscribe registers a subscriber.
func (b *imsEventBus) Subscribe(sub imsEventSubscriber) {
	if b == nil || sub == nil {
		return
	}
	b.mu.Lock()
	b.subscribers = append(b.subscribers, sub)
	b.mu.Unlock()
}

// Unsubscribe removes a subscriber.
func (b *imsEventBus) Unsubscribe(sub imsEventSubscriber) {
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
func (b *imsEventBus) Publish(ev events.Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	subs := append([]imsEventSubscriber{}, b.subscribers...)
	b.mu.RUnlock()
	for _, sub := range subs {
		if sub != nil {
			sub.OnIMSEvent(ev)
		}
	}
}

// Snapshot returns the subscriber count.
func (b *imsEventBus) Snapshot() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
