package startup

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

type identityProviderStub struct {
	identity profile.Identity
	err      error
	calls    int
}

func (provider *identityProviderStub) GetISIMIdentity() (profile.Identity, error) {
	provider.calls++
	return provider.identity, provider.err
}

type accessStub struct{ provider profile.Provider }

func (accessStub) Capabilities() access.Capabilities { return access.Capabilities{} }
func (adapter accessStub) IMSIdentityProvider() profile.Provider {
	return adapter.provider
}

func TestHasIMSIdentityResolutionMatchesRecoveredFields(t *testing.T) {
	values := []profile.IMSIdentityResult{
		{Applied: true}, {RequestedSource: " isim "}, {ActualSource: "isim"},
		{AKAAppPreference: "isim"}, {IMPI: "impi"}, {IMPU: "impu"}, {Domain: "domain"},
	}
	for _, value := range values {
		if !HasIMSIdentityResolution(value) {
			t.Fatalf("resolution not detected for %+v", value)
		}
	}
	if HasIMSIdentityResolution(profile.IMSIdentityResult{}) {
		t.Fatal("zero identity reported as resolved")
	}
}

func TestNormalizeIMSIdentity(t *testing.T) {
	zero := profile.IMSIdentityResult{}
	if got := NormalizeIMSIdentity(zero); got != zero {
		t.Fatalf("zero identity changed: %+v", got)
	}
	got := NormalizeIMSIdentity(profile.IMSIdentityResult{
		RequestedSource: " ISIM ", ActualSource: " unknown ", AKAAppPreference: " isim_strict ",
		IMPI: " impi ", IMPU: " sip:user@example.com ", Domain: " SIPS:ims.example.com;foo=bar ",
	})
	want := profile.IMSIdentityResult{
		RequestedSource: "isim", ActualSource: "derived", AKAAppPreference: "isim_strict",
		IMPI: "impi", IMPU: "sip:user@example.com", Domain: "ims.example.com",
	}
	if got != want {
		t.Fatalf("NormalizeIMSIdentity() = %+v, want %+v", got, want)
	}
}

func TestPrepareStartDerivedIdentityAndRedirect(t *testing.T) {
	provider := &identityProviderStub{err: errors.New("must not be called")}
	prepared, err := PrepareStart(
		"wwan0",
		profile.Profile{IMSI: "234102356143376", MCC: "234", MNC: "10"},
		" redirect.epdg.example ", profile.IMSIdentityResult{}, provider, nil,
	)
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("derived identity called provider %d times", provider.calls)
	}
	if prepared.EPDGAddr != "redirect.epdg.example" || prepared.EPDGSource != "redirect" {
		t.Fatalf("ePDG = %q source %q", prepared.EPDGAddr, prepared.EPDGSource)
	}
	if HasIMSIdentityResolution(prepared.IMSIdentity) {
		t.Fatalf("derived identity was applied during startup: %+v", prepared.IMSIdentity)
	}
	if !strings.HasPrefix(prepared.Profile.IMEI, "86034905") ||
		prepared.IdentityIMEISource != "carrier_device_model" {
		t.Fatalf("device identity = %q source %q", prepared.Profile.IMEI, prepared.IdentityIMEISource)
	}
}

func TestPrepareStartAutoFallsBackToUnappliedIdentity(t *testing.T) {
	provider := &identityProviderStub{err: errors.New("identity unavailable")}
	identity, err := resolveIdentitySource("auto", provider)
	if err != nil {
		t.Fatalf("resolveIdentitySource: %v", err)
	}
	if provider.calls != 1 || HasIMSIdentityResolution(identity) {
		t.Fatalf("auto fallback calls=%d identity=%+v", provider.calls, identity)
	}
}

func TestPrepareStartStrictISIMUsesAccessProvider(t *testing.T) {
	provider := &identityProviderStub{identity: profile.Identity{
		IMPI: " user@private.att.net ",
		IMPU: []string{"", " sip:user@one.att.net "},
	}}
	prepared, err := PrepareStart(
		"wwan0",
		profile.Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		"", profile.IMSIdentityResult{}, nil, accessStub{provider: provider},
	)
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	if provider.calls != 1 || prepared.IMSIdentity.Domain != "one.att.net" ||
		prepared.IMSIdentity.AKAAppPreference != profile.AKAAppISIMStrict {
		t.Fatalf("provider calls=%d identity=%+v", provider.calls, prepared.IMSIdentity)
	}
}

func TestPrepareStartSuppliedResolutionSkipsProvidersAndDetachesPlan(t *testing.T) {
	direct := &identityProviderStub{err: errors.New("direct called")}
	fallback := &identityProviderStub{err: errors.New("fallback called")}
	identity := profile.IMSIdentityResult{
		RequestedSource: " ISIM ", ActualSource: " ISIM ", AKAAppPreference: "isim_strict",
		Applied: true, IMPI: " user@ims.example ", IMPU: " sip:user@ims.example ", Domain: " ims.example ",
	}
	first, err := PrepareStart(
		"wwan0", profile.Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		"", identity, direct, accessStub{provider: fallback},
	)
	if err != nil {
		t.Fatalf("PrepareStart: %v", err)
	}
	second, err := PrepareStart(
		"wwan0", profile.Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"},
		"", identity, direct, accessStub{provider: fallback},
	)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("second PrepareStart = %+v err %v", second, err)
	}
	if direct.calls != 0 || fallback.calls != 0 {
		t.Fatalf("provider calls direct=%d fallback=%d", direct.calls, fallback.calls)
	}
	first.CarrierPlan.IKE.IKEProposals[0] = "changed"
	if second.CarrierPlan.IKE.IKEProposals[0] == "changed" {
		t.Fatal("prepared carrier proposals alias between calls")
	}
}

func TestPrepareStartStrictISIMErrors(t *testing.T) {
	value := profile.Profile{IMSI: "310280233621715", MCC: "310", MNC: "280"}
	if _, err := PrepareStart("dev", value, "", profile.IMSIdentityResult{}, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "provider 不支持") {
		t.Fatalf("missing provider error = %v", err)
	}
	transportErr := errors.New("SIM transport disconnected")
	if _, err := PrepareStart(
		"dev", value, "", profile.IMSIdentityResult{},
		&identityProviderStub{err: transportErr}, nil,
	); !errors.Is(err, transportErr) {
		t.Fatalf("transport error = %v", err)
	}
	tests := []struct {
		name     string
		identity profile.Identity
		want     string
	}{
		{name: "impi", identity: profile.Identity{IMPU: []string{"sip:user@ims.example"}}, want: "ISIM 身份不完整: 缺少 IMPI"},
		{name: "impu", identity: profile.Identity{IMPI: "user@ims.example"}, want: "ISIM 身份不完整: 缺少 IMPU"},
		{name: "domain", identity: profile.Identity{IMPI: "opaque", IMPU: []string{"tel:+15551234567"}}, want: "ISIM 身份不完整: 缺少 DOMAIN"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := PrepareStart(
				"dev", value, "", profile.IMSIdentityResult{},
				&identityProviderStub{identity: test.identity}, nil,
			)
			if err == nil || err.Error() != test.want {
				t.Fatalf("incomplete identity error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestIdentityDomain(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "sip parameters", values: []string{"sip:user@IMS.Example;user=phone"}, want: "IMS.Example"},
		{name: "sips query", values: []string{"sips:user@secure.example?subject=x"}, want: "secure.example"},
		{name: "last at", values: []string{"sip:user@realm@last.example"}, want: "last.example"},
		{name: "skip tel", values: []string{"tel:+15551234567", "user@fallback.example"}, want: "fallback.example"},
		{name: "none", values: []string{"tel:+15551234567", "opaque"}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := identityDomain(test.values...); got != test.want {
				t.Fatalf("identityDomain() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPrepareStartRejectsBlockedMCC(t *testing.T) {
	_, err := PrepareStart(
		"dev", profile.Profile{IMSI: "460001234567890", MCC: "460", MNC: "00"},
		"", profile.IMSIdentityResult{}, nil, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "460") {
		t.Fatalf("blocked MCC error = %v", err)
	}
}
