package runtimehost

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

func TestObserverUnsubscribeAndContextPropagation(t *testing.T) {
	instance := &Instance{}
	key := struct{}{}
	ctx := context.WithValue(context.Background(), key, "runtime-context")
	var first, second atomic.Int32
	unsubscribe := instance.AddObserver(ObserverFunc(func(got context.Context, _ Event) {
		if got.Value(key) != "runtime-context" {
			t.Error("observer lost event context")
		}
		first.Add(1)
	}))
	instance.AddObserver(ObserverFunc(func(context.Context, Event) { second.Add(1) }))
	instance.publish(ctx, Event{Kind: "prepared"})
	unsubscribe()
	unsubscribe()
	instance.publish(ctx, Event{Kind: "prepared"})
	if first.Load() != 1 || second.Load() != 2 {
		t.Fatalf("observer calls first=%d second=%d", first.Load(), second.Load())
	}
}

func TestMainStartWaitsForRecoveredIPSecReadyEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	started := make(chan struct{})
	finished := make(chan struct{})
	request := restoredRuntimeRequest()
	request.runner = func(ctx context.Context, core runtimecore.RuntimeStartRequest) (StartResult, error) {
		defer close(finished)
		if !core.Reconnect || time.Duration(core.ReconnectDelay(0)) != 5*time.Second {
			return StartResult{}, errors.New("runtime core reconnect policy was not installed")
		}
		core.Observer.OnRuntimeEvent(ctx, runtimecore.RuntimeEvent[*runtimecore.SessionResult]{
			Kind: "prepared", DeviceID: core.DeviceID, TraceID: core.TraceID,
		})
		close(started)
		<-release
		session := &runtimecore.SessionResult{DeviceID: core.DeviceID}
		core.Observer.OnRuntimeEvent(ctx, runtimecore.RuntimeEvent[*runtimecore.SessionResult]{
			Kind: "established", Handle: session, DeviceID: core.DeviceID,
			TraceID: core.TraceID, Snapshot: runtimecore.Snapshot{Established: true},
		})
		<-ctx.Done()
		core.Observer.OnRuntimeEvent(ctx, runtimecore.RuntimeEvent[*runtimecore.SessionResult]{
			Kind: "stopped", Handle: session, DeviceID: core.DeviceID, TraceID: core.TraceID,
		})
		return StartResult{TraceID: core.TraceID}, ctx.Err()
	}

	type result struct {
		instance *Instance
		err      error
	}
	resultChannel := make(chan result, 1)
	go func() {
		instance, err := Start(ctx, request)
		resultChannel <- result{instance: instance, err: err}
	}()
	<-started
	select {
	case got := <-resultChannel:
		t.Fatalf("Start returned before ipsec_up: %+v", got)
	default:
	}
	close(release)
	got := <-resultChannel
	if got.err != nil || got.instance == nil {
		t.Fatalf("Start instance=%v err=%v", got.instance, got.err)
	}
	state := got.instance.State()
	if !state.TunnelReady || state.LastEvent != "ipsec_up" || state.NetworkMode != "wifi" {
		t.Fatalf("runtime state = %+v", state)
	}
	if state.EPDGAddress != "epdg.example.com" || state.DataplaneMode != swu.DataplaneModeUserspace {
		t.Fatalf("runtime metadata = %+v", state)
	}
	got.instance.mu.RLock()
	hasSession := got.instance.session != nil
	got.instance.mu.RUnlock()
	if !hasSession {
		t.Fatal("ipsec_up did not retain the runtime session")
	}
	if err := got.instance.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-finished
}

func TestReaderStartReturnsBeforeRuntimeReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finished := make(chan struct{})
	request := restoredRuntimeRequest()
	request.Mode = StartModeReader
	request.runner = func(ctx context.Context, core runtimecore.RuntimeStartRequest) (StartResult, error) {
		defer close(finished)
		if time.Duration(core.ReconnectDelay(1)) != 60*time.Second {
			return StartResult{}, errors.New("reader reconnect policy was not installed")
		}
		close(started)
		<-ctx.Done()
		return StartResult{TraceID: core.TraceID}, ctx.Err()
	}
	instance, err := Start(ctx, request)
	if err != nil || instance == nil {
		t.Fatalf("reader Start instance=%v err=%v", instance, err)
	}
	<-started
	if state := instance.State(); state.Phase != "starting" || state.TunnelReady {
		t.Fatalf("reader initial state = %+v", state)
	}
	cancel()
	<-finished
	if err := instance.Stop(context.Background()); err != nil {
		t.Fatalf("reader Stop: %v", err)
	}
}

func TestMainStartExposesRunnerFailureBeforeReady(t *testing.T) {
	want := errors.New("core start rejected")
	request := restoredRuntimeRequest()
	request.runner = func(context.Context, runtimecore.RuntimeStartRequest) (StartResult, error) {
		return StartResult{TraceID: request.TraceID}, want
	}
	instance, err := Start(context.Background(), request)
	if instance != nil || !errors.Is(err, want) {
		t.Fatalf("Start instance=%v err=%v, want %v", instance, err, want)
	}
}

func TestCoreRequestConvertsCompleteBeforeStartConfig(t *testing.T) {
	request := restoredRuntimeRequest()
	request.Dataplane.TUNName = "vowifi-test0"
	request.DNSServer = "192.0.2.53"
	request.Proxy = &ProxyConfig{ID: "p1", Addr: "127.0.0.1:1080", Enabled: true}
	var received SessionConfig
	request.BeforeStart = func(_ context.Context, config SessionConfig) error {
		received = config
		return nil
	}
	core := request.coreRequest()
	if core.BeforeSessionStart == nil || core.Prepared == nil {
		t.Fatal("core request dropped prepared session or BeforeStart")
	}
	ctx := context.Background()
	err := core.BeforeSessionStart(ctx, runtimecore.SessionConfig{
		Ctx: ctx, DeviceID: core.DeviceID, TraceID: core.TraceID, Prepared: *core.Prepared,
		DataplaneMode: core.Dataplane.Mode, TUNName: core.Dataplane.TUNName,
		Proxy: core.Proxy, DNSServer: core.DNSServer,
	})
	if err != nil {
		t.Fatalf("BeforeSessionStart: %v", err)
	}
	if received.Ctx != ctx || received.DeviceID != request.DeviceID || received.TraceID != request.TraceID {
		t.Fatalf("BeforeStart identity = %+v", received)
	}
	if received.Prepared.Profile.IMSI != request.Prepared.Profile.IMSI || received.TUNName != "vowifi-test0" {
		t.Fatalf("BeforeStart prepared/dataplane = %+v", received)
	}
	if received.Proxy == nil || received.Proxy.Addr != request.Proxy.Addr || received.DNSServer != request.DNSServer {
		t.Fatalf("BeforeStart network = %+v", received)
	}
}

func TestRecoveredPreparedSessionConversionsDetachCarrierData(t *testing.T) {
	request := restoredRuntimeRequest()
	request.Prepared.EffectiveCarrier.MCC = request.Prepared.Profile.MCC
	request.Prepared.EffectiveCarrier.MNC = request.Prepared.Profile.MNC
	request.Prepared.EffectiveCarrier.IKEProposals = []string{"aes128-sha256-modp2048"}
	request.Prepared.AuthPlan = identity.AuthPlan{EPDGApp: "ISIM", IMSApp: "invalid"}
	internal := preparedSessionPtrToInternal(request.Prepared)
	if internal == nil || internal.AuthPlan.EPDGApp != "isim" || internal.AuthPlan.IMSApp != "usim" {
		t.Fatalf("internal auth plan = %+v", internal)
	}
	request.Prepared.EffectiveCarrier.IKEProposals[0] = "changed"
	if got := internal.CarrierPlan.IKE.IKEProposals[0]; got != "aes128-sha256-modp2048" {
		t.Fatalf("internal proposal retained caller alias: %q", got)
	}
	external := preparedSessionFromInternal(*internal)
	internal.CarrierPlan.IKE.IKEProposals[0] = "changed-again"
	if got := external.EffectiveCarrier.IKEProposals[0]; got != "aes128-sha256-modp2048" {
		t.Fatalf("external proposal retained internal alias: %q", got)
	}
}

func TestRuntimeEventNamesAndStateAreRecovered(t *testing.T) {
	instance := &Instance{}
	observer := &instanceObserver{inst: instance, deviceID: "dev-1"}
	observer.OnRuntimeEvent(context.Background(), runtimecore.RuntimeEvent[*runtimecore.SessionResult]{
		Kind: "retry", Attempt: 2, RetryDelay: int64(time.Second), RedirectEPDG: "epdg-2.example",
	})
	state := instance.State()
	if state.LastEvent != "retrying" || state.Phase != "retrying" || state.LastRedirectEPDG != "epdg-2.example" {
		t.Fatalf("retry state = %+v", state)
	}
	observer.OnRuntimeEvent(context.Background(), runtimecore.RuntimeEvent[*runtimecore.SessionResult]{
		Kind: "error", Message: "authentication rejected",
	})
	state = instance.State()
	if state.LastEvent != "terminal_error" || state.LastError != "authentication rejected" {
		t.Fatalf("terminal state = %+v", state)
	}
}

func restoredRuntimeRequest() StartRequest {
	return StartRequest{
		Mode: StartModeMain, DeviceID: "dev-1", TraceID: "trace-1", NetworkMode: "wifi",
		Prepared: &identity.PreparedSession{
			Profile: identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "260"},
			IMSIdentity: identity.IMSIdentityResult{
				IMPI: "310260123456789@ims.example", IMPU: "sip:310260123456789@ims.example",
				Domain: "ims.example", Applied: true,
			},
			EPDGAddr: "epdg.example.com",
		},
		SIM:       NewReaderSIMAdapter(startAKAProvider{}),
		Dataplane: DataplanePolicy{Mode: swu.DataplaneModeUserspace},
	}
}
