package identity

import (
	"errors"
	"reflect"
	"testing"

	internalaccess "github.com/iniwex5/vowifi-go/internal/vowifi/access"
	internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

var (
	_ func(LogicalChannelTransport) (Identity, error)  = ReadISIMIdentity
	_ func(PrepareStartInput) (PreparedSession, error) = PrepareStart
	_ internalaccess.Adapter                           = accessAdapter{}
	_ internalaccess.Adapter                           = (*accessAdapter)(nil)
	_ internalprofile.Provider                         = identityProviderAdapter{}
	_ internalprofile.Provider                         = (*identityProviderAdapter)(nil)
)

type recoveredPreparedSessionPrefix struct {
	Profile            Profile
	EffectiveCarrier   carrier.EffectiveCarrierConfig
	IMSIdentity        IMSIdentityResult
	AuthPlan           AuthPlan
	EPDGAddr           string
	EPDGSource         string
	IdentityIMEISource string
}

type recoveredIdentityPrefix struct {
	IMPI   string
	IMPU   []string
	Domain string
}

type recoveredCapabilitiesPrefix struct {
	SIM          bool
	ISIMIdentity bool
	ISIMAKA      bool
	Modem        bool
	Reader       bool
}

func TestRecoveredPublicTypePrefixes(t *testing.T) {
	assertStructPrefix(t, reflect.TypeOf(PreparedSession{}), reflect.TypeOf(recoveredPreparedSessionPrefix{}))
	assertStructPrefix(t, reflect.TypeOf(Identity{}), reflect.TypeOf(recoveredIdentityPrefix{}))
	assertStructPrefix(t, reflect.TypeOf(AccessCapabilities{}), reflect.TypeOf(recoveredCapabilitiesPrefix{}))
	identityResult := reflect.TypeOf(IMSIdentityResult{})
	for index := 0; index < 3; index++ {
		if identityResult.Field(index).Type.Kind() != reflect.String {
			t.Fatalf("IMSIdentityResult field %d type = %s", index, identityResult.Field(index).Type)
		}
	}
}

func assertStructPrefix(t *testing.T, actual, prefix reflect.Type) {
	t.Helper()
	if actual.NumField() < prefix.NumField() {
		t.Fatalf("%s has %d fields, want prefix of %d", actual, actual.NumField(), prefix.NumField())
	}
	for index := 0; index < prefix.NumField(); index++ {
		got, want := actual.Field(index), prefix.Field(index)
		if got.Name != want.Name || got.Type != want.Type || got.Offset != want.Offset {
			t.Fatalf("%s field %d = %s %s @%d, want %s %s @%d",
				actual, index, got.Name, got.Type, got.Offset, want.Name, want.Type, want.Offset)
		}
	}
}

type adapterProviderStub struct {
	identity Identity
	err      error
}

func (provider *adapterProviderStub) GetISIMIdentity() (Identity, error) {
	return provider.identity, provider.err
}

type adapterAccessStub struct {
	capabilities AccessCapabilities
	provider     IMSIdentityProvider
}

func (access adapterAccessStub) Capabilities() AccessCapabilities { return access.capabilities }
func (access adapterAccessStub) IMSIdentityProvider() IMSIdentityProvider {
	return access.provider
}

func TestRecoveredAccessAdaptersPreserveCapabilitiesAndErrors(t *testing.T) {
	providerErr := errors.New("modem identity transport failed")
	provider := &adapterProviderStub{err: providerErr}
	adapted := accessAdapter{host: adapterAccessStub{
		capabilities: AccessCapabilities{HasISIM: true, HasUSIM: true, ISIMAKA: true, Modem: true},
		provider:     provider,
	}}
	capabilities := capabilitiesFromInternalAdapter(&adapted)
	if !capabilities.SIM || !capabilities.ISIMIdentity || !capabilities.ISIMAKA || !capabilities.Modem {
		t.Fatalf("adapted capabilities = %+v", capabilities)
	}
	if _, err := adapted.IMSIdentityProvider().GetISIMIdentity(); !errors.Is(err, providerErr) {
		t.Fatalf("provider error = %v", err)
	}
}

func capabilitiesFromInternalAdapter(adapter internalaccess.Adapter) internalaccess.Capabilities {
	return adapter.Capabilities()
}

func TestRecoveredIdentityProviderAdapterOwnsIMPU(t *testing.T) {
	source := Identity{IMPI: "impi", IMPU: []string{"sip:impu"}, Domain: "ims.example"}
	adapted := identityProviderAdapter{provider: &adapterProviderStub{identity: source}}
	got, err := adapted.GetISIMIdentity()
	if err != nil {
		t.Fatalf("GetISIMIdentity() error = %v", err)
	}
	got.IMPU[0] = "changed"
	if source.IMPU[0] == "changed" {
		t.Fatal("identity provider adapter aliased the public IMPU slice")
	}
	if zero, err := (identityProviderAdapter{}).GetISIMIdentity(); err != nil || !reflect.DeepEqual(zero, internalprofile.Identity{}) {
		t.Fatalf("nil provider = %+v, %v", zero, err)
	}
}

func TestCurrentAdaptersRemainExplicit(t *testing.T) {
	if _, err := NewIdentityProviderAdapter(nil).GetISIMIdentity(); !errors.Is(err, errNoProvider) {
		t.Fatalf("nil current provider error = %v", err)
	}
	provider := &adapterProviderStub{}
	access := NewAccessAdapter(adapterAccessStub{provider: provider})
	if !access.Capabilities().HasISIM || access.IMSIdentityProvider() != provider {
		t.Fatalf("current access adapter = %+v", access.Capabilities())
	}
	if _, err := ReadISIMIdentityFromAccess(nil); err == nil {
		t.Fatal("nil current access succeeded")
	}
}

func TestPreparedCarrierProjectionsAreDetached(t *testing.T) {
	prepared, err := PrepareStart(PrepareStartInput{
		Profile: Profile{IMSI: "234102356143376", MCC: "234", MNC: "10"},
	})
	if err != nil {
		t.Fatalf("PrepareStart() error = %v", err)
	}
	prepared.EffectiveCarrier.IKEProposals[0] = "changed"
	if prepared.CarrierConfig.IKEProposals[0] == "changed" {
		t.Fatal("current CarrierConfig aliases recovered EffectiveCarrier slices")
	}
	currentOnly := PreparedSession{CarrierConfig: prepared.CarrierConfig}
	if got := currentOnly.ResolvedCarrierConfig().PresetID; got != "giffgaff_23410" {
		t.Fatalf("current carrier fallback preset = %q", got)
	}
}
