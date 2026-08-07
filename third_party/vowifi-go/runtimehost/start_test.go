package runtimehost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

// stubAccess is a minimal identity.Access for tests.
type stubAccess struct{}

func (stubAccess) Capabilities() identity.Capabilities { return identity.Capabilities{HasISIM: true} }
func (stubAccess) IMSIdentityProvider() identity.IMSIdentityProvider {
	return stubProvider{}
}

type stubProvider struct{}

func (stubProvider) GetISIMIdentity() (identity.Identity, error) {
	return identity.Identity{
		IMSI:   "310260123456789",
		IMPI:   "310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
		IMPU:   []string{"sip:310260123456789@ims.mnc026.mcc310.3gppnetwork.org"},
		Domain: "ims.mnc026.mcc310.3gppnetwork.org",
	}, nil
}

type startAKAProvider struct{}

func (startAKAProvider) CalculateAKA(_, _ []byte) (enginesim.AKAResult, error) {
	return enginesim.AKAResult{RES: []byte{1}, CK: make([]byte, 16), IK: make([]byte, 16)}, nil
}

type startSIMAdapter struct{}

func (startSIMAdapter) AKAProvider() enginesim.AKAProvider { return startAKAProvider{} }

type lifecycleTunnel struct {
	mu            sync.Mutex
	state         string
	connectErr    error
	done          chan struct{}
	connectCalled chan struct{}
	shutdownOnce  sync.Once
	onStateChange func(string)
}

func newLifecycleTunnel(connectErr error) *lifecycleTunnel {
	return &lifecycleTunnel{
		state: "idle", connectErr: connectErr, done: make(chan struct{}), connectCalled: make(chan struct{}),
	}
}

func (t *lifecycleTunnel) Connect(context.Context) error {
	t.setState("connecting")
	close(t.connectCalled)
	if t.connectErr != nil {
		t.setState("error")
		return t.connectErr
	}
	t.setState("established")
	return nil
}

func (t *lifecycleTunnel) Shutdown() {
	t.shutdownOnce.Do(func() {
		t.setState("shutdown")
		close(t.done)
	})
}

func (t *lifecycleTunnel) State() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state
}

func (t *lifecycleTunnel) WaitDoneContext(ctx context.Context) error {
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *lifecycleTunnel) setState(state string) {
	t.mu.Lock()
	t.state = state
	t.mu.Unlock()
	if t.onStateChange != nil {
		t.onStateChange(state)
	}
}

func runtimeTestRequest(prepared *identity.PreparedSession, tunnel *lifecycleTunnel) StartRequest {
	return StartRequest{
		Mode: StartModeMain, DeviceID: "wwan0", TraceID: "trace-1", Prepared: prepared,
		SIM: startSIMAdapter{}, Dataplane: DataplanePolicy{Mode: swu.DataplaneModeUserspace},
		TunnelFactory: func(cfg *swu.Config) (Tunnel, error) {
			tunnel.onStateChange = cfg.OnStateChange
			return tunnel, nil
		},
	}
}

func TestStart(t *testing.T) {
	prepared, err := identity.PrepareStart(identity.PrepareStartInput{
		DeviceID: "wwan0",
		Profile:  identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "26"},
		Access:   stubAccess{},
	})
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	var beforeCalled bool
	tunnel := newLifecycleTunnel(nil)
	req := runtimeTestRequest(&prepared, tunnel)
	req.Profile = prepared.Profile
	req.BeforeStart = func(_ context.Context, cfg SessionConfig) error {
		beforeCalled = cfg.DataplaneMode == swu.DataplaneModeUserspace
		return nil
	}
	req.ShouldRun = func() bool { return true }
	inst, err := Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !beforeCalled {
		t.Error("BeforeStart not called")
	}
	if inst.State().SessionState != "established" {
		t.Errorf("state = %+v", inst.State())
	}
	if inst.State().EPDGAddress == "" {
		t.Error("EPDG address not set from prepared session")
	}
	select {
	case <-tunnel.connectCalled:
	default:
		t.Error("SWu Connect was not called")
	}
	if !inst.State().TunnelReady || !inst.State().DataPlaneUp {
		t.Errorf("tunnel state = %+v", inst.State())
	}
	if err := inst.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestStartShouldRunFalse(t *testing.T) {
	if _, err := Start(context.Background(), StartRequest{
		ShouldRun: func() bool { return false },
	}); err == nil {
		t.Error("Start with ShouldRun=false should error")
	}
}

func TestNewModemAccessAdapter(t *testing.T) {
	if NewModemAccessAdapter(nil) != nil {
		t.Error("nil modem should produce nil adapter")
	}
}
func TestStartWiresRealIMSService(t *testing.T) {
	prepared := &identity.PreparedSession{
		Profile: identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "260"},
		IMSIdentity: identity.IMSIdentity{
			IMPI:   "310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
			IMPU:   "sip:310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
			Domain: "ims.mnc026.mcc310.3gppnetwork.org",
		},
		EPDGAddr: "epdg.example.com",
	}
	req := runtimeTestRequest(prepared, newLifecycleTunnel(nil))
	req.DeviceID = "dev-1"
	req.Prepared = prepared
	inst, err := Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	svc := inst.Service()
	if svc == nil {
		t.Fatal("service not installed")
	}
	// The real service adapter should report the device ID.
	st := svc.Status()
	if st.State.DeviceID != "dev-1" {
		t.Errorf("device = %q, want dev-1", st.State.DeviceID)
	}
	// SMS should be available (not the placeholder error).
	if _, err := svc.SendSMSWithResult(context.Background(), "+8613800000000", "hi"); err != nil {
		if strings.Contains(err.Error(), "not available") {
			t.Errorf("placeholder service wired: %v", err)
		}
	}
}

func TestStartReturnsFailedInstanceWhenTunnelConnectFails(t *testing.T) {
	connectErr := errors.New("IKE_AUTH rejected")
	tunnel := newLifecycleTunnel(connectErr)
	prepared := &identity.PreparedSession{
		Profile:     identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "260"},
		IMSIdentity: identity.IMSIdentity{IMPI: "310260123456789@ims.example", IMPU: "sip:310260123456789@ims.example", Domain: "ims.example"},
		EPDGAddr:    "epdg.example.com",
	}
	inst, err := Start(context.Background(), runtimeTestRequest(prepared, tunnel))
	if !errors.Is(err, connectErr) {
		t.Fatalf("Start error = %v, want %v", err, connectErr)
	}
	if inst == nil || inst.State().SessionState != "error" || !strings.Contains(inst.State().Error, connectErr.Error()) {
		t.Fatalf("failed instance state = %+v", inst.State())
	}
	select {
	case <-tunnel.done:
	default:
		t.Fatal("failed tunnel was not shut down")
	}
}

func TestStartContextCancellationStopsTunnel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tunnel := newLifecycleTunnel(nil)
	prepared := &identity.PreparedSession{
		Profile:     identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "260"},
		IMSIdentity: identity.IMSIdentity{IMPI: "310260123456789@ims.example", IMPU: "sip:310260123456789@ims.example", Domain: "ims.example"},
		EPDGAddr:    "epdg.example.com",
	}
	inst, err := Start(ctx, runtimeTestRequest(prepared, tunnel))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopped := make(chan struct{}, 1)
	inst.AddObserver(func(_ context.Context, event Event) {
		if event.State.SessionState == "stopped" {
			stopped <- struct{}{}
		}
	})
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not stop runtime")
	}
	select {
	case <-tunnel.done:
	default:
		t.Fatal("context cancellation did not shut down tunnel")
	}
}

func TestStartPassesSOCKS5ProxyToTunnel(t *testing.T) {
	tunnel := newLifecycleTunnel(nil)
	prepared := &identity.PreparedSession{
		Profile:     identity.Profile{IMSI: "310260123456789", MCC: "310", MNC: "260"},
		IMSIdentity: identity.IMSIdentity{IMPI: "310260123456789@ims.example", IMPU: "sip:310260123456789@ims.example", Domain: "ims.example"},
		EPDGAddr:    "epdg.example.com",
	}
	req := runtimeTestRequest(prepared, tunnel)
	req.Proxy = &ProxyConfig{Enabled: true, Addr: "127.0.0.1:1080", Username: "alice", Password: "secret"}
	var captured *swu.Config
	req.TunnelFactory = func(cfg *swu.Config) (Tunnel, error) {
		captured = cfg
		tunnel.onStateChange = cfg.OnStateChange
		return tunnel, nil
	}
	inst, err := Start(context.Background(), req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer inst.Stop(context.Background())
	if captured == nil || captured.ProxyAddr != "127.0.0.1:1080" || captured.Proxy == nil {
		t.Fatalf("captured proxy config = %+v", captured)
	}
	if captured.Proxy.Username != "alice" || captured.Proxy.Password != "secret" {
		t.Fatalf("captured proxy credentials mismatch")
	}
}
