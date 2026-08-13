package voicehost

import (
	"context"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

func (g *Gateway) SetEventDispatcher(dispatcher eventhost.Dispatcher) {
	if g == nil || g.inner == nil {
		return
	}
	g.mu.Lock()
	g.eventDispatcher = dispatcher
	hasSubscribers := len(g.callEventSubscribers) > 0
	g.mu.Unlock()
	if dispatcher == nil && !hasSubscribers {
		g.inner.SetEventDispatcher(nil)
		return
	}
	g.inner.SetEventDispatcher(eventDispatcherAdapter{dispatch: dispatcher, gateway: g})
}

// SubscribeCallEvents adds a buffered call lifecycle subscription.
func (g *Gateway) SubscribeCallEvents(handler func(CallEvent)) func() {
	if g == nil || handler == nil {
		return func() {}
	}
	subscription := newCallEventSubscription(handler)
	g.mu.Lock()
	g.nextSubscriptionID++
	id := g.nextSubscriptionID
	if g.callEventSubscribers == nil {
		g.callEventSubscribers = make(map[uint64]*callEventSubscription)
	}
	g.callEventSubscribers[id] = subscription
	dispatcher := g.eventDispatcher
	g.mu.Unlock()
	if g.inner != nil {
		g.inner.SetEventDispatcher(eventDispatcherAdapter{dispatch: dispatcher, gateway: g})
	}
	return func() {
		g.mu.Lock()
		if current := g.callEventSubscribers[id]; current == subscription {
			delete(g.callEventSubscribers, id)
		}
		hasSubscribers := len(g.callEventSubscribers) > 0
		dispatcher := g.eventDispatcher
		g.mu.Unlock()
		subscription.close()
		if g.inner != nil && !hasSubscribers && dispatcher == nil {
			g.inner.SetEventDispatcher(nil)
		}
	}
}

func (g *Gateway) publishCallEvent(event CallEvent) {
	g.mu.RLock()
	subscribers := make([]*callEventSubscription, 0, len(g.callEventSubscribers))
	for _, subscription := range g.callEventSubscribers {
		subscribers = append(subscribers, subscription)
	}
	g.mu.RUnlock()
	for _, subscription := range subscribers {
		subscription.enqueue(event)
	}
}

type eventDispatcherAdapter struct {
	dispatch eventhost.Dispatcher
	gateway  *Gateway
}

func (adapter eventDispatcherAdapter) Dispatch(ctx context.Context, event events.Event) {
	if callEvent, ok := callEventFromInternal(event); ok && adapter.gateway != nil {
		adapter.gateway.publishCallEvent(callEvent)
	}
	if adapter.dispatch != nil {
		go adapter.dispatch.Dispatch(ctx, event)
	}
}

func callEventFromInternal(event events.Event) (CallEvent, bool) {
	if event == nil {
		return CallEvent{}, false
	}
	result := CallEvent{Type: event.Type(), DeviceID: event.DeviceID(), Time: time.Now()}
	if applyCallProgressEvent(&result, event) || applyCallTerminalEvent(&result, event) || applyCallAuxiliaryEvent(&result, event) {
		return result, true
	}
	return CallEvent{}, false
}

func applyCallProgressEvent(result *CallEvent, event events.Event) bool {
	switch value := event.(type) {
	case events.EventIncomingCall:
		applyIncomingEvent(result, value)
	case *events.EventIncomingCall:
		applyIncomingEvent(result, *value)
	case events.EventCallRinging:
		result.CallID, result.Time = value.CallID, eventTime(value.Time)
	case *events.EventCallRinging:
		result.CallID, result.Time = value.CallID, eventTime(value.Time)
	case events.EventCallAnswered:
		result.CallID, result.Time = value.CallID, eventTime(value.Time, value.AnsweredAt)
	case *events.EventCallAnswered:
		result.CallID, result.Time = value.CallID, eventTime(value.Time, value.AnsweredAt)
	default:
		return false
	}
	return true
}

func applyCallTerminalEvent(result *CallEvent, event events.Event) bool {
	switch value := event.(type) {
	case events.EventCallEnded:
		result.CallID, result.Reason, result.Time = value.CallID, value.Reason, eventTime(value.Time, value.EndedAt)
	case *events.EventCallEnded:
		result.CallID, result.Reason, result.Time = value.CallID, value.Reason, eventTime(value.Time, value.EndedAt)
	case events.EventCallFailed:
		result.CallID, result.Reason, result.Time = value.CallID, value.Reason, eventTime(value.Time)
	case *events.EventCallFailed:
		result.CallID, result.Reason, result.Time = value.CallID, value.Reason, eventTime(value.Time)
	case events.EventCallCanceled:
		result.CallID, result.Reason, result.Time = value.CallID, value.Reason, eventTime(value.Time)
	case *events.EventCallCanceled:
		result.CallID, result.Reason, result.Time = value.CallID, value.Reason, eventTime(value.Time)
	default:
		return false
	}
	return true
}

func applyCallAuxiliaryEvent(result *CallEvent, event events.Event) bool {
	switch value := event.(type) {
	case events.EventCallMediaUpdated:
		result.CallID, result.Direction, result.State, result.Time = value.CallID, value.Direction, value.State, eventTime(value.Time)
	case *events.EventCallMediaUpdated:
		result.CallID, result.Direction, result.State, result.Time = value.CallID, value.Direction, value.State, eventTime(value.Time)
	case events.EventCallBusy:
		result.CallID, result.Caller, result.Callee, result.Time = value.CallID, value.Caller, value.Callee, eventTime(value.Time)
	case *events.EventCallBusy:
		result.CallID, result.Caller, result.Callee, result.Time = value.CallID, value.Caller, value.Callee, eventTime(value.Time)
	case events.EventCallFinalized:
		applyFinalizedEvent(result, value)
	case *events.EventCallFinalized:
		applyFinalizedEvent(result, *value)
	default:
		return false
	}
	return true
}

func applyIncomingEvent(result *CallEvent, value events.EventIncomingCall) {
	result.CallID, result.Caller, result.Callee = value.CallID, value.Caller, value.Callee
	result.Time = eventTime(value.Time, value.ReceivedAt)
}

func applyFinalizedEvent(result *CallEvent, value events.EventCallFinalized) {
	result.CallID, result.PCAPPath, result.AudioPath = value.CallID, value.PCAPPath, value.AudioPath
	result.AudioCodec, result.RecordingError = value.AudioCodec, value.RecordingError
	result.Time = eventTime(value.Time)
}

func eventTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Now()
}
