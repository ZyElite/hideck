package vowifihost

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

type runtimeStartTestModem struct{}

func (runtimeStartTestModem) DeviceID() string { return "dev-1" }
func (runtimeStartTestModem) IsHealthy() bool  { return true }
func (runtimeStartTestModem) IsSimInserted() bool {
	return true
}
func (runtimeStartTestModem) QuerySIMInserted() (bool, error) { return true, nil }
func (runtimeStartTestModem) GetRegStatus() (int, string)     { return 1, "registered" }
func (runtimeStartTestModem) GetNetworkMode() string          { return "LTE" }
func (runtimeStartTestModem) Capabilities() runtimehost.ModemCapabilities {
	return runtimehost.ModemCapabilities{}
}
func (runtimeStartTestModem) IMSIdentityProvider() runtimehost.IMSIdentityProvider {
	return runtimeStartTestModem{}
}
func (runtimeStartTestModem) GetISIMIdentity() (identity.Identity, error) {
	return identity.Identity{IMPI: "310260123456789@ims.example.com", IMPU: []string{"sip:310260123456789@ims.example.com"}, Domain: "ims.example.com"}, nil
}
func (runtimeStartTestModem) ExecuteATSilent(string, time.Duration) (string, error) {
	return "", nil
}
func (runtimeStartTestModem) OpenLogicalChannel(string) (int, error) { return 0, nil }
func (runtimeStartTestModem) CloseLogicalChannel(int) error          { return nil }
func (runtimeStartTestModem) TransmitAPDU(int, string) (string, error) {
	return "", nil
}
func (runtimeStartTestModem) Stop() {}

func TestManagerStartRuntimeBuildsRequestAndClaimsInstance(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-1"
	claim := manager.BeginStart(deviceID)
	if !claim.Accepted {
		t.Fatalf("BeginStart() = %+v, want accepted", claim)
	}
	wantInst := &runtimehost.Instance{}
	var captured runtimehost.StartRequest
	manager.SetRuntimeStartForTest(func(ctx context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
		captured = req
		if !req.ShouldRun() {
			t.Fatal("StartRequest.ShouldRun() = false before invalidation, want true")
		}
		return wantInst, nil
	})

	result, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		TraceID:  "trace-1",
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			Profile: identity.Profile{IMSI: "001010000000001"},
			SIM:     runtimehost.NewReaderSIMAdapter(simProviderStub{}),
			Prepared: identity.PreparedSession{
				Profile: identity.Profile{IMSI: "001010000000001"},
			},
			NetworkMode: "LTE",
		},
		Modem:     runtimeStartTestModem{},
		Dataplane: runtimehost.DataplanePolicy{Mode: "userspace"},
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if result.Instance != wantInst || result.Stale {
		t.Fatalf("StartRuntime() = %+v, want claimed instance", result)
	}
	if manager.Instance(deviceID) != wantInst {
		t.Fatal("StartRuntime() should claim instance in runtime store")
	}
	if captured.Mode != runtimehost.StartModeMain || captured.DeviceID != deviceID || captured.TraceID != "trace-1" {
		t.Fatalf("captured request identity = mode %q device %q trace %q", captured.Mode, captured.DeviceID, captured.TraceID)
	}
	if captured.SIM == nil || captured.Access == nil {
		t.Fatal("captured request should include SIM and Access adapters")
	}
	if captured.NetworkMode != "LTE" || captured.Dataplane.Mode != "userspace" {
		t.Fatalf("captured request network/dataplane = %q/%q", captured.NetworkMode, captured.Dataplane.Mode)
	}
}

func TestManagerStartRuntimeBroadcastsClaimedState(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-broadcast"
	claim := manager.BeginStart(deviceID)
	notifications, unsubscribe := manager.SubscribeState(deviceID)
	defer unsubscribe()
	manager.SetRuntimeStartForTest(func(context.Context, runtimehost.StartRequest) (*runtimehost.Instance, error) {
		return &runtimehost.Instance{}, nil
	})
	_, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			SIM:      runtimehost.NewReaderSIMAdapter(simProviderStub{}),
			Prepared: identity.PreparedSession{Profile: identity.Profile{IMSI: "001010000000001"}},
		},
		Modem: runtimeStartTestModem{},
	})
	if err != nil {
		t.Fatalf("StartRuntime: %v", err)
	}
	select {
	case <-notifications:
	case <-time.After(time.Second):
		t.Fatal("claimed runtime state was not broadcast")
	}
}

func TestManagerStartRuntimeStopsStaleStartedInstance(t *testing.T) {
	manager := NewManager()
	deviceID := "dev-stale"
	claim := manager.BeginStart(deviceID)
	manager.InvalidateRuntime(deviceID, "test")
	manager.SetRuntimeStartForTest(func(ctx context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
		if req.ShouldRun() {
			t.Fatal("StartRequest.ShouldRun() = true after invalidation, want false")
		}
		return &runtimehost.Instance{}, nil
	})

	result, err := manager.StartRuntime(context.Background(), RuntimeStartRequest{
		DeviceID: deviceID,
		TraceID:  "trace-stale",
		Epoch:    claim.Epoch,
		Prepared: PreparedStart{
			Profile:     identity.Profile{IMSI: "001010000000001"},
			SIM:         runtimehost.NewReaderSIMAdapter(simProviderStub{}),
			Prepared:    identity.PreparedSession{Profile: identity.Profile{IMSI: "001010000000001"}},
			NetworkMode: "LTE",
		},
		Modem: runtimeStartTestModem{},
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}
	if !result.Stale {
		t.Fatalf("StartRuntime() stale = false, want true")
	}
	if manager.Active(deviceID) {
		t.Fatal("stale started instance should not become active")
	}
}
