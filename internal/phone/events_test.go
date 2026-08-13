package phone

import (
	"testing"
	"time"
)

func TestEventHubReplaysOnlyEventsAfterLastID(t *testing.T) {
	hub := newEventHub()
	first := hub.publish(Event{Type: "first", Time: time.Now()})
	second := hub.publish(Event{Type: "second", Time: time.Now()})
	backlog, stream, cancel := hub.subscribe(first.ID)
	defer cancel()
	if len(backlog) != 1 || backlog[0].ID != second.ID {
		t.Fatalf("backlog = %+v", backlog)
	}
	third := hub.publish(Event{Type: "third", Time: time.Now()})
	select {
	case event := <-stream:
		if event.ID != third.ID {
			t.Fatalf("stream event = %+v, want %+v", event, third)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive new event")
	}
}

func TestEventHubDisconnectsSlowSubscriberForReplay(t *testing.T) {
	hub := newEventHub()
	_, stream, cancel := hub.subscribe(0)
	defer cancel()
	for index := 0; index <= subscriberBufferSize; index++ {
		hub.publish(Event{Type: "event", Time: time.Now()})
	}
	for range stream {
	}
	backlog, _, replayCancel := hub.subscribe(0)
	defer replayCancel()
	if len(backlog) != subscriberBufferSize+1 {
		t.Fatalf("replay count = %d, want %d", len(backlog), subscriberBufferSize+1)
	}
}
