package voicehost

import (
	"context"
	"reflect"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imsendpoint"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voiceclient"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
)

type recoveredGatewayAPI interface {
	Start(context.Context) error
	Stop() error
	SetNotifier(Notifier)
	SetEventDispatcher(eventhost.Dispatcher)
	SetClientAdapter(voiceclient.Adapter)
	GetAgent(string) interface{}
	DeviceStatus(string) map[string]interface{}
	SimulateCall(context.Context, string, SimulateCallRequest) (*SimulateCallResult, error)
	HandleClientInvite(string, *sip.Request, sip.ServerTransaction)
	HandleClientCancel(string, *sip.Request, sip.ServerTransaction)
	HandleClientPrack(string, *sip.Request, sip.ServerTransaction)
	HandleClientAck(string, *sip.Request)
	HandleClientBye(string, *sip.Request, sip.ServerTransaction)
	StartPCAP(string, string) error
	StopPCAP(string) error
}

func TestRecoveredGatewayMethodSet(t *testing.T) {
	var _ recoveredGatewayAPI = (*Gateway)(nil)
	var _ runtimeLifecycle = lifecycleAdapter{}
	var _ interface {
		AttachDevice(string, imsendpoint.Endpoint) error
		DetachDevice(string)
	} = lifecycleAdapter{}
}

func TestRecoveredVoicehostStructLayouts(t *testing.T) {
	type gatewayPrefix struct {
		inner *voice.Gateway
	}
	type requestPrefix struct {
		Callee      string `json:"callee"`
		HoldSeconds int    `json:"hold_seconds,omitempty"`
		OnConnected func() `json:"-" binding:"-"`
	}
	type resultPrefix struct {
		Success    bool   `json:"success"`
		DurationMs int64  `json:"duration_ms"`
		Reason     string `json:"reason"`
	}
	type dispatcherPrefix struct {
		dispatch eventhost.Dispatcher
	}
	type lifecyclePrefix struct {
		gateway *Gateway
	}

	assertVoicehostStructPrefix(t, reflect.TypeOf(Gateway{}), reflect.TypeOf(gatewayPrefix{}))
	assertVoicehostStructPrefix(t, reflect.TypeOf(SimulateCallRequest{}), reflect.TypeOf(requestPrefix{}))
	assertVoicehostStructPrefix(t, reflect.TypeOf(SimulateCallResult{}), reflect.TypeOf(resultPrefix{}))
	assertVoicehostStructPrefix(t, reflect.TypeOf(eventDispatcherAdapter{}), reflect.TypeOf(dispatcherPrefix{}))
	assertVoicehostStructPrefix(t, reflect.TypeOf(lifecycleAdapter{}), reflect.TypeOf(lifecyclePrefix{}))
}

func TestRecoveredNilGatewayBehavior(t *testing.T) {
	var gateway *Gateway
	if err := gateway.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := gateway.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if result, err := gateway.SimulateCall(context.Background(), "missing", SimulateCallRequest{}); result != nil || err != nil {
		t.Fatalf("SimulateCall result=%+v error=%v", result, err)
	}
	if err := gateway.StartPCAP("missing", t.TempDir()); err != nil {
		t.Fatalf("StartPCAP: %v", err)
	}
	if err := gateway.StopPCAP("missing"); err != nil {
		t.Fatalf("StopPCAP: %v", err)
	}
}

func TestRecoveredGetAgentReturnsNilInterfaceForMissingDevice(t *testing.T) {
	if agent := NewGateway().GetAgent("missing"); agent != nil {
		t.Fatalf("missing agent = %#v", agent)
	}
}

func assertVoicehostStructPrefix(t *testing.T, actual, prefix reflect.Type) {
	t.Helper()
	if actual.NumField() < prefix.NumField() {
		t.Fatalf("%s has %d fields, want at least %d", actual, actual.NumField(), prefix.NumField())
	}
	for index := 0; index < prefix.NumField(); index++ {
		got, want := actual.Field(index), prefix.Field(index)
		if got.Name != want.Name || got.Type != want.Type || got.Tag != want.Tag {
			t.Fatalf("%s field %d = %s %s %q, want %s %s %q", actual, index,
				got.Name, got.Type, got.Tag, want.Name, want.Type, want.Tag)
		}
	}
}
