package runtimehost

import (
	"context"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

type imsEventBridge struct {
	dispatcher eventhost.Dispatcher
}

func (b *imsEventBridge) OnIMSEvent(event events.Event) {
	if b == nil || b.dispatcher == nil || event == nil {
		return
	}
	if publicEvent := publicRuntimeEvent(event); publicEvent != nil {
		b.dispatcher.Dispatch(context.Background(), publicEvent)
	}
}

func publicRuntimeEvent(event events.Event) eventhost.Event {
	switch value := event.(type) {
	case *events.EventSMSReceived:
		return eventhost.SMSReceived{
			DevID: value.DevID, Sender: value.Sender, TargetURI: value.TargetURI,
			Content: value.Content, Time: value.Time,
		}
	case *events.EventSMSSent:
		return eventhost.SMSSent{
			DevID: value.DevID, TargetURI: value.TargetURI, Content: value.Content,
			Time: value.Time, TotalParts: value.TotalParts,
		}
	case *events.EventLocalNumberLearned:
		return eventhost.LocalNumberLearned{
			DevID: value.DevID, IMSI: value.IMSI, Number: value.Number, Source: value.Source,
		}
	case *events.EventLogNotify:
		return eventhost.LogNotify{Message: value.Message}
	default:
		return eventhost.Generic{DevID: event.DeviceID(), TypeName: event.Type()}
	}
}
