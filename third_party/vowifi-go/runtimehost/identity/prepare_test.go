package identity

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type stubAccess struct {
	ident Identity
	err   error
	calls int
}

func (a *stubAccess) Capabilities() Capabilities { return Capabilities{HasISIM: true} }
func (a *stubAccess) IMSIdentityProvider() IMSIdentityProvider {
	return &stubProvider{a: a}
}

type stubProvider struct{ a *stubAccess }

func (p *stubProvider) GetISIMIdentity() (Identity, error) {
	p.a.calls++
	return p.a.ident, p.a.err
}

func TestNormalizeProfile(t *testing.T) {
	p := NormalizeProfile(Profile{IMSI: " 310260123456789 ", MCC: " 310 ", MNC: " 26 ", IMEI: " 1234 "})
	if p.IMSI != "310260123456789" || p.MCC != "310" || p.MNC != "26" || p.IMEI != "1234" {
		t.Errorf("normalized = %+v", p)
	}
}

func TestPrepareStart(t *testing.T) {
	access := &stubAccess{ident: Identity{
		IMSI: "310280233621715", IMPI: "310280233621715@private.att.net",
		IMPU:   []string{"sip:310280233621715@one.att.net"},
		Domain: "one.att.net",
	}}
	prepared, err := PrepareStart(PrepareStartInput{
		DeviceID: "wwan0",
		Profile:  Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		Access:   access,
	})
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	if prepared.IMSIdentity.IMPI != "310280233621715@private.att.net" {
		t.Errorf("IMPI = %q", prepared.IMSIdentity.IMPI)
	}
	if prepared.IMSIdentity.IMPU != "sip:310280233621715@one.att.net" {
		t.Errorf("IMPU = %q", prepared.IMSIdentity.IMPU)
	}
	if prepared.IMSIdentity.ActualSource != IMSIdentitySourceISIM || !prepared.IMSIdentity.Applied {
		t.Errorf("identity = %+v", prepared.IMSIdentity)
	}
	if prepared.EffectiveCarrier.MCC != "310" || prepared.EffectiveCarrier.MNC != "280" {
		t.Errorf("carrier = %+v", prepared.EffectiveCarrier)
	}
	if prepared.EPDGAddr == "" {
		t.Errorf("EPDG = %q", prepared.EPDGAddr)
	}
}

func TestPrepareStartOverride(t *testing.T) {
	access := &stubAccess{ident: Identity{
		IMPI:   "310260123456789@ims.example.com",
		IMPU:   []string{"sip:310260123456789@ims.example.com"},
		Domain: "ims.example.com",
	}}
	prepared, err := PrepareStart(PrepareStartInput{
		Profile:             Profile{IMSI: "310280123456789", MCC: "310", MNC: "280"},
		RuntimeEPDGOverride: "epdg.example.com",
		Access:              access,
	})
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	if prepared.EPDGAddr != "epdg.example.com" || prepared.EPDGSource != "redirect" {
		t.Errorf("EPDG = %q source %q", prepared.EPDGAddr, prepared.EPDGSource)
	}
}

func TestPrepareStartIdentityInputPriority(t *testing.T) {
	direct := &stubAccess{ident: Identity{
		IMPI: "direct@private.att.net", IMPU: []string{"sip:direct@one.att.net"},
	}}
	fallback := &stubAccess{err: errors.New("fallback must not be called")}
	prepared, err := PrepareStart(PrepareStartInput{
		Profile:          Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		IdentityProvider: &stubProvider{a: direct}, Access: fallback,
	})
	if err != nil {
		t.Fatalf("PrepareStart direct provider: %v", err)
	}
	if direct.calls != 1 || fallback.calls != 0 || prepared.IMSIdentity.IMPI != "direct@private.att.net" {
		t.Fatalf("calls direct=%d fallback=%d identity=%+v", direct.calls, fallback.calls, prepared.IMSIdentity)
	}

	direct.err = errors.New("supplied identity must skip provider")
	prepared, err = PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		IMSIdentityResult: IMSIdentityResult{
			RequestedSource: IMSIdentitySourceISIM, ActualSource: IMSIdentitySourceISIM,
			Applied: true, IMPI: "supplied@private.att.net",
		},
		IdentityProvider: &stubProvider{a: direct}, Access: fallback,
	})
	if err != nil {
		t.Fatalf("PrepareStart supplied identity: %v", err)
	}
	if direct.calls != 1 || fallback.calls != 0 || prepared.IMSIdentity.IMPI != "supplied@private.att.net" {
		t.Fatalf("calls direct=%d fallback=%d identity=%+v", direct.calls, fallback.calls, prepared.IMSIdentity)
	}
}

func TestPrepareStartErrors(t *testing.T) {
	if _, err := PrepareStart(PrepareStartInput{Profile: Profile{}}); err == nil {
		t.Error("empty IMSI should error")
	}
	access := &stubAccess{err: errors.New("no isim")}
	if _, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		Access:  access,
	}); err == nil {
		t.Error("identity read failure should error")
	}
}

func TestPrepareStartDerivedCarrierSkipsUnavailableISIM(t *testing.T) {
	access := &stubAccess{err: fmt.Errorf("card status: %w", ErrISIMUnavailable)}
	prepared, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "234102356143376", MCC: "234", MNC: "10"},
		Access:  access,
	})
	if err != nil {
		t.Fatalf("PrepareStart() error = %v", err)
	}
	resolved := prepared.IMSIdentity
	if resolved != (IMSIdentity{}) || prepared.AuthPlan.IMSApp != "usim" {
		t.Fatalf("identity = %+v auth plan=%+v", resolved, prepared.AuthPlan)
	}
	wantDomain := "ims.mnc010.mcc234.3gppnetwork.org"
	if prepared.Profile.IMSDomain != wantDomain {
		t.Fatalf("profile = %+v, want padded 3GPP domain", prepared.Profile)
	}
	if prepared.EPDGAddr != "epdg.epc.mnc010.mcc234.pub.3gppnetwork.org" {
		t.Fatalf("EPDGAddr = %q", prepared.EPDGAddr)
	}
	if prepared.EffectiveCarrier.PresetID != "giffgaff_23410" ||
		prepared.CarrierConfig.DeviceModel != "rmx3366" {
		t.Fatalf("carrier config = %+v", prepared.CarrierConfig)
	}
	if !strings.HasPrefix(prepared.Profile.IMEI, "86034905") ||
		prepared.IdentityIMEISource != "carrier_device_model" {
		t.Fatalf("device identity = %q, source = %q", prepared.Profile.IMEI, prepared.IdentityIMEISource)
	}
	if prepared.Profile.UserAgent == "" || prepared.Profile.IMSDomain != wantDomain {
		t.Fatalf("built profile = %+v", prepared.Profile)
	}
}

func TestPrepareStartDoesNotHideISIMTransportFailure(t *testing.T) {
	transportErr := errors.New("QMI transport disconnected")
	_, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		Access:  &stubAccess{err: transportErr},
	})
	if !errors.Is(err, transportErr) {
		t.Fatalf("PrepareStart() error = %v, want transport error chain", err)
	}
}

func TestReadISIMIdentityNilAccess(t *testing.T) {
	if _, err := ReadISIMIdentity(nil); err == nil {
		t.Error("nil access should error")
	}
}
