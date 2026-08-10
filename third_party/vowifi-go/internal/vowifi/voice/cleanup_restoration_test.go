package voice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/media"
)

type originalCallCleanupAPI interface {
	Hangup()
	StartOutboundNoAnswerTimer(time.Duration)
	StopOutboundNoAnswerTimer()
	CancelOutboundInviteTimer()
	EnsureTimerStopped()
}

type originalAgentCleanupAPI interface {
	Hangup()
}

var _ originalCallCleanupAPI = (*Call)(nil)
var _ originalAgentCleanupAPI = (*Agent)(nil)

type cleanupCaptureWriter struct {
	closeCount atomic.Int32
	closeErr   error
}

func (w *cleanupCaptureWriter) Write(packet []byte) (int, error) { return len(packet), nil }

func (w *cleanupCaptureWriter) Close() error {
	w.closeCount.Add(1)
	return w.closeErr
}

func TestTerminalFinalizationReleasesAllCallOwnership(t *testing.T) {
	agent := NewAgent("cleanup-device", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "cleanup-call", "43430")
	if err := call.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	relay := media.NewRTPRelay(nil, nil)
	call.SetRTPRelay(relay)
	call.StartOutboundNoAnswerTimer(time.Hour)
	call.StartSessionTimer(func() {})
	call.StartPrackRuntimeRetransmission(func() {})
	var runtimeCanceled atomic.Int32
	call.SetOutboundRuntimeCancel(func() { runtimeCanceled.Add(1) })
	registerCleanupCall(agent, call, true)

	if err := agent.finishLocalHangup(call); err != nil {
		t.Fatal(err)
	}
	assertCallOwnershipReleased(t, agent, call)
	if runtimeCanceled.Load() != 1 {
		t.Fatalf("outbound runtime canceled %d times, want 1", runtimeCanceled.Load())
	}
	if err := relay.StartCurrent(); err == nil {
		t.Fatal("released media relay started again")
	}
	if err := agent.finalizeActiveCall(call); err != nil {
		t.Fatal(err)
	}
	if runtimeCanceled.Load() != 1 {
		t.Fatalf("repeated finalization canceled runtime %d times", runtimeCanceled.Load())
	}
}

func TestTerminalFinalizationReturnsPCAPCloseFailure(t *testing.T) {
	closeFailure := errors.New("forced cleanup PCAP close failure")
	writer := &cleanupCaptureWriter{closeErr: closeFailure}
	agent := NewAgent("cleanup-error-device", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "cleanup-error-call", "43430")
	if err := call.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	relay := media.NewRTPRelay(nil, nil)
	call.SetRTPRelay(relay)
	if err := call.StartPCAPCurrent(writer); err != nil {
		t.Fatal(err)
	}
	registerCleanupCall(agent, call, true)

	if err := agent.finishLocalHangup(call); !errors.Is(err, closeFailure) {
		t.Fatalf("terminal cleanup error = %v", err)
	}
	if writer.closeCount.Load() != 1 {
		t.Fatalf("PCAP writer closed %d times, want 1", writer.closeCount.Load())
	}
	if err := agent.finalizeActiveCall(call); !errors.Is(err, closeFailure) {
		t.Fatalf("repeated cleanup error = %v", err)
	}
	if writer.closeCount.Load() != 1 {
		t.Fatalf("repeated finalization closed writer %d times", writer.closeCount.Load())
	}
}

func TestCancelOutboundRuntimeReleasesCallbackOnce(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "cancel-runtime-call", "43430")
	defer call.actor.Stop()
	call.StartOutboundNoAnswerTimer(time.Hour)
	var canceled atomic.Int32
	call.SetOutboundRuntimeCancel(func() { canceled.Add(1) })
	call.cancelOutboundRuntime()
	call.cancelOutboundRuntime()
	if err := call.finalizeResourcesCurrent(); err != nil {
		t.Fatal(err)
	}
	if canceled.Load() != 1 {
		t.Fatalf("outbound runtime callback count = %d, want 1", canceled.Load())
	}
	call.mu.RLock()
	defer call.mu.RUnlock()
	if call.outboundRuntimeCancel != nil || call.outboundNoAnswerStop != nil || call.noAnswerTimer != nil {
		t.Fatal("outbound runtime cancellation retained ownership")
	}
}

func TestRecoveredCallHangupReleasesLocalResources(t *testing.T) {
	call := NewCall(nil, callstate.DirectionOutbound, "legacy-call-hangup", "43430")
	defer call.actor.Stop()
	if err := call.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	relay := media.NewRTPRelay(nil, nil)
	call.SetRTPRelay(relay)
	call.StartSessionTimer(func() {})
	call.StartPrackRuntimeRetransmission(func() {})
	call.Hangup()
	if call.CallState() != callstate.StateTerminated {
		t.Fatalf("legacy Call.Hangup state = %s", call.CallState())
	}
	select {
	case <-call.Ctx.Done():
	default:
		t.Fatal("legacy Call.Hangup retained its context")
	}
	if err := relay.StartCurrent(); err == nil {
		t.Fatal("legacy Call.Hangup retained a usable relay")
	}
}

func TestRecoveredAgentHangupReleasesActiveCall(t *testing.T) {
	agent := NewAgent("legacy-agent-hangup", nil, nil)
	call := NewCall(agent, callstate.DirectionOutbound, "legacy-agent-call", "43430")
	if err := call.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	registerCleanupCall(agent, call, true)
	agent.Hangup()
	assertCallOwnershipReleased(t, agent, call)
	call.actor.Stop()
}

func TestAgentStopReleasesCallsEvenWhenNotStarted(t *testing.T) {
	agent := NewAgent("stopped-agent-device", nil, nil)
	active := NewCall(agent, callstate.DirectionOutbound, "stopped-active", "43430")
	stale := NewCall(agent, callstate.DirectionOutbound, "stopped-stale", "43430")
	active.StopMedia()
	if err := stale.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	registerCleanupCall(agent, active, true)
	registerCleanupCall(agent, stale, false)

	if err := agent.Stop(); err != nil {
		t.Fatal(err)
	}
	assertCallOwnershipReleased(t, agent, active)
	assertCallOwnershipReleased(t, agent, stale)
	if active.actor.Enqueue("after-stop", func() {}) || stale.actor.Enqueue("after-stop", func() {}) {
		t.Fatal("stopped call actor accepted work")
	}
	if err := agent.Stop(); err != nil {
		t.Fatalf("repeated Agent.Stop: %v", err)
	}
}

func TestGatewayStopWaitsForRunningEntryWorker(t *testing.T) {
	agent := NewAgent("worker-device", nil, nil)
	gateway := NewGateway(agent)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	if !gateway.enqueueDeviceTask("worker-device", "blocking-cleanup", func(*Agent) {
		close(started)
		<-release
	}) {
		t.Fatal("gateway rejected cleanup test task")
	}
	<-started
	stopped := make(chan error, 1)
	go func() { stopped <- gateway.Stop() }()
	select {
	case err := <-stopped:
		close(release)
		t.Fatalf("Gateway.Stop returned before worker exit: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Gateway.Stop did not join released worker")
	}
}

func TestGatewayStopJoinsWorkerReplacedByRestart(t *testing.T) {
	agent := NewAgent("restarted-worker-device", nil, nil)
	gateway := NewGateway(agent)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	if !gateway.enqueueDeviceTask("restarted-worker-device", "blocking-old-worker", func(*Agent) {
		close(started)
		<-release
	}) {
		t.Fatal("gateway rejected old-worker task")
	}
	<-started
	if err := gateway.Start(context.Background()); err != nil {
		close(release)
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- gateway.Stop() }()
	select {
	case err := <-stopped:
		close(release)
		t.Fatalf("Gateway.Stop lost replaced worker: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Gateway.Stop did not join replaced worker")
	}
}

func registerCleanupCall(agent *Agent, call *Call, active bool) {
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	if active {
		agent.activeCall = call
	}
	agent.mu.Unlock()
}

func assertCallOwnershipReleased(t *testing.T, agent *Agent, call *Call) {
	t.Helper()
	if agent.callByID(call.CallID()) != nil || agent.ActiveCall() == call {
		t.Fatal("terminal call remained in Agent registry")
	}
	select {
	case <-call.Done:
	default:
		t.Fatal("terminal call completion channel remained open")
	}
	select {
	case <-call.Ctx.Done():
	default:
		t.Fatal("terminal call context remained active")
	}
	call.mu.RLock()
	noAnswerTimer := call.noAnswerTimer
	noAnswerStop := call.outboundNoAnswerStop
	runtimeCancel := call.outboundRuntimeCancel
	prackTimer := call.prackTimer
	prackRetry := call.prackRetransmit
	call.mu.RUnlock()
	call.SessionTimerMu.Lock()
	sessionTimer := call.SessionTimer
	call.SessionTimerMu.Unlock()
	if noAnswerTimer != nil || noAnswerStop != nil || runtimeCancel != nil ||
		prackTimer != nil || prackRetry != nil || sessionTimer != nil {
		t.Fatal("terminal call retained timer or runtime ownership")
	}
}
