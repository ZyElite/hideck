package voice

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

func TestNoAnswerBeforeProvisionalReturns408WithoutCANCEL(t *testing.T) {
	agent := NewAgent("device-timeout-before-1xx", nil, nil)
	call := newPendingClientCall(t, agent, "timeout-before-1xx")
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	call.SetOutboundRuntimeCancel(func() { cancelOnce.Do(func() { close(canceled) }) })

	agent.handleOutboundInviteNoAnswerTimeout(call)
	waitVoiceResponse(t, call.DialogState.ClientTx.(*voiceServerTransaction), 408)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("timeout did not stop the outbound runtime")
	}
	if !callDone(call) || call.CallState() != callstate.StateTerminated || agent.IsBusy() {
		t.Fatalf("timeout cleanup: done=%t state=%s busy=%t",
			callDone(call), call.CallState(), agent.IsBusy())
	}
}

func TestNoAnswerCancelSettleReturns408AndReleasesCall(t *testing.T) {
	agent := NewAgent("device-timeout-settle", nil, nil)
	call := newPendingClientCall(t, agent, "timeout-settle")
	call.MarkInviteProvisional(180)
	if !call.MarkLocalCancelSent("no_answer") {
		t.Fatal("no-answer cancellation was not marked")
	}

	agent.settleOutboundCancel(call, "no_answer")
	waitVoiceResponse(t, call.DialogState.ClientTx.(*voiceServerTransaction), 408)
	if !callDone(call) || agent.IsBusy() {
		t.Fatalf("settled timeout: done=%t busy=%t", callDone(call), agent.IsBusy())
	}
}

func TestCanceledInvitePublishesOnlyOneLocalFinal(t *testing.T) {
	agent := NewAgent("device-cancel-final", nil, nil)
	call := newPendingClientCall(t, agent, "cancel-final-once")
	transaction := call.DialogState.ClientTx.(*voiceServerTransaction)
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func(status int) {
			defer workers.Done()
			agent.respondSyntheticFinalToClient(call, status, "Canceled")
		}(487 + index%2)
	}
	workers.Wait()

	select {
	case <-transaction.responses:
	case <-time.After(time.Second):
		t.Fatal("canceled INVITE did not receive a final response")
	}
	select {
	case response := <-transaction.responses:
		t.Fatalf("canceled INVITE received duplicate final %d", response.StatusCode)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestCancelSettleAndFinalFailureShareTerminalOwner(t *testing.T) {
	agent := NewAgent("device-cancel-terminal", nil, nil)
	call := newPendingClientCall(t, agent, "cancel-terminal-once")
	if !call.MarkLocalCancelSent("client_cancel") {
		t.Fatal("client cancellation was not marked")
	}
	var notifications atomic.Int32
	agent.SetNotifier(func(events.Event) { notifications.Add(1) })
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		agent.settleOutboundCancel(call, "client_cancel")
	}()
	go func() {
		defer workers.Done()
		_ = agent.failOutboundCall(call, errors.New("IMS final failure"))
	}()
	workers.Wait()
	deadline := time.Now().Add(time.Second)
	for notifications.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := notifications.Load(); got != 1 {
		t.Fatalf("terminal notifications = %d, want 1", got)
	}
}

func newPendingClientCall(t *testing.T, agent *Agent, callID string) *Call {
	t.Helper()
	request := mustClientRequest(t, sip.INVITE, callID, testClientSDP, "")
	call := NewCallFromClientInvite(agent.DeviceID(), request)
	call.agent = agent
	if err := call.TransitionChecked(callstate.StateCalling); err != nil {
		t.Fatal(err)
	}
	call.DialogState.ClientTx = newVoiceServerTransaction()
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	t.Cleanup(call.Cancel)
	return call
}
