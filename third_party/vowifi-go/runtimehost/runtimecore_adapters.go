package runtimehost

import (
	"context"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/simauth"
	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

type runtimeCoreSIMAdapter struct{ aka enginesim.AKAProvider }

func (adapter runtimeCoreSIMAdapter) EPDGSIMProvider(profile.AuthPlan) enginesim.AKAProvider {
	return adapter.aka
}

func (adapter runtimeCoreSIMAdapter) IMSAKAProvider(profile.AuthPlan) simauth.AKAProvider {
	return adapter.aka
}

func (runtimeCoreSIMAdapter) IMSIdentityProvider() profile.Provider { return nil }

type runtimeCoreEventDispatcher struct{ dispatcher eventhost.Dispatcher }

func runtimeCoreDispatcher(dispatcher eventhost.Dispatcher) events.EventDispatcher {
	if dispatcher == nil {
		return nil
	}
	return runtimeCoreEventDispatcher{dispatcher: dispatcher}
}

func (adapter runtimeCoreEventDispatcher) Dispatch(ctx context.Context, event events.Event) {
	if event == nil {
		return
	}
	if publicEvent := publicRuntimeEvent(event); publicEvent != nil {
		adapter.dispatcher.Dispatch(ctx, publicEvent)
	}
}

type runtimeCoreDeliveryStoreAdapter struct{ store messaging.DeliveryStore }

type runtimeCoreSIPDeliveryStoreAdapter struct {
	runtimeCoreDeliveryStoreAdapter
	store messaging.SIPResultStore
}

func runtimeCoreDeliveryStore(store messaging.DeliveryStore) smsdelivery.Store {
	if store == nil {
		return nil
	}
	base := runtimeCoreDeliveryStoreAdapter{store: store}
	sipResults, ok := store.(messaging.SIPResultStore)
	if !ok {
		return base
	}
	return runtimeCoreSIPDeliveryStoreAdapter{
		runtimeCoreDeliveryStoreAdapter: base,
		store:                           sipResults,
	}
}

func (adapter runtimeCoreSIPDeliveryStoreAdapter) MarkSMSDeliveryPartSIPResult(
	messageID string,
	partNo, sipCode int,
	state, errorText string,
	at time.Time,
) error {
	return adapter.store.MarkSMSDeliveryPartSIPResult(
		messageID, partNo, sipCode, state, errorText, at,
	)
}

func (adapter runtimeCoreDeliveryStoreAdapter) CreateSMSDelivery(
	messageID, imsi, deviceID, peer, content string,
	partsTotal int,
	at time.Time,
) error {
	return adapter.store.CreateSMSDelivery(messageID, imsi, deviceID, peer, content, partsTotal, at)
}

func (adapter runtimeCoreDeliveryStoreAdapter) UpsertSMSDeliveryPart(
	messageID string,
	partNo int,
	callID string,
	rpMR int,
	state string,
	sentAt time.Time,
) error {
	return adapter.store.UpsertSMSDeliveryPart(messageID, partNo, callID, rpMR, state, sentAt)
}

func (adapter runtimeCoreDeliveryStoreAdapter) MarkSMSDeliveryPartReport(
	inReplyTo, callID, deviceID string,
	rpMR int,
	state string,
	sipCode, rpCause int,
	errText string,
	at time.Time,
) (smsdelivery.DeliveryPartMatch, error) {
	match, err := adapter.store.MarkSMSDeliveryPartReport(
		inReplyTo, callID, deviceID, rpMR, state, sipCode, rpCause, errText, at,
	)
	return smsdelivery.DeliveryPartMatch{
		MessageID: match.MessageID, PartNo: match.PartNo, State: match.State, Matched: match.Matched,
	}, err
}

func (adapter runtimeCoreDeliveryStoreAdapter) RecomputeSMSDelivery(
	messageID string,
	at time.Time,
) error {
	return adapter.store.RecomputeSMSDelivery(messageID, at)
}

func (adapter runtimeCoreDeliveryStoreAdapter) UpdateSMSDeliveryState(
	messageID, state, lastError string,
	acks int,
	at time.Time,
) error {
	return adapter.store.UpdateSMSDeliveryState(messageID, state, lastError, acks, at)
}

func (adapter runtimeCoreDeliveryStoreAdapter) GetSMSDeliveryStatus(
	messageID string,
) (*smsdelivery.DeliveryStatus, error) {
	status, err := adapter.store.GetSMSDeliveryStatus(messageID)
	if err != nil || status == nil {
		return nil, err
	}
	return runtimeCoreDeliveryStatus(status), nil
}

func runtimeCoreDeliveryStatus(status *messaging.DeliveryStatus) *smsdelivery.DeliveryStatus {
	result := &smsdelivery.DeliveryStatus{
		MessageID: status.MessageID, IMSI: status.IMSI, DeviceID: status.DeviceID,
		Peer: status.Peer, Content: status.Content, PartsTotal: status.PartsTotal,
		Acks: status.Acks, State: status.State, LastError: status.LastError,
		CreatedAt: status.CreatedAt, UpdatedAt: status.UpdatedAt,
		Parts: make([]smsdelivery.DeliveryPartStatus, 0, len(status.Parts)),
	}
	for _, part := range status.Parts {
		result.Parts = append(result.Parts, runtimeCoreDeliveryPart(part))
	}
	return result
}

func runtimeCoreDeliveryPart(part messaging.DeliveryPartStatus) smsdelivery.DeliveryPartStatus {
	return smsdelivery.DeliveryPartStatus{
		PartNo: part.PartNo, CallID: part.CallID, InReplyTo: part.InReplyTo,
		RPMR: part.RPMR, State: part.State, SIPCode: part.SIPCode,
		RPCause: part.RPCause, RPCauseText: part.RPCauseText, ErrorText: part.ErrorText,
		SentAt: part.SentAt, ReportAt: part.ReportAt,
		CreatedAt: part.CreatedAt, UpdatedAt: part.UpdatedAt,
	}
}
