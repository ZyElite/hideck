package runtimehost

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
)

type instanceObserver struct {
	inst      *Instance
	deviceID  string
	ready     chan struct{}
	readyOnce sync.Once
}

func (observer *instanceObserver) OnRuntimeEvent(
	ctx context.Context,
	event runtimecore.RuntimeEvent[*runtimecore.SessionResult],
) {
	if observer == nil || observer.inst == nil {
		return
	}
	kind := recoveredEventKind(event.Kind)
	state := observer.inst.State()
	if state.DeviceID == "" {
		state.DeviceID = observer.deviceID
	}
	if strings.TrimSpace(event.DeviceID) != "" {
		state.DeviceID = strings.TrimSpace(event.DeviceID)
	}
	if strings.TrimSpace(event.RedirectEPDG) != "" {
		state.LastRedirectEPDG = strings.TrimSpace(event.RedirectEPDG)
	}
	observer.applyEvent(kind, event, &state)
	state.LastEvent = kind
	state.UpdatedAt = time.Now()
	observer.inst.setState(state)
	observer.inst.publish(ctx, Event{
		Kind: kind, DeviceID: state.DeviceID, TraceID: event.TraceID,
		Reason: event.Reason, Attempt: event.Attempt, RetryDelay: event.RetryDelay,
		RedirectEPDG: event.RedirectEPDG, State: state,
		Type: kind, Detail: event.Reason, Session: observer.inst,
	})
}

func (observer *instanceObserver) applyEvent(
	kind string,
	event runtimecore.RuntimeEvent[*runtimecore.SessionResult],
	state *State,
) {
	switch kind {
	case "prepared":
		state.Phase = PhaseSIMReady
		state.SIMReady = true
	case "ipsec_up":
		observer.installSession(event)
		state.Phase = readyPhase(*state)
		state.SessionState = "established"
		state.TunnelReady = event.Snapshot.Established || event.Handle != nil
		state.DataPlaneUp = state.TunnelReady
		if observer.ready != nil {
			observer.readyOnce.Do(func() { close(observer.ready) })
		}
	case "ims_registered":
		observer.installService(event)
		state.Phase = "ims_ready"
		state.IMSState = "registered"
		state.IMSReady = true
		state.RegStatus = 1
		state.RegStatusText = "registered"
	case "sms_ready":
		state.Phase = "sms_ready"
		state.SMSReady = true
	case "interrupted":
		state.Phase = "interrupted"
		state.TunnelReady = false
		state.IMSReady = false
		state.SMSReady = false
		state.DataPlaneUp = false
		state.LastReason = strings.TrimSpace(event.Reason)
		state.LastRedirectEPDG = strings.TrimSpace(event.RedirectEPDG)
	case "retrying":
		state.Phase = "retrying"
		state.LastReason = strings.TrimSpace(event.Reason)
	case "terminal_error":
		state.Phase = "error"
		state.SessionState = "error"
		state.LastErrorClass = "runtime"
		state.LastError = firstNonEmptyString(event.Message, event.Reason)
		state.Error = state.LastError
	case "stopped":
		observer.inst.setService(nil)
		observer.inst.setSession(nil)
		state.Phase = "stopped"
		state.SessionState = "stopped"
		state.TunnelReady = false
		state.IMSReady = false
		state.SMSReady = false
		state.DataPlaneUp = false
	}
}

func readyPhase(state State) string {
	if state.SMSReady {
		return "sms_ready"
	}
	if state.IMSReady {
		return "ims_ready"
	}
	return "ipsec_up"
}

func (observer *instanceObserver) installSession(
	event runtimecore.RuntimeEvent[*runtimecore.SessionResult],
) {
	observer.inst.setSession(event.Handle)
	observer.installService(event)
}

func (observer *instanceObserver) installService(
	event runtimecore.RuntimeEvent[*runtimecore.SessionResult],
) {
	service := event.Service
	if service == nil && event.Handle != nil {
		service = event.Handle.IMSService
	}
	if service != nil {
		observer.inst.setService(newServiceAdapter(service))
	}
}

func recoveredEventKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "established":
		return "ipsec_up"
	case "retry":
		return "retrying"
	case "error":
		return "terminal_error"
	default:
		return strings.TrimSpace(kind)
	}
}
