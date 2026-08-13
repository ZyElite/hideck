package voicehost

import "sync"

const subscriptionQueueSize = 32

type incomingSubscription struct {
	queue   chan IncomingCall
	done    chan struct{}
	stop    sync.Once
	handler func(IncomingCall)
}

func newIncomingSubscription(handler func(IncomingCall)) *incomingSubscription {
	subscription := &incomingSubscription{
		queue: make(chan IncomingCall, subscriptionQueueSize), done: make(chan struct{}), handler: handler,
	}
	go subscription.run()
	return subscription
}

func (s *incomingSubscription) run() {
	for {
		select {
		case call := <-s.queue:
			s.handler(call)
		case <-s.done:
			return
		}
	}
}

func (s *incomingSubscription) enqueue(call IncomingCall) {
	if s == nil {
		return
	}
	select {
	case s.queue <- call:
	case <-s.done:
	default:
	}
}

func (s *incomingSubscription) close() {
	if s != nil {
		s.stop.Do(func() { close(s.done) })
	}
}

type callEventSubscription struct {
	queue   chan CallEvent
	done    chan struct{}
	stop    sync.Once
	handler func(CallEvent)
}

func newCallEventSubscription(handler func(CallEvent)) *callEventSubscription {
	subscription := &callEventSubscription{
		queue: make(chan CallEvent, subscriptionQueueSize), done: make(chan struct{}), handler: handler,
	}
	go subscription.run()
	return subscription
}

func (s *callEventSubscription) run() {
	for {
		select {
		case event := <-s.queue:
			s.handler(event)
		case <-s.done:
			return
		}
	}
}

func (s *callEventSubscription) enqueue(event CallEvent) {
	if s == nil {
		return
	}
	select {
	case s.queue <- event:
	case <-s.done:
	default:
	}
}

func (s *callEventSubscription) close() {
	if s != nil {
		s.stop.Do(func() { close(s.done) })
	}
}
