package voicehost

import (
	"context"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

func (g *Gateway) SetEventDispatcher(dispatcher eventhost.Dispatcher) {
	if g == nil || g.inner == nil {
		return
	}
	if dispatcher == nil {
		g.inner.SetEventDispatcher(nil)
		return
	}
	g.inner.SetEventDispatcher(eventDispatcherAdapter{dispatch: dispatcher})
}

type eventDispatcherAdapter struct {
	dispatch eventhost.Dispatcher
}

func (adapter eventDispatcherAdapter) Dispatch(ctx context.Context, event events.Event) {
	if adapter.dispatch != nil {
		adapter.dispatch.Dispatch(ctx, event)
	}
}
