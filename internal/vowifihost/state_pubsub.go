package vowifihost

import (
	"sync"

	"github.com/iniwex5/vowifi-go/runtimehost"
)

type StateHub struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[string]map[uint64]*stateSubscriber
}

func NewStateHub() *StateHub {
	return &StateHub{subs: make(map[string]map[uint64]*stateSubscriber)}
}

func (h *StateHub) Subscribe(deviceID string) (<-chan runtimehost.State, func()) {
	if h == nil {
		h = NewStateHub()
	}
	subscriber := newStateSubscriber()

	h.mu.Lock()
	if h.subs == nil {
		h.subs = make(map[string]map[uint64]*stateSubscriber)
	}
	h.nextID++
	subID := h.nextID
	if h.subs[deviceID] == nil {
		h.subs[deviceID] = make(map[uint64]*stateSubscriber)
	}
	h.subs[deviceID][subID] = subscriber
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		if subs, ok := h.subs[deviceID]; ok {
			delete(subs, subID)
			if len(subs) == 0 {
				delete(h.subs, deviceID)
			}
		}
		h.mu.Unlock()
		subscriber.stop()
	}

	return subscriber.out, unsub
}

func (h *StateHub) Broadcast(deviceID string, state runtimehost.State) {
	if h == nil {
		return
	}

	h.mu.RLock()
	subs, ok := h.subs[deviceID]
	if !ok || len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	listeners := make([]*stateSubscriber, 0, len(subs))
	for _, subscriber := range subs {
		listeners = append(listeners, subscriber)
	}
	h.mu.RUnlock()

	for _, subscriber := range listeners {
		subscriber.enqueue(state)
	}
}

func (h *StateHub) SubscriberCount(deviceID string) int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[deviceID])
}

type stateSubscriber struct {
	mu      sync.Mutex
	out     chan runtimehost.State
	wake    chan struct{}
	done    chan struct{}
	queue   []runtimehost.State
	stopped bool
}

func newStateSubscriber() *stateSubscriber {
	subscriber := &stateSubscriber{
		out: make(chan runtimehost.State), wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	go subscriber.run()
	return subscriber
}

func (subscriber *stateSubscriber) enqueue(state runtimehost.State) {
	subscriber.mu.Lock()
	if subscriber.stopped {
		subscriber.mu.Unlock()
		return
	}
	subscriber.queue = append(subscriber.queue, state)
	subscriber.mu.Unlock()
	select {
	case subscriber.wake <- struct{}{}:
	default:
	}
}

func (subscriber *stateSubscriber) run() {
	for {
		select {
		case <-subscriber.done:
			return
		case <-subscriber.wake:
			if !subscriber.flush() {
				return
			}
		}
	}
}

func (subscriber *stateSubscriber) flush() bool {
	for {
		state, ok := subscriber.next()
		if !ok {
			return true
		}
		select {
		case subscriber.out <- state:
		case <-subscriber.done:
			return false
		}
	}
}

func (subscriber *stateSubscriber) next() (runtimehost.State, bool) {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	if len(subscriber.queue) == 0 {
		return runtimehost.State{}, false
	}
	state := subscriber.queue[0]
	subscriber.queue[0] = runtimehost.State{}
	subscriber.queue = subscriber.queue[1:]
	return state, true
}

func (subscriber *stateSubscriber) stop() {
	subscriber.mu.Lock()
	if subscriber.stopped {
		subscriber.mu.Unlock()
		return
	}
	subscriber.stopped = true
	subscriber.queue = nil
	close(subscriber.done)
	subscriber.mu.Unlock()
}
