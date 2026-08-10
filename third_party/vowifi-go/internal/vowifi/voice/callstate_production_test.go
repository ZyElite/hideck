package voice

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

func TestAgentIMSEventNotificationsUseActor(t *testing.T) {
	const eventCount = 64
	agent := NewAgent("actor-device", nil, nil)
	var active atomic.Int32
	var maximum atomic.Int32
	var completed atomic.Int32
	done := make(chan struct{})
	agent.SetNotifier(func(events.Event) {
		current := active.Add(1)
		updateActorMaximum(&maximum, current)
		time.Sleep(time.Millisecond)
		active.Add(-1)
		if completed.Add(1) == eventCount {
			close(done)
		}
	})
	if err := agent.StartCurrent(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := agent.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	var producers sync.WaitGroup
	producers.Add(eventCount)
	for index := 0; index < eventCount; index++ {
		go func(index int) {
			defer producers.Done()
			agent.OnIMSEvent(&events.EventCallRinging{
				DevID: "actor-device", CallID: fmt.Sprintf("call-%d", index), Time: time.Now(),
			})
		}(index)
	}
	producers.Wait()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("received %d of %d notifications", completed.Load(), eventCount)
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent notifier calls = %d, want 1", got)
	}
}

func updateActorMaximum(maximum *atomic.Int32, value int32) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}
