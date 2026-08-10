package runtimehost

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/smsdelivery"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

var _ events.EventDispatcher = eventDispatcherAdapter{}
var _ events.EventDispatcher = (*eventDispatcherAdapter)(nil)

func TestModuleEventFromInternalPreservesRecoveredFields(t *testing.T) {
	at := time.Date(2026, time.August, 10, 8, 30, 0, 0, time.UTC)
	received := moduleEventFromInternal(events.EventSMSReceived{
		DevID: "dev-1", Sender: "+100", Content: "reply", Time: at,
		TargetURI: "sip:local@example.com", FragmentSessionKey: "fragment-1", Incomplete: true,
	}).(eventhost.SMSReceived)
	if received.DevID != "dev-1" || received.Sender != "+100" || received.Content != "reply" ||
		!received.Time.Equal(at) || received.TargetURI != "sip:local@example.com" ||
		received.FragmentSessionKey != "fragment-1" || !received.Incomplete {
		t.Fatalf("received event = %+v", received)
	}

	local := moduleEventFromInternal(events.EventLocalNumberLearned{
		DevID: "dev-1", IMSI: "00101", Number: "+101", Source: "associated-uri", Time: at,
	}).(eventhost.LocalNumberLearned)
	if local.DeviceID() != "dev-1" || !local.Time.Equal(at) {
		t.Fatalf("local-number event = %+v", local)
	}
	logEvent := moduleEventFromInternal(events.EventLogNotify{DevID: "dev-1", Message: "notice"})
	if logEvent.DeviceID() != "dev-1" || logEvent.Type() != "LogNotify" {
		t.Fatalf("log event = %#v", logEvent)
	}
	generic := moduleEventFromInternal(events.EventSMSDeliveryUpdated{DevID: "dev-1"}).(eventhost.Generic)
	if generic.EventType != "SMSDeliveryUpdated" || generic.TypeName != generic.EventType || generic.DevID != "dev-1" {
		t.Fatalf("generic event = %+v", generic)
	}
}

type dispatchContextKey struct{}

type orderedPublicDispatcher struct {
	types  []string
	values []string
}

func (dispatcher *orderedPublicDispatcher) Dispatch(ctx context.Context, event eventhost.Event) {
	dispatcher.types = append(dispatcher.types, event.Type())
	value, _ := ctx.Value(dispatchContextKey{}).(string)
	dispatcher.values = append(dispatcher.values, value)
}

func TestRuntimeCoreDispatcherPreservesContextAndOrder(t *testing.T) {
	public := &orderedPublicDispatcher{}
	dispatcher := runtimeCoreDispatcher(public)
	ctx := context.WithValue(context.Background(), dispatchContextKey{}, "session-context")
	dispatcher.Dispatch(ctx, events.EventSMSReceived{})
	dispatcher.Dispatch(ctx, events.EventSMSSent{})
	dispatcher.Dispatch(ctx, events.EventLogNotify{})
	if !reflect.DeepEqual(public.types, []string{"SMSReceived", "SMSSent", "LogNotify"}) {
		t.Fatalf("dispatch order = %v", public.types)
	}
	if !reflect.DeepEqual(public.values, []string{"session-context", "session-context", "session-context"}) {
		t.Fatalf("dispatch contexts = %v", public.values)
	}
}

type panicPublicDispatcher struct{}

func (panicPublicDispatcher) Dispatch(context.Context, eventhost.Event) { panic("dispatcher failure") }

func TestEventDispatcherDoesNotHideConsumerFailure(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != "dispatcher failure" {
			t.Fatalf("recovered panic = %#v", recovered)
		}
	}()
	eventDispatcherAdapter{dispatch: panicPublicDispatcher{}}.Dispatch(
		context.Background(), events.EventLogNotify{},
	)
}

func TestDeliveryStoreErrorBridgePreservesBothSentinels(t *testing.T) {
	externalCause := fmt.Errorf("load delivery: %w", messaging.ErrDeliveryNotFound)
	internal := deliveryStoreErrorToInternal(externalCause)
	if !errors.Is(internal, messaging.ErrDeliveryNotFound) ||
		!errors.Is(internal, smsdelivery.ErrDeliveryNotFound) {
		t.Fatalf("internal error = %v", internal)
	}
	external := deliveryStoreErrorFromInternal(internal)
	if !errors.Is(external, messaging.ErrDeliveryNotFound) ||
		!errors.Is(external, smsdelivery.ErrDeliveryNotFound) ||
		!errors.Is(external, externalCause) {
		t.Fatalf("external error = %v", external)
	}
	ordinary := errors.New("database unavailable")
	if deliveryStoreErrorToInternal(ordinary) != ordinary ||
		deliveryStoreErrorFromInternal(ordinary) != ordinary {
		t.Fatal("ordinary delivery error identity changed")
	}
}

type recoveredDeliveryStore struct{}

func (recoveredDeliveryStore) CreateSMSDelivery(string, string, string, string, string, int, time.Time) error {
	return nil
}
func (recoveredDeliveryStore) UpsertSMSDeliveryPart(string, int, string, int, string, time.Time) error {
	return nil
}
func (recoveredDeliveryStore) MarkSMSDeliveryPartReport(
	string, string, string, int, string, int, int, string, time.Time,
) (messaging.DeliveryPartMatch, error) {
	return messaging.DeliveryPartMatch{MessageID: "message-1", PartNo: 1, State: "acked"}, nil
}
func (recoveredDeliveryStore) RecomputeSMSDelivery(string, time.Time) error { return nil }
func (recoveredDeliveryStore) UpdateSMSDeliveryState(string, string, string, int, time.Time) error {
	return nil
}
func (recoveredDeliveryStore) GetSMSDeliveryStatus(string) (*messaging.DeliveryStatus, error) {
	return nil, fmt.Errorf("query delivery: %w", messaging.ErrDeliveryNotFound)
}

func TestProductionDeliveryAdaptersAcceptRecoveredMatchAndMapErrors(t *testing.T) {
	store := recoveredDeliveryStore{}
	adapters := []smsdelivery.Store{newDeliveryStoreAdapter(store), runtimeCoreDeliveryStore(store)}
	for _, adapter := range adapters {
		match, err := adapter.MarkSMSDeliveryPartReport("", "", "", 0, "acked", 0, 0, "", time.Time{})
		if err != nil || !match.Matched || match.MessageID != "message-1" {
			t.Fatalf("adapter %T match = %+v, err %v", adapter, match, err)
		}
		_, err = adapter.GetSMSDeliveryStatus("missing")
		if !errors.Is(err, messaging.ErrDeliveryNotFound) ||
			!errors.Is(err, smsdelivery.ErrDeliveryNotFound) {
			t.Fatalf("adapter %T error = %v", adapter, err)
		}
	}
}

func TestServiceAdapterBuildsRecoveredMessagingStatus(t *testing.T) {
	service := newTestService(t)
	internal := service.StatusSnapshot()
	status := newServiceAdapter(service).messagingStatusSnapshot()
	if status.Enabled != internal.Enabled || status.DeviceID != "dev-1" || !status.IsRegistered() ||
		status.RegStatus != "Registered" || status.Registrar == "" ||
		status.AssociatedMSISDN != "+15551234567" {
		t.Fatalf("messaging status = %+v", status)
	}
}

func TestUSSDResultConversionPreservesRecoveredAndCurrentRawFields(t *testing.T) {
	result := messagingUSSDResult(&imscore.USSDResult{
		Status: 1, Text: "menu", RawXML: "<ussd-data/>", DCS: 15, SessionID: "session-1",
	})
	if result.RawXML != "<ussd-data/>" || result.RawText != result.RawXML ||
		result.Message != "menu" || result.SessionID != "session-1" {
		t.Fatalf("USSD result = %+v", result)
	}
}
