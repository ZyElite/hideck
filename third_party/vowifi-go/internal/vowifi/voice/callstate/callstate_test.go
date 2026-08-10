package callstate

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const actorTestTimeout = 2 * time.Second

type originalActorAPI interface {
	Start(context.Context)
	Stop()
	Enqueue(string, func()) bool
	QueueLen() int
}

var _ originalActorAPI = (*Actor)(nil)

func TestStateString(t *testing.T) {
	want := []string{
		"Init", "Calling", "Ringing", "EarlyMedia",
		"PreconditionWait", "Connected", "Terminating", "Terminated",
	}
	for state, name := range want {
		if got := State(state).String(); got != name {
			t.Errorf("State(%d).String() = %q, want %q", state, got, name)
		}
	}
	if got := State(99).String(); got != "State(99)" {
		t.Fatalf("invalid state string = %q", got)
	}
}

func TestTransitionMapMatchesOriginal(t *testing.T) {
	want := map[State][]State{
		StateInit:             {StateCalling, StateRinging, StateConnected, StateTerminated},
		StateCalling:          {StateRinging, StateEarlyMedia, StateConnected, StateTerminating, StateTerminated},
		StateRinging:          {StateEarlyMedia, StateConnected, StateTerminating, StateTerminated},
		StateEarlyMedia:       {StatePreconditionWait, StateConnected, StateTerminating, StateTerminated},
		StatePreconditionWait: {StateEarlyMedia, StateConnected, StateTerminating, StateTerminated},
		StateConnected:        {StateTerminating, StateTerminated},
		StateTerminating:      {StateTerminated},
		StateTerminated:       {},
	}
	for from := StateInit; from <= StateTerminated; from++ {
		allowed := make(map[State]bool, len(want[from]))
		for _, to := range want[from] {
			allowed[to] = true
		}
		for to := StateInit; to <= StateTerminated; to++ {
			if got := CanTransition(from, to); got != allowed[to] {
				t.Errorf("CanTransition(%s, %s) = %t, want %t", from, to, got, allowed[to])
			}
		}
	}
}

func TestTerminalStatesMatchOriginalCallLifecycle(t *testing.T) {
	for state := StateInit; state <= StateTerminated; state++ {
		want := state == StateTerminating || state == StateTerminated
		if got := IsTerminal(state); got != want {
			t.Errorf("IsTerminal(%s) = %t, want %t", state, got, want)
		}
	}
	if IsTerminal(State(99)) {
		t.Fatal("unknown state must not be terminal")
	}
}

func TestActorLifecycleAndRestart(t *testing.T) {
	a := NewActor()
	if a.Enqueue("before_start", func() {}) {
		t.Fatal("stopped actor accepted work")
	}
	for cycle := 0; cycle < 2; cycle++ {
		a.Start(context.Background())
		a.Start(context.Background())
		done := make(chan struct{})
		if !a.Enqueue("lifecycle", func() { close(done) }) {
			t.Fatalf("cycle %d: running actor rejected work", cycle)
		}
		waitClosed(t, done)
		a.Stop()
	}
	a.Stop()
}

func TestActorQueueFullNeverRunsOnCaller(t *testing.T) {
	a := NewActorWithConfig(ActorConfig{QueueCapacity: 1})
	a.Start(context.Background())
	defer a.Stop()

	running := make(chan struct{})
	release := make(chan struct{})
	if !a.Enqueue("running", func() {
		close(running)
		<-release
	}) {
		t.Fatal("failed to enqueue blocking task")
	}
	waitClosed(t, running)
	if !a.Enqueue("queued", func() {}) {
		t.Fatal("failed to fill queue")
	}
	if got := a.QueueLen(); got != 1 {
		t.Fatalf("QueueLen = %d, want 1", got)
	}
	var callerRan atomic.Bool
	if a.Enqueue("rejected", func() { callerRan.Store(true) }) {
		t.Fatal("full queue accepted task")
	}
	if callerRan.Load() {
		t.Fatal("rejected task ran synchronously on caller")
	}
	close(release)
}

func TestActorSerializesConcurrentProducers(t *testing.T) {
	a := NewActor()
	a.Start(context.Background())
	defer a.Stop()

	const taskCount = 96
	var active atomic.Int32
	var maxActive atomic.Int32
	var tasks sync.WaitGroup
	tasks.Add(taskCount)
	start := make(chan struct{})
	accepted := make(chan bool, taskCount)
	for i := 0; i < taskCount; i++ {
		go func() {
			<-start
			accepted <- a.Enqueue("concurrent", func() {
				current := active.Add(1)
				updateMaximum(&maxActive, current)
				time.Sleep(time.Millisecond)
				active.Add(-1)
				tasks.Done()
			})
		}()
	}
	close(start)
	for i := 0; i < taskCount; i++ {
		if !<-accepted {
			t.Fatalf("task %d was rejected", i)
		}
	}
	waitGroup(t, &tasks)
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("maximum concurrent tasks = %d, want 1", got)
	}
}

func TestActorRecoversTaskPanic(t *testing.T) {
	a := NewActor()
	a.Start(context.Background())
	defer a.Stop()

	if !a.Enqueue("panic", func() { panic("test panic") }) {
		t.Fatal("panic task rejected")
	}
	done := make(chan struct{})
	if !a.Enqueue("after_panic", func() { close(done) }) {
		t.Fatal("follow-up task rejected")
	}
	waitClosed(t, done)
}

func TestActorParentCancellationRejectsWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := NewActor()
	a.Start(ctx)
	cancel()

	deadline := time.Now().Add(actorTestTimeout)
	for time.Now().Before(deadline) {
		if !a.Enqueue("after_cancel", func() {}) {
			a.Stop()
			return
		}
		time.Sleep(time.Millisecond)
	}
	a.Stop()
	t.Fatal("actor continued accepting work after parent cancellation")
}

func TestActorStopWaitsForRunningTask(t *testing.T) {
	a := NewActor()
	a.Start(context.Background())
	running := make(chan struct{})
	release := make(chan struct{})
	if !a.Enqueue("blocking", func() {
		close(running)
		<-release
	}) {
		t.Fatal("blocking task rejected")
	}
	waitClosed(t, running)

	stopped := make(chan struct{})
	go func() {
		a.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before the running task completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	waitClosed(t, stopped)
}

func TestActorNilAndQueueLength(t *testing.T) {
	var nilActor *Actor
	if nilActor.Enqueue("nil", func() {}) || nilActor.QueueLen() != 0 {
		t.Fatal("nil actor must reject work and report an empty queue")
	}
	nilActor.Start(nil)
	nilActor.Stop()

	a := NewActorWithConfig(ActorConfig{QueueCapacity: 2})
	a.Start(nil)
	defer a.Stop()
	if a.Enqueue("nil_fn", nil) {
		t.Fatal("nil task function accepted")
	}
}

func TestOriginalResourceStateLayouts(t *testing.T) {
	assertFieldNames(t, reflect.TypeOf(Task{}), []string{"Name", "EnqueuedAt", "Fn"})
	assertFieldNames(t, reflect.TypeOf(DialogState{}), []string{
		"IMSSession", "CallerID", "CalleeID", "CallID", "FromTag", "ToTag",
		"OutboundIMSCallID", "IMSCallID", "IMSFromTag", "IMSToTag", "IMSBranch",
		"IMSCSeq", "ACKSent", "InviteProvisional", "LocalCancelSent",
		"LocalCancelReason", "InviteFinalSeen", "ErrorACKSent", "ClientFromTag",
		"ClientToTag", "ClientCallID", "ClientDest", "ClientLocalIP", "ClientTx",
		"OriginalRequest", "IMSResponseCh", "IMSDialog", "IMSInviteHandle",
		"ServerInvite", "IMSContact", "RouteSet", "OutboundTxBranch", "OutboundCSeq",
		"ServerTx",
	})
	assertFieldNames(t, reflect.TypeOf(MediaState{}), []string{
		"IMSSDP", "ClientSDP", "RTPRelay", "MediaManager", "PreconditionMet",
	})
	assertFieldNames(t, reflect.TypeOf(Timers{}), []string{
		"SessionExpires", "SessionTimer", "SessionTimerMu", "PrackTimer",
		"PrackTimerMu", "PrackTimerGeneration", "RSeq",
	})
	assertFieldPrefix(t, reflect.TypeOf(Actor{}), []string{
		"deviceID", "traceID", "queueCap", "mu", "ctx", "cancel", "queue", "done",
	})
}

func TestCompatibilityPhaseStrings(t *testing.T) {
	if DirectionOutbound.String() != "outbound" || DirectionInbound.String() != "inbound" {
		t.Fatal("direction strings changed")
	}
	if MediaActive.String() != "active" || DialogConfirmed.String() != "confirmed" {
		t.Fatal("phase strings changed")
	}
}

func assertFieldNames(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	if typ.NumField() != len(want) {
		t.Fatalf("%s field count = %d, want %d", typ.Name(), typ.NumField(), len(want))
	}
	for index, name := range want {
		if got := typ.Field(index).Name; got != name {
			t.Fatalf("%s field %d = %s, want %s", typ.Name(), index, got, name)
		}
	}
}

func assertFieldPrefix(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	if typ.NumField() < len(want) {
		t.Fatalf("%s field count = %d, want at least %d", typ.Name(), typ.NumField(), len(want))
	}
	for index, name := range want {
		if got := typ.Field(index).Name; got != name {
			t.Fatalf("%s field %d = %s, want %s", typ.Name(), index, got, name)
		}
	}
}

func updateMaximum(maximum *atomic.Int32, value int32) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(actorTestTimeout):
		t.Fatal("timed out waiting for channel")
	}
}

func waitGroup(t *testing.T, group *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	waitClosed(t, done)
}
