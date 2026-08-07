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
}

func (a *stubAccess) Capabilities() Capabilities { return Capabilities{HasISIM: true} }
func (a *stubAccess) IMSIdentityProvider() IMSIdentityProvider {
	return &stubProvider{a: a}
}

type stubProvider struct{ a *stubAccess }

func (p *stubProvider) GetISIMIdentity() (Identity, error) {
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
		IMSI: "310260123456789", IMPI: "310260123456789@ims.mnc026.mcc310.3gppnetwork.org",
		IMPU:   []string{"sip:310260123456789@ims.mnc026.mcc310.3gppnetwork.org"},
		Domain: "ims.mnc026.mcc310.3gppnetwork.org",
	}}
	prepared, err := PrepareStart(PrepareStartInput{
		DeviceID: "wwan0",
		Profile:  Profile{IMSI: "310260123456789", MCC: "310", MNC: "26"},
		Access:   access,
	})
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	if prepared.IMSIdentity.IMPI != "310260123456789@ims.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("IMPI = %q", prepared.IMSIdentity.IMPI)
	}
	if prepared.IMSIdentity.IMPU != "sip:310260123456789@ims.mnc026.mcc310.3gppnetwork.org" {
		t.Errorf("IMPU = %q", prepared.IMSIdentity.IMPU)
	}
	if prepared.IMSIdentity.ActualSource != IMSIdentitySourceISIM || !prepared.IMSIdentity.Applied {
		t.Errorf("identity = %+v", prepared.IMSIdentity)
	}
	if prepared.EffectiveCarrier.MCC != "310" || prepared.EffectiveCarrier.MNC != "26" {
		t.Errorf("carrier = %+v", prepared.EffectiveCarrier)
	}
	// Default ePDG FQDN from the carrier.
	if !strings.Contains(prepared.EPDGAddr, "epdg.epc.mnc026.mcc310.pub.3gppnetwork.org") {
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
		Profile:             Profile{IMSI: "310260123456789", MCC: "310", MNC: "26"},
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

func TestPrepareStartErrors(t *testing.T) {
	if _, err := PrepareStart(PrepareStartInput{Profile: Profile{}}); err == nil {
		t.Error("empty IMSI should error")
	}
	access := &stubAccess{err: errors.New("no isim")}
	if _, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "310260123456789", MCC: "310", MNC: "26"},
		Access:  access,
	}); err == nil {
		t.Error("identity read failure should error")
	}
}

func TestPrepareStartUsesUSIMOnlyWhenISIMUnavailable(t *testing.T) {
	access := &stubAccess{err: fmt.Errorf("card status: %w", ErrISIMUnavailable)}
	prepared, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "234102356143376", MCC: "234", MNC: "10"},
		Access:  access,
	})
	if err != nil {
		t.Fatalf("PrepareStart() error = %v", err)
	}
	identity := prepared.IMSIdentity
	if identity.ActualSource != IMSIdentitySourceUSIM || identity.AKAAppPreference != AKAAppPreferenceUSIMStrict {
		t.Fatalf("identity = %+v, want strict USIM", identity)
	}
	wantDomain := "ims.mnc010.mcc234.3gppnetwork.org"
	if identity.Domain != wantDomain || identity.IMPI != "234102356143376@"+wantDomain {
		t.Fatalf("identity = %+v, want padded 3GPP domain", identity)
	}
	if prepared.EPDGAddr != "epdg.epc.mnc010.mcc234.pub.3gppnetwork.org" {
		t.Fatalf("EPDGAddr = %q", prepared.EPDGAddr)
	}
}

func TestPrepareStartDoesNotHideISIMTransportFailure(t *testing.T) {
	transportErr := errors.New("QMI transport disconnected")
	_, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "234102356143376", MCC: "234", MNC: "10"},
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
