package imscore

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

type legacyIMSServiceSurface interface {
	GetIMPU() string
	GetIMSContextSnapshot() sipkit.IMSRuntimeSnapshot
	GetLocalPorts() (int, int)
	GetRemotePorts() (int, int)
	GetServiceRoute() string
	GetSpiPairs() (uint32, uint32, uint32, uint32)
	ListenPacket(context.Context, string, net.Addr) (net.PacketConn, error)
	Session() *imsendpoint.Session
	Snapshot() imsendpoint.Snapshot
	Status() map[string]interface{}
	StatusSnapshot() ServiceStatus
	Stop(context.Context) error
	Subscribe(imsendpoint.EventSubscription, func(imsendpoint.Event)) func()
	TriggerFastReconnect(string)
	TriggerRegisterImmediate(string)
	UpdateLastPingAt()
	VoiceProfile() imsendpoint.VoiceProfile
}

var _ legacyIMSServiceSurface = (*Service)(nil)
var _ imsendpoint.Endpoint = (*Service)(nil)

func TestIMSEndpointRecoveredTypeFieldOrder(t *testing.T) {
	assertStructFields(t, reflect.TypeOf(imsendpoint.EventSubscription{}),
		[]string{"Name", "QueueSize", "Workers", "Match"})
	assertStructFields(t, reflect.TypeOf(imsendpoint.Event{}), []string{
		"DeviceID", "Kind", "Method", "CSeqMethod", "CallID", "StatusCode", "Session",
		"Request", "Response", "InboundRequest", "ServerInvite", "ResponseAcknowledged",
	})
	assertStructFields(t, reflect.TypeOf(imsendpoint.Session{}), []string{
		"SignalingConn", "LocalIP", "LocalPortC", "RemoteIP", "RemotePortS", "TransportMode",
		"ServiceRoute", "Path", "SecVerify", "SecMode", "RouteSet", "IMPU", "IMPI", "Domain",
		"Realm", "MSISDN", "Registered",
	})
	assertStructFields(t, reflect.TypeOf(imsendpoint.Snapshot{}), []string{
		"IMPU", "Realm", "ContactID", "ServiceRoute", "SecVerify", "EffectiveSecMode",
		"PAccessNetworkInfo", "UserAgent", "LocalAddr", "LocalPortC", "LocalPortS",
		"RemotePortC", "RemotePortS", "LocalSpiC", "LocalSpiS", "RemoteSpiC", "RemoteSpiS",
		"Transport", "IMEI", "PubGRUU", "Voice",
	})
	assertStructFields(t, reflect.TypeOf(imsendpoint.VoiceProfile{}), []string{
		"SupportedHeader", "AllowHeader", "AcceptContact", "PPreferredService", "AccessType",
		"ContactParamOrder",
	})
}

func assertStructFields(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	if typ.NumField() != len(want) {
		t.Fatalf("%s fields = %d, want %d", typ, typ.NumField(), len(want))
	}
	for index, name := range want {
		if got := typ.Field(index).Name; got != name {
			t.Fatalf("%s field %d = %s, want %s", typ, index, got, name)
		}
	}
}

func TestIMSEventBusBackpressureSnapshotAndUnsubscribe(t *testing.T) {
	bus := newIMSEventBus("dev-events")
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	unsubscribe := bus.subscribe(imsendpoint.EventSubscription{
		Name: "slow", QueueSize: 1, Workers: 1,
	}, func(imsendpoint.Event) {
		enteredOnce.Do(func() { close(entered) })
		<-release
	})

	event := imsendpoint.Event{Kind: "request", Method: "MESSAGE"}
	if _, enqueued, full := bus.publish(event); enqueued != 1 || full != 0 {
		t.Fatalf("first publish enqueued=%d full=%d", enqueued, full)
	}
	<-entered
	bus.publish(event)
	if matched, enqueued, full := bus.publish(event); matched != 1 || enqueued != 0 || full != 1 {
		t.Fatalf("full publish matched=%d enqueued=%d full=%d", matched, enqueued, full)
	}

	counts, subscriptions := bus.snapshot()
	if counts["request:MESSAGE"] != 3 || len(subscriptions) != 1 {
		t.Fatalf("snapshot counts=%v subscriptions=%+v", counts, subscriptions)
	}
	snapshot := subscriptions[0]
	if snapshot.Name != "slow" || snapshot.Workers != 1 || snapshot.Matched != 3 ||
		snapshot.Enqueued != 2 || snapshot.QueueFull != 1 {
		t.Fatalf("subscription snapshot = %+v", snapshot)
	}

	done := make(chan struct{})
	go func() {
		unsubscribe()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("unsubscribe returned before the worker drained")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unsubscribe did not wait for worker shutdown")
	}
	if _, subscriptions = bus.snapshot(); len(subscriptions) != 0 {
		t.Fatalf("subscriptions after unsubscribe = %+v", subscriptions)
	}
}

func TestIMSEventBusRecoversMatcherAndHandlerPanics(t *testing.T) {
	bus := newIMSEventBus("dev-panic")
	unsubscribeMatcher := bus.subscribe(imsendpoint.EventSubscription{
		Name: "matcher", Match: func(imsendpoint.Event) bool { panic("matcher") },
	}, func(imsendpoint.Event) { t.Error("panicking matcher delivered an event") })
	handled := make(chan struct{}, 1)
	unsubscribeHandler := bus.subscribe(imsendpoint.EventSubscription{Name: "handler"}, func(imsendpoint.Event) {
		handled <- struct{}{}
		panic("handler")
	})
	bus.publish(imsendpoint.Event{Kind: "response", CSeqMethod: "OPTIONS"})
	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("handler did not receive event after matcher panic")
	}
	unsubscribeMatcher()
	unsubscribeHandler()
}

func TestServiceStopClosesIMSEventBus(t *testing.T) {
	service := mustEventTestService(t)
	received := make(chan imsendpoint.Event, 1)
	service.Subscribe(imsendpoint.EventSubscription{}, func(event imsendpoint.Event) {
		received <- event
	})
	_, active := service.getIMSEventBus().snapshot()
	if len(active) != 1 || active[0].Name != defaultIMSEventSubscriptionName ||
		active[0].QueueCap != defaultIMSEventQueueSize || active[0].Workers != defaultIMSEventWorkers {
		t.Fatalf("default subscription = %+v", active)
	}
	service.publishIMSEvent(imsendpoint.Event{Kind: "request", Method: "OPTIONS"})
	receiveEndpointEvent(t, received)
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	counts, subscriptions := service.getIMSEventBus().snapshot()
	if counts["request:OPTIONS"] != 1 || len(subscriptions) != 0 {
		t.Fatalf("closed bus counts=%v subscriptions=%+v", counts, subscriptions)
	}
	service.Subscribe(imsendpoint.EventSubscription{}, func(imsendpoint.Event) {
		t.Error("closed bus accepted a subscriber")
	})
	if matched, enqueued, full := service.getIMSEventBus().publish(imsendpoint.Event{}); matched != 0 || enqueued != 0 || full != 0 {
		t.Fatalf("publish after close = %d/%d/%d", matched, enqueued, full)
	}
}

func TestServiceStopCleansUpAfterContextCancellation(t *testing.T) {
	service := mustEventTestService(t)
	service.Subscribe(imsendpoint.EventSubscription{}, func(imsendpoint.Event) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop error = %v, want context.Canceled", err)
	}
	_, subscriptions := service.getIMSEventBus().snapshot()
	if len(subscriptions) != 0 {
		t.Fatalf("subscriptions after canceled Stop = %+v", subscriptions)
	}
}

func TestProductionSIPDispatchPublishesEndpointEvents(t *testing.T) {
	service := mustEventTestService(t)
	events := make(chan imsendpoint.Event, 2)
	unsubscribe := service.Subscribe(imsendpoint.EventSubscription{Name: "wire"}, func(event imsendpoint.Event) {
		events <- event
	})
	t.Cleanup(unsubscribe)

	request := "OPTIONS sip:ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-event\r\n" +
		"From: <sip:a@ims.example>;tag=from\r\nTo: <sip:b@ims.example>\r\n" +
		"Call-ID: request-call\r\nCSeq: 7 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	if err := service.dispatchInboundSIP(request, func(string) error { return nil }); err != nil {
		t.Fatalf("dispatch request: %v", err)
	}
	response := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-response\r\n" +
		"From: <sip:a@ims.example>;tag=from\r\nTo: <sip:b@ims.example>;tag=to\r\n" +
		"Call-ID: response-call\r\nCSeq: 9 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	if err := service.dispatchInboundSIP(response, nil); err != nil {
		t.Fatalf("dispatch response: %v", err)
	}

	requestEvent := receiveEndpointEvent(t, events)
	if requestEvent.Kind != "request" || requestEvent.Method != "OPTIONS" ||
		requestEvent.CSeqMethod != "OPTIONS" || requestEvent.CallID != "request-call" ||
		requestEvent.Request == nil || requestEvent.Session == nil || !requestEvent.ResponseAcknowledged ||
		requestEvent.InboundRequest != nil || requestEvent.ServerInvite != nil {
		t.Fatalf("request event = %+v", requestEvent)
	}
	responseEvent := receiveEndpointEvent(t, events)
	if responseEvent.Kind != "response" || responseEvent.Method != "" ||
		responseEvent.CSeqMethod != "OPTIONS" || responseEvent.CallID != "response-call" ||
		responseEvent.StatusCode != 200 || responseEvent.Response == nil {
		t.Fatalf("response event = %+v", responseEvent)
	}
	status := service.StatusSnapshot()
	counts := status.IMSEventBus["publish_counts"].(map[string]uint64)
	if counts["request:OPTIONS"] != 1 || counts["response:OPTIONS"] != 1 {
		t.Fatalf("published event counts = %v", counts)
	}
}

func TestProductionInviteEventCarriesServerHandle(t *testing.T) {
	service := mustEventTestService(t)
	events := make(chan imsendpoint.Event, 1)
	unsubscribe := service.Subscribe(imsendpoint.EventSubscription{Name: "invite"}, func(event imsendpoint.Event) {
		events <- event
	})
	t.Cleanup(unsubscribe)
	request := "INVITE sip:user@ims.example SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK-invite\r\n" +
		"From: <sip:a@ims.example>;tag=from\r\nTo: <sip:user@ims.example>\r\n" +
		"Call-ID: invite-call\r\nCSeq: 1 INVITE\r\nContent-Length: 0\r\n\r\n"
	if err := service.dispatchInboundSIP(request, func(string) error { return nil }); err != nil {
		t.Fatalf("dispatch INVITE: %v", err)
	}
	event := receiveEndpointEvent(t, events)
	if event.ServerInvite == nil || event.InboundRequest != nil || event.ResponseAcknowledged {
		t.Fatalf("INVITE event = %+v", event)
	}
}

func mustEventTestService(t *testing.T) *Service {
	t.Helper()
	service, err := New(&IMSConfig{
		DeviceID: "dev-events", IMPI: "user@ims.example", IMPU: "sip:user@ims.example",
		Domain: "ims.example", Realm: "ims.example", LocalIP: net.ParseIP("192.0.2.10"),
		LocalPort: 5060, EnableIPSec3GPP: disabledBoolPointer(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(service.StopCurrent)
	return service
}

func receiveEndpointEvent(t *testing.T, events <-chan imsendpoint.Event) imsendpoint.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for endpoint event")
		return imsendpoint.Event{}
	}
}
