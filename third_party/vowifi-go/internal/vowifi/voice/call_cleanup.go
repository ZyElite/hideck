package voice

import (
	"context"
	"errors"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/logging"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/client"
)

type agentStopSnapshot struct {
	cancel      context.CancelFunc
	unsubscribe func()
	bridge      *client.Bridge
	ims         *imscore.Service
	active      *Call
	calls       []*Call
}

func (c *Call) finalizeResourcesCurrent() error {
	if c == nil {
		return nil
	}
	c.cleanupOnce.Do(func() {
		c.cleanupErr = c.StopMediaCurrent()
		c.EnsureTimerStopped()
		c.CancelOutboundInviteTimer()
		c.CloseDone()
		if c.Cancel != nil {
			c.Cancel()
		}
	})
	return c.cleanupErr
}

func releaseUnregisteredCall(call *Call) error {
	if call == nil {
		return nil
	}
	err := call.finalizeResourcesCurrent()
	if call.actor != nil {
		call.actor.Stop()
	}
	return err
}

func (a *Agent) stopAndRelease() error {
	snapshot := a.detachRuntimeForStop()
	if snapshot.cancel != nil {
		snapshot.cancel()
	}
	if snapshot.unsubscribe != nil {
		snapshot.unsubscribe()
	}
	if snapshot.ims != nil {
		snapshot.ims.SetVoiceRequestHandler(nil)
	}
	if snapshot.bridge != nil {
		snapshot.bridge.Stop()
	}
	a.actor.Stop()
	return a.releaseStoppedCalls(snapshot)
}

func (a *Agent) detachRuntimeForStop() agentStopSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	snapshot := agentStopSnapshot{
		cancel: a.cancel, unsubscribe: a.imsUnsubscribe,
		bridge: a.clientBridge, ims: a.ims, active: a.activeCall,
	}
	seen := make(map[*Call]struct{}, len(a.calls)+1)
	for _, call := range a.calls {
		if call != nil {
			seen[call] = struct{}{}
		}
	}
	if a.activeCall != nil {
		seen[a.activeCall] = struct{}{}
	}
	for call := range seen {
		snapshot.calls = append(snapshot.calls, call)
	}
	a.started = false
	a.cancel = nil
	a.ctx = nil
	a.imsUnsubscribe = nil
	return snapshot
}

func (a *Agent) releaseStoppedCalls(snapshot agentStopSnapshot) error {
	var stopErr error
	for _, call := range snapshot.calls {
		if call == snapshot.active && !call.IsTerminalState() {
			stopErr = errors.Join(stopErr, a.hangupCallForStop(call))
		}
		stopErr = errors.Join(stopErr, a.finalizeActiveCall(call))
		if call.actor != nil {
			call.actor.Stop()
		}
	}
	a.mu.Lock()
	a.activeCall = nil
	a.calls = make(map[string]*Call)
	a.mu.Unlock()
	return stopErr
}

func (a *Agent) hangupCallForStop(call *Call) error {
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	if err := a.hangupCall(ctx, call); err != nil {
		return errors.Join(err, a.forceReleaseCall(call, err))
	}
	return nil
}

func (a *Agent) closeCallDialogForCleanup(call *Call) error {
	ctx, cancel := context.WithTimeout(context.Background(), voiceHangupTimeout)
	defer cancel()
	return a.closeCallDialog(ctx, call)
}

func (a *Agent) reportCallCleanupError(call *Call, err error) {
	if err == nil {
		return
	}
	callID := ""
	if call != nil {
		callID = call.CallID()
	}
	logging.WarnRate("voice-call-cleanup:"+callID, 10*time.Second,
		"voice call cleanup failed", "device", a.deviceID, "call_id", callID, "err", err)
}
