package imscore

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ussi"
)

type orderedEventSubscriber struct {
	id     string
	mu     *sync.Mutex
	record *[]string
	before func()
}

func (subscriber *orderedEventSubscriber) OnIMSEvent(event events.Event) {
	if subscriber.before != nil {
		subscriber.before()
	}
	subscriber.mu.Lock()
	*subscriber.record = append(*subscriber.record, subscriber.id+":"+event.Type())
	subscriber.mu.Unlock()
}

func TestEventBusPreservesPublishAndSubscriptionOrder(t *testing.T) {
	bus := NewEventBus()
	var mu sync.Mutex
	var record []string
	first := &orderedEventSubscriber{id: "first", mu: &mu, record: &record}
	second := &orderedEventSubscriber{id: "second", mu: &mu, record: &record}
	bus.Subscribe(first)
	bus.Subscribe(second)

	bus.Publish(events.EventSMSSendAccepted{DevID: "dev"})
	bus.Publish(events.EventSMSSent{DevID: "dev"})
	want := []string{
		"first:SMSSendAccepted", "second:SMSSendAccepted",
		"first:SMSSent", "second:SMSSent",
	}
	if !reflect.DeepEqual(record, want) {
		t.Fatalf("delivery order = %v, want %v", record, want)
	}
	if got := bus.Snapshot(); got != 2 {
		t.Fatalf("subscriber count = %d, want 2", got)
	}
}

func TestEventBusSubscriberMutationUsesPublishSnapshot(t *testing.T) {
	bus := NewEventBus()
	var mu sync.Mutex
	var record []string
	second := &orderedEventSubscriber{id: "second", mu: &mu, record: &record}
	first := &orderedEventSubscriber{id: "first", mu: &mu, record: &record}
	first.before = func() { bus.Unsubscribe(second) }
	bus.Subscribe(first)
	bus.Subscribe(second)

	bus.Publish(events.EventCallRinging{DevID: "dev"})
	bus.Publish(events.EventCallAnswered{DevID: "dev"})
	want := []string{"first:CallRinging", "second:CallRinging", "first:CallAnswered"}
	if !reflect.DeepEqual(record, want) {
		t.Fatalf("delivery order = %v, want %v", record, want)
	}
}

type countingEventSubscriber struct{ count atomic.Int64 }

func (subscriber *countingEventSubscriber) OnIMSEvent(events.Event) {
	subscriber.count.Add(1)
}

func TestEventBusConcurrentPublishAndSubscription(t *testing.T) {
	const publishers = 8
	const eventsPerPublisher = 100
	bus := NewEventBus()
	base := &countingEventSubscriber{}
	bus.Subscribe(base)

	var workers sync.WaitGroup
	for publisher := 0; publisher < publishers; publisher++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			for sequence := 0; sequence < eventsPerPublisher; sequence++ {
				bus.Publish(events.EventLogNotify{DevID: fmt.Sprint(id), Message: fmt.Sprint(sequence)})
			}
		}(publisher)
	}
	for worker := 0; worker < publishers; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			temporary := &countingEventSubscriber{}
			bus.Subscribe(temporary)
			bus.Unsubscribe(temporary)
		}()
	}
	workers.Wait()
	if got, want := base.count.Load(), int64(publishers*eventsPerPublisher); got != want {
		t.Fatalf("base deliveries = %d, want %d", got, want)
	}
}

type channelEventSubscriber chan events.Event

func (subscriber channelEventSubscriber) OnIMSEvent(event events.Event) {
	subscriber <- event
}

func TestPublishUSSIResultPopulatesRecoveredFields(t *testing.T) {
	bus := NewEventBus()
	received := make(channelEventSubscriber, 1)
	bus.Subscribe(received)
	service := &Service{cfg: &IMSConfig{DeviceID: "wwan0"}, bus: bus}
	service.dispatchUSSIResult("*100#", &ussi.Result{
		SessionID: "session-1", Status: 1, Text: "Balance: 10.00", DCS: 15,
	})

	select {
	case event := <-received:
		result, ok := event.(events.EventUSSDResult)
		if !ok || result.SessionID != "session-1" || result.Command != "*100#" || result.Text != "Balance: 10.00" ||
			result.Status != 1 || result.Code != "1" || result.Message != result.Text || result.Time.IsZero() {
			t.Fatalf("USSD event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("USSD event was not published")
	}
}
