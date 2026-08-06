package runtimehost

import (
	"context"
	"strings"
	"testing"

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
	return identity.Identity{IMSI: "310260123456789"}, nil
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
	inst, err := Start(context.Background(), StartRequest{
		Mode:        StartModeMain,
		DeviceID:    "wwan0",
		TraceID:     "trace-1",
		Profile:     prepared.Profile,
		Prepared:    &prepared,
		BeforeStart: func(_ context.Context, _ SessionConfig) error { beforeCalled = true; return nil },
		ShouldRun:   func() bool { return true },
	})
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
	req := StartRequest{
		Mode:     StartModeMain,
		DeviceID: "dev-1",
		TraceID:  "trace-1",
		Prepared: &identity.PreparedSession{
			IMSIdentity: identity.IMSIdentity{
				IMPI:   "310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
				IMPU:   "sip:310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
				Domain: "ims.mnc026.mcc310.3gppnetwork.org",
			},
			EPDGAddr: "epdg.example.com",
		},
	}
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
