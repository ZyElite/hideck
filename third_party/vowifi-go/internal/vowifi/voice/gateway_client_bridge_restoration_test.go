package voice

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/events"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
)

var _ func(string, imsendpoint.Endpoint, *Gateway) *Agent = NewAgent

var _ interface {
	Start(context.Context) error
	Stop() error
	ReplaceIMSProvider(imsendpoint.Endpoint) error
	HandleClientBye(*sip.Request, sip.ServerTransaction)
	Snapshot() map[string]interface{}
} = (*Agent)(nil)

var _ interface {
	Start(context.Context) error
	Stop() error
	GetAgent(string) *Agent
	SetNotifier(CallNotifier)
	GetNotifier() CallNotifier
	SetEventDispatcher(events.EventDispatcher)
	SetClientAdapter(voiceclient.Adapter)
	GetClientAdapter() voiceclient.Adapter
	RegisterDevice(string, imsendpoint.Endpoint) error
	UnregisterDevice(string)
	DeviceStatus(string) map[string]interface{}
	OnIMSInvite(string, []byte, *imsendpoint.Session)
	OnIMSBye(string, []byte, *imsendpoint.Session)
	OnIMSUpdate(string, []byte, *imsendpoint.Session)
	OnIMSCancel(string, []byte, *imsendpoint.Session)
	HandleClientInvite(string, *sip.Request, sip.ServerTransaction)
	HandleClientCancel(string, *sip.Request, sip.ServerTransaction)
	HandleClientPrack(string, *sip.Request, sip.ServerTransaction)
	HandleClientAck(string, *sip.Request)
	HandleClientBye(string, *sip.Request, sip.ServerTransaction)
	SimulateCall(context.Context, string, SimulateCallRequest) (*SimulateCallResult, error)
	StartPCAP(string, string) error
	StopPCAP(string) error
} = (*Gateway)(nil)

type capturedCallNotifier struct {
	calls chan [3]string
}

func (n *capturedCallNotifier) NotifyIncomingCall(deviceID, caller, callee string) {
	n.calls <- [3]string{deviceID, caller, callee}
}

type capturedEventDispatcher struct {
	events chan events.Event
}

func (d *capturedEventDispatcher) Dispatch(_ context.Context, event events.Event) {
	d.events <- event
}

func TestGatewayRoutesRealClientDialogByDevice(t *testing.T) {
	agent := newTestAgent(t)
	gateway := NewGateway(agent)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer gateway.Stop()

	inviteTx := newVoiceServerTransaction()
	invite := mustClientRequest(t, sip.INVITE, "gateway-client-route", testClientSDP, "")
	gateway.HandleClientInvite(agent.DeviceID(), invite, inviteTx)
	waitVoiceResponse(t, inviteTx, 200)
	call := agent.ActiveCall()
	if call == nil {
		t.Fatal("gateway INVITE did not create the device call")
	}

	gateway.HandleClientAck(agent.DeviceID(), mustClientRequest(
		t, sip.ACK, "gateway-client-route", "", "",
	))
	byeTx := newVoiceServerTransaction()
	gateway.HandleClientBye(agent.DeviceID(), mustClientRequest(
		t, sip.BYE, "gateway-client-route", "", "",
	), byeTx)
	waitVoiceResponse(t, byeTx, 200)
	select {
	case <-call.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway BYE did not close the real IMS dialog")
	}
}

func TestGatewayRegisterDeviceAndStopLifecycle(t *testing.T) {
	endpointOwner := newTestAgent(t)
	gateway := &Gateway{}
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterDevice("dev-1", endpointOwner.endpoint); err != nil {
		t.Fatal(err)
	}
	agent := gateway.GetAgent("dev-1")
	if agent == nil || agent == endpointOwner {
		t.Fatal("RegisterDevice did not create an endpoint-owned Agent")
	}
	state, ok := gateway.DeviceStatus("dev-1")["state"].(map[string]interface{})
	if !ok || state["running"] != true || state["active_call"] != false {
		t.Fatalf("device state = %#v", state)
	}

	gateway.UnregisterDevice("dev-1")
	if gateway.GetAgent("dev-1") != nil || agent.Snapshot()["running"] != false {
		t.Fatal("UnregisterDevice retained a running Agent")
	}
	if err := gateway.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentSnapshotRetainsRecoveredActiveCallFields(t *testing.T) {
	agent := NewAgent("snapshot-device", nil, nil)
	call := NewCall(agent, 1, "snapshot-trace", "+441234")
	call.TraceID = "snapshot-trace"
	call.DialogState.CallerID = "+441234"
	call.DialogState.CalleeID = "+449999"
	call.DialogState.IMSCallID = "ims-inbound"
	call.DialogState.OutboundIMSCallID = "ims-outbound"
	call.clientCallID = "client-call"
	call.SetStartTime(time.Unix(10, 0))
	agent.mu.Lock()
	agent.activeCall = call
	agent.mu.Unlock()

	state := agent.Snapshot()
	want := map[string]interface{}{
		"running": false, "device_id": "snapshot-device", "active_call": true,
		"trace_id": "snapshot-trace", "direction": 1, "state": call.State,
		"caller": "+441234", "callee": "+449999", "ims_call_id": "ims-inbound",
		"outbound_ims_call_id": "ims-outbound", "client_call_id": "client-call",
		"started_at": time.Unix(10, 0),
	}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("Snapshot() = %#v, want %#v", state, want)
	}
}

func TestGatewayReturnsProtocolErrorsWhenRouteUnavailable(t *testing.T) {
	gateway := NewGateway(nil)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := mustClientRequest(t, sip.INVITE, "missing-device", testClientSDP, "")
	missingTx := newVoiceServerTransaction()
	gateway.HandleClientInvite("missing", request, missingTx)
	waitVoiceResponse(t, missingTx, 500)
	if err := gateway.Stop(); err != nil {
		t.Fatal(err)
	}

	seeded := NewGateway(NewAgent("stopped-device", nil, nil))
	queueTx := newVoiceServerTransaction()
	seeded.HandleClientPrack("stopped-device", mustClientRequest(
		t, sip.PRACK, "stopped-route", "", "RAck: 1 1 INVITE\r\n",
	), queueTx)
	waitVoiceResponse(t, queueTx, 503)
}

func TestGatewaySerializesDispatchAgainstUnregister(t *testing.T) {
	agent := NewAgent("race-device", nil, nil)
	gateway := NewGateway(agent)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := mustClientRequest(t, sip.ACK, "race-call", "", "")
	var calls sync.WaitGroup
	calls.Add(2)
	go func() {
		defer calls.Done()
		for index := 0; index < 100; index++ {
			gateway.HandleClientAck("race-device", request)
		}
	}()
	go func() {
		defer calls.Done()
		gateway.UnregisterDevice("race-device")
	}()
	calls.Wait()
	if gateway.GetAgent("race-device") != nil {
		t.Fatal("concurrent unregister retained the Agent")
	}
	if err := gateway.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayProjectsAgentEventsToBothLegacySinks(t *testing.T) {
	agent := NewAgent("event-device", nil, nil)
	gateway := NewGateway(agent)
	notifier := &capturedCallNotifier{calls: make(chan [3]string, 1)}
	dispatcher := &capturedEventDispatcher{events: make(chan events.Event, 1)}
	gateway.SetNotifier(notifier)
	gateway.SetEventDispatcher(dispatcher)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer gateway.Stop()

	call := NewCall(agent, 0, "incoming-event", "+441234")
	call.DialogState.CalleeID = "+449999"
	agent.mu.Lock()
	agent.calls[call.CallID()] = call
	agent.activeCall = call
	agent.mu.Unlock()
	agent.emitIncomingCall(call)
	select {
	case values := <-notifier.calls:
		if values != [3]string{"event-device", "+441234", "+449999"} {
			t.Fatalf("notifier values = %#v", values)
		}
	case <-time.After(time.Second):
		t.Fatal("legacy incoming-call notifier was not called")
	}
	select {
	case event := <-dispatcher.events:
		if event.Type() != "IncomingCall" || event.DeviceID() != "event-device" {
			t.Fatalf("dispatched event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("structured event dispatcher was not called")
	}

	var typedNil *capturedCallNotifier
	gateway.SetNotifier(typedNil)
	if gateway.GetNotifier() != nil {
		t.Fatal("typed-nil notifier was retained")
	}
}

func TestGatewayLayoutAndQueueFullResponse(t *testing.T) {
	gatewayType := reflect.TypeOf(Gateway{})
	wantFields := []string{
		"notifier", "eventDispatcher", "clientAdapter", "mu", "agents", "entryWorkers",
		"running", "epoch", "ctx", "cancel",
	}
	if gatewayType.NumField() != len(wantFields) {
		t.Fatalf("Gateway field count = %d, want %d", gatewayType.NumField(), len(wantFields))
	}
	for index, name := range wantFields {
		if gatewayType.Field(index).Name != name {
			t.Fatalf("Gateway field %d = %q, want %q", index, gatewayType.Field(index).Name, name)
		}
	}

	agent := NewAgent("queue-device", nil, nil)
	gateway := NewGateway(agent)
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer gateway.Stop()
	started, release := make(chan struct{}), make(chan struct{})
	if !gateway.enqueueDeviceTask("queue-device", "block", func(*Agent) {
		close(started)
		<-release
	}) {
		t.Fatal("blocking task was rejected")
	}
	<-started
	for index := 0; index < gatewayEntryQueueSize; index++ {
		if !gateway.enqueueDeviceTask("queue-device", "fill", func(*Agent) {}) {
			t.Fatalf("queue rejected task %d before reaching capacity", index)
		}
	}
	tx := newVoiceServerTransaction()
	gateway.HandleClientPrack("queue-device", mustClientRequest(
		t, sip.PRACK, "queue-full", "", "RAck: 1 1 INVITE\r\n",
	), tx)
	response := waitVoiceResponse(t, tx, 503)
	if response.Reason != "Service Unavailable - Queue Full" {
		t.Fatalf("queue-full reason = %q", response.Reason)
	}
	close(release)
}
