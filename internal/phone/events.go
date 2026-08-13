package phone

import "sync"

const (
	eventBufferSize      = 256
	subscriberBufferSize = 32
)

type eventHub struct {
	mu          sync.Mutex
	nextID      uint64
	events      []Event
	nextSubID   uint64
	subscribers map[uint64]chan Event
}

func newEventHub() *eventHub {
	return &eventHub{subscribers: make(map[uint64]chan Event)}
}

func (hub *eventHub) publish(event Event) Event {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.nextID++
	event.ID = hub.nextID
	hub.events = append(hub.events, event)
	if len(hub.events) > eventBufferSize {
		hub.events = append([]Event(nil), hub.events[len(hub.events)-eventBufferSize:]...)
	}
	for id, subscriber := range hub.subscribers {
		select {
		case subscriber <- event:
		default:
			delete(hub.subscribers, id)
			close(subscriber)
		}
	}
	return event
}

func (hub *eventHub) subscribe(afterID uint64) ([]Event, <-chan Event, func()) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	backlog := make([]Event, 0, len(hub.events))
	for _, event := range hub.events {
		if event.ID > afterID {
			backlog = append(backlog, event)
		}
	}
	hub.nextSubID++
	id := hub.nextSubID
	stream := make(chan Event, subscriberBufferSize)
	hub.subscribers[id] = stream
	return backlog, stream, func() {
		hub.mu.Lock()
		if subscriber, ok := hub.subscribers[id]; ok {
			delete(hub.subscribers, id)
			close(subscriber)
		}
		hub.mu.Unlock()
	}
}
