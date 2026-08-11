package runtimehost

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

func TestRecoveredRootStructPrefixes(t *testing.T) {
	assertRootPrefix(t, reflect.TypeOf(State{}), reflect.TypeOf(recoveredStatePrefix{}))
	assertRootPrefix(t, reflect.TypeOf(Event{}), reflect.TypeOf(recoveredEventPrefix{}))
	assertRootPrefix(t, reflect.TypeOf(Instance{}), reflect.TypeOf(recoveredInstancePrefix{}))
	assertRootPrefix(t, reflect.TypeOf(StartRequest{}), reflect.TypeOf(recoveredStartRequestPrefix{}))
	assertRootPrefix(t, reflect.TypeOf(SessionConfig{}), reflect.TypeOf(recoveredSessionConfigPrefix{}))
	assertRootPrefix(t, reflect.TypeOf(DataplanePolicy{}), reflect.TypeOf(recoveredDataplanePrefix{}))
}

func TestRecoveredRootInterfacesAndMethods(t *testing.T) {
	var _ Observer = ObserverFunc(nil)
	var _ messaging.Service = serviceAdapter{}
	var _ messaging.Service = (*serviceAdapter)(nil)
	var _ SIMAdapter = simAdapter{}
	var _ SIMAdapter = (*simAdapter)(nil)
	var _ access.Adapter = accessAdapter{}
	var _ access.SIMAdapter = NewReaderSIMAdapter(startAKAProvider{}).runtimeSIMAdapter()

	var addObserver func(*Instance, Observer) func() = (*Instance).AddObserver
	var getDelivery func(*Instance, string) (*messaging.DeliveryStatus, error) = (*Instance).GetSMSDeliveryStatus
	var service func(*Instance) messaging.Service = (*Instance).Service
	if addObserver == nil || getDelivery == nil || service == nil {
		t.Fatal("recovered Instance methods are unavailable")
	}

	assertInterfaceMethods(t, reflect.TypeOf((*Observer)(nil)).Elem(), []string{"OnRuntimeHostEvent"})
	assertInterfaceMethods(t, reflect.TypeOf((*SIMAdapter)(nil)).Elem(), []string{"runtimeSIMAdapter"})
	assertInterfaceMethods(t, reflect.TypeOf((*Modem)(nil)).Elem(), []string{
		"CloseLogicalChannel", "DeviceID", "ExecuteATSilent", "GetNetworkMode", "GetRegStatus",
		"IsHealthy", "IsSimInserted", "OpenLogicalChannel", "QuerySIMInserted", "Stop", "TransmitAPDU",
	})
}

func TestRecoveredReconnectDelays(t *testing.T) {
	wait, delay := (StartRequest{}).startPolicy()
	if !wait || time.Duration(delay(0)) != 5*time.Second {
		t.Fatalf("default start policy wait=%v delay=%s", wait, time.Duration(delay(0)))
	}
	wait, delay = (StartRequest{Mode: StartModeReader}).startPolicy()
	if wait || time.Duration(delay(0)) != 30*time.Second {
		t.Fatalf("reader start policy wait=%v delay=%s", wait, time.Duration(delay(0)))
	}
	if got := time.Duration(defaultMainReconnectDelay(0)); got != 5*time.Second {
		t.Fatalf("main delay 0 = %s", got)
	}
	if got := time.Duration(defaultMainReconnectDelay(1)); got != 30*time.Second {
		t.Fatalf("main delay 1 = %s", got)
	}
	wantReader := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second}
	for attempt, want := range wantReader {
		if got := time.Duration(defaultReaderReconnectDelay(attempt)); got != want {
			t.Fatalf("reader delay %d = %s, want %s", attempt, got, want)
		}
	}
}

func assertRootPrefix(t *testing.T, actual, prefix reflect.Type) {
	t.Helper()
	if actual.NumField() < prefix.NumField() {
		t.Fatalf("%s has %d fields, want prefix %d", actual, actual.NumField(), prefix.NumField())
	}
	for index := 0; index < prefix.NumField(); index++ {
		got, want := actual.Field(index), prefix.Field(index)
		if got.Name != want.Name || got.Type != want.Type || got.Offset != want.Offset {
			t.Fatalf("%s field %d = %s %s @%d, want %s %s @%d",
				actual, index, got.Name, got.Type, got.Offset, want.Name, want.Type, want.Offset)
		}
	}
}

func assertInterfaceMethods(t *testing.T, value reflect.Type, names []string) {
	t.Helper()
	if value.NumMethod() != len(names) {
		t.Fatalf("%s has %d methods, want %d", value, value.NumMethod(), len(names))
	}
	for index, name := range names {
		if got := value.Method(index).Name; got != name {
			t.Fatalf("%s method %d = %s, want %s", value, index, got, name)
		}
	}
}

type recoveredStatePrefix struct {
	Phase            string
	DeviceID         string
	DataplaneMode    string
	NetworkMode      string
	SIMReady         bool
	AccessReady      bool
	TunnelReady      bool
	IMSReady         bool
	SMSReady         bool
	RegStatus        int
	RegStatusText    string
	LastEvent        string
	LastReason       string
	LastRedirectEPDG string
	LastErrorClass   string
	LastError        string
	UpdatedAt        time.Time
}

type recoveredEventPrefix struct {
	Kind         string
	DeviceID     string
	TraceID      string
	Reason       string
	Attempt      int
	RetryDelay   int64
	RedirectEPDG string
	State        State
}

type recoveredInstancePrefix struct {
	mu        sync.RWMutex
	state     State
	service   messaging.Service
	session   *runtimecore.SessionResult
	startedAt time.Time
	cancel    func()
	observers []Observer
	onNotify  func(string)
	onSMS     func(string, string, string, time.Time)
}

type recoveredStartRequestPrefix struct {
	Mode          string
	DeviceID      string
	TraceID       string
	Profile       identity.Profile
	Prepared      *identity.PreparedSession
	IMSIdentity   identity.IMSIdentityResult
	NetworkMode   string
	VoiceGateway  *voicehost.Gateway
	SIM           SIMAdapter
	Access        identity.AccessAdapter
	Dataplane     DataplanePolicy
	Proxy         *ProxyConfig
	DNSServer     string
	DeliveryStore messaging.DeliveryStore
	Dispatch      eventhost.Dispatcher
	BeforeStart   func(context.Context, SessionConfig) error
	ShouldRun     func() bool
	runner        func(context.Context, runtimecore.RuntimeStartRequest) (StartResult, error)
}

type recoveredSessionConfigPrefix struct {
	Ctx           context.Context
	DeviceID      string
	TraceID       string
	Prepared      identity.PreparedSession
	DataplaneMode string
	TUNName       string
	Proxy         *ProxyConfig
	DNSServer     string
}

type recoveredDataplanePrefix struct {
	Mode    string
	TUNName string
}
