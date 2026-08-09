package runtimecore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
)

type eventDispatcherSubscriber struct {
	ctx      context.Context
	dispatch events.EventDispatcher
}

func (subscriber eventDispatcherSubscriber) OnIMSEvent(event events.Event) {
	if subscriber.dispatch != nil {
		subscriber.dispatch.Dispatch(subscriber.ctx, event)
	}
}

func buildEventBus(ctx context.Context, dispatch events.EventDispatcher) *imscore.EventBus {
	bus := imscore.NewEventBus()
	if dispatch != nil {
		bus.Subscribe(eventDispatcherSubscriber{ctx: ctx, dispatch: dispatch})
	}
	return bus
}

type deliveryStoreAdapter struct{ store smsdelivery.Store }

type deliveryStoreSIPAdapter struct {
	deliveryStoreAdapter
	store smsdelivery.SIPResultStore
}

func adaptDeliveryStore(store smsdelivery.Store) imscore.DeliveryStore {
	if store == nil {
		return nil
	}
	base := deliveryStoreAdapter{store: store}
	sipResults, ok := store.(smsdelivery.SIPResultStore)
	if !ok {
		return base
	}
	return deliveryStoreSIPAdapter{deliveryStoreAdapter: base, store: sipResults}
}

func (adapter deliveryStoreSIPAdapter) MarkSMSDeliveryPartSIPResult(
	messageID string,
	partNo, sipCode int,
	state, errorText string,
	at time.Time,
) error {
	return adapter.store.MarkSMSDeliveryPartSIPResult(
		messageID, partNo, sipCode, state, errorText, at,
	)
}

func (adapter deliveryStoreAdapter) CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, parts int, at time.Time) error {
	return adapter.store.CreateSMSDelivery(messageID, imsi, deviceID, peer, content, parts, at)
}

func (adapter deliveryStoreAdapter) UpsertSMSDeliveryPart(messageID string, part int, callID string, rpMR int, state string, at time.Time) error {
	return adapter.store.UpsertSMSDeliveryPart(messageID, part, callID, rpMR, state, at)
}

func (adapter deliveryStoreAdapter) MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode, rpCause int, errorText string, at time.Time) (imscore.DeliveryPartMatch, error) {
	match, err := adapter.store.MarkSMSDeliveryPartReport(
		inReplyTo, callID, deviceID, rpMR, state, sipCode, rpCause, errorText, at,
	)
	return imscore.DeliveryPartMatch{MessageID: match.MessageID, PartNo: match.PartNo, State: match.State, Matched: err == nil && match.MessageID != ""}, err
}

func (adapter deliveryStoreAdapter) RecomputeSMSDelivery(id string, at time.Time) error {
	return adapter.store.RecomputeSMSDelivery(id, at)
}

func (adapter deliveryStoreAdapter) UpdateSMSDeliveryState(messageID, state, lastError string, acknowledgements int, at time.Time) error {
	return adapter.store.UpdateSMSDeliveryState(messageID, state, lastError, acknowledgements, at)
}

func (adapter deliveryStoreAdapter) GetSMSDeliveryStatus(id string) (*imscore.DeliveryStatus, error) {
	status, err := adapter.store.GetSMSDeliveryStatus(id)
	if err != nil || status == nil {
		return nil, err
	}
	result := &imscore.DeliveryStatus{
		MessageID: status.MessageID, IMSI: status.IMSI, DeviceID: status.DeviceID,
		Peer: status.Peer, Content: status.Content, PartsTotal: status.PartsTotal,
		Acks: status.Acks, State: status.State, LastError: status.LastError,
	}
	for _, part := range status.Parts {
		result.Parts = append(result.Parts, imscore.DeliveryPartStatus{
			PartNo: part.PartNo, CallID: part.CallID, State: part.State,
			SIPCode: part.SIPCode, RPCause: part.RPCause,
		})
	}
	return result, nil
}

type installerIMSNetwork struct {
	imscore.IMSNetwork
	mu        sync.Mutex
	installer imscore.IPSec3GPPInstaller
	cleanup   func() error
}

func newInstallerIMSNetwork(
	network imscore.IMSNetwork,
	installer imscore.IPSec3GPPInstaller,
) *installerIMSNetwork {
	return &installerIMSNetwork{IMSNetwork: network, installer: installer}
}

func (network *installerIMSNetwork) InstallIPSec3GPP(value ipsec3gpp.Policy) error {
	network.mu.Lock()
	defer network.mu.Unlock()
	if network.installer == nil {
		_, err := (&imscore.MissingIPSec3GPPInstaller{}).InstallIPSec3GPP(
			context.Background(), value,
		)
		return err
	}
	previous := network.cleanup
	network.cleanup = nil
	if previous != nil {
		if err := previous(); err != nil {
			return err
		}
	}
	cleanup, err := network.installer.InstallIPSec3GPP(context.Background(), value)
	if err != nil {
		return err
	}
	network.cleanup = cleanup
	return nil
}

func (network *installerIMSNetwork) Close() error {
	network.mu.Lock()
	cleanup := network.cleanup
	network.cleanup = nil
	network.mu.Unlock()
	var cleanupErr error
	if cleanup != nil {
		cleanupErr = cleanup()
	}
	closer, _ := network.IMSNetwork.(interface{ Close() error })
	if closer == nil {
		return cleanupErr
	}
	return errors.Join(cleanupErr, closer.Close())
}
