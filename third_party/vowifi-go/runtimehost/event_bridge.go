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
	case events.EventSMSReceived:
		return publicSMSReceived(value)
	case *events.EventSMSReceived:
		if value == nil {
			return nil
		}
		return publicSMSReceived(*value)
	case events.EventSMSSent:
		return publicSMSSent(value)
	case *events.EventSMSSent:
		if value == nil {
			return nil
		}
		return publicSMSSent(*value)
	case events.EventLocalNumberLearned:
		return publicLocalNumberLearned(value)
	case *events.EventLocalNumberLearned:
		if value == nil {
			return nil
		}
		return publicLocalNumberLearned(*value)
	case events.EventLogNotify:
		return eventhost.LogNotify{Message: value.Message}
	case *events.EventLogNotify:
		if value == nil {
			return nil
		}
		return eventhost.LogNotify{Message: value.Message}
	default:
		return eventhost.Generic{DevID: event.DeviceID(), TypeName: event.Type()}
	}
}

func publicSMSReceived(value events.EventSMSReceived) eventhost.SMSReceived {
	return eventhost.SMSReceived{
		DevID: value.DevID, Sender: value.Sender, TargetURI: value.TargetURI,
		Content: value.Content, Time: value.Time,
	}
}

func publicSMSSent(value events.EventSMSSent) eventhost.SMSSent {
	return eventhost.SMSSent{
		DevID: value.DevID, TargetURI: value.TargetURI, Content: value.Content,
		Time: value.Time, TotalParts: value.TotalParts,
	}
}

func publicLocalNumberLearned(value events.EventLocalNumberLearned) eventhost.LocalNumberLearned {
	return eventhost.LocalNumberLearned{
		DevID: value.DevID, IMSI: value.IMSI, Number: value.Number, Source: value.Source,
	}
}
