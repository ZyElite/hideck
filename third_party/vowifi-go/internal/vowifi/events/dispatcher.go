package events

import "context"

type EventDispatcher interface {
	Dispatch(context.Context, Event)
}
