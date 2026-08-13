package voice

import (
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
)

func TestIncomingNotifierIsDeduplicatedAndNonBlocking(t *testing.T) {
	gateway := NewGateway(nil)
	notifier := &blockingCallNotifier{started: make(chan struct{}), release: make(chan struct{})}
	gateway.SetNotifier(notifier)
	event := events.EventIncomingCall{
		DevID: "dev-1", CallID: "call-1", Caller: "+15550001", Callee: "+15550002",
	}

	returned := make(chan struct{})
	go func() {
		gateway.forwardAgentEvent(event)
		gateway.forwardAgentEvent(event)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("blocking notifier stalled voice event forwarding")
	}
	select {
	case <-notifier.started:
	case <-time.After(time.Second):
		t.Fatal("notifier was not called")
	}
	close(notifier.release)
	select {
	case count := <-notifier.completed:
		if count != 1 {
			t.Fatalf("notification count = %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("notifier did not complete")
	}
}

type blockingCallNotifier struct {
	started   chan struct{}
	release   chan struct{}
	completed chan int
}

func (n *blockingCallNotifier) NotifyIncomingCall(_, _, _ string) {
	if n.completed == nil {
		n.completed = make(chan int, 1)
	}
	close(n.started)
	<-n.release
	n.completed <- 1
}
