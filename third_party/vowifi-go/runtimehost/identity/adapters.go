package identity

import (
	"errors"

	"github.com/iniwex5/vowifi-go/internal/runtimehostcarrier"
	internalaccess "github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

// errNoProvider is returned when no identity provider is installed.
var errNoProvider = errors.New("identity: no identity provider")

// Capabilities returns the modem's identity capabilities.
func (a *accessAdapter) Capabilities() Capabilities {
	if a == nil || a.access == nil {
		return Capabilities{}
	}
	provider := a.access.IMSIdentityProvider()
	if provider == nil {
		return Capabilities{}
	}
	// The provider surface does not expose ISIM/USIM presence directly;
	// report ISIM as available when a provider is present.
	return Capabilities{ISIMIdentity: true, HasISIM: true}
}

// IMSIdentityProvider returns the underlying identity provider.
func (a *accessAdapter) IMSIdentityProvider() IMSIdentityProvider {
	if a == nil || a.access == nil {
		return nil
	}
	return a.access.IMSIdentityProvider()
}

// GetISIMIdentity reads the ISIM identity through the provider.
func (a *identityProviderAdapter) GetISIMIdentity() (Identity, error) {
	if a == nil || a.provider == nil {
		return Identity{}, errNoProvider
	}
	return a.provider.GetISIMIdentity()
}

// NewAccessAdapter adapts an Access surface.
func NewAccessAdapter(access Access) *accessAdapter {
	return &accessAdapter{access: access}
}

// NewIdentityProviderAdapter adapts an IMSIdentityProvider.
func NewIdentityProviderAdapter(provider IMSIdentityProvider) *identityProviderAdapter {
	return &identityProviderAdapter{provider: provider}
}

// preparedSessionFromInternal converts an internal prepared session.
func preparedSessionFromInternal(value internalprofile.PreparedSession) PreparedSession {
	carrierConfig := runtimehostcarrier.FromInternal(
		policy.EffectiveCarrierConfigFromCarrierPlan(value.CarrierPlan),
	)
	return PreparedSession{
		Profile: profileFromInternal(value.Profile),
		EffectiveCarrier: EffectiveCarrier{
			MCC: carrierConfig.MCC, MNC: carrierConfig.MNC, PresetID: carrierConfig.PresetID,
		},
		CarrierConfig: carrierConfig,
		IMSIdentity:   imsIdentityResultFromInternal(value.IMSIdentity),
		AuthPlan: AuthPlan{
			EPDGApp: value.AuthPlan.EPDGApp, IMSApp: value.AuthPlan.IMSApp,
		},
		EPDGAddr: value.EPDGAddr, EPDGSource: value.EPDGSource,
		IdentityIMEISource: value.IdentityIMEISource,
	}
}

func profileFromInternal(value internalprofile.Profile) Profile {
	return Profile{
		IMSI: value.IMSI, MCC: value.MCC, MNC: value.MNC, IMEI: value.IMEI,
		UserAgent: value.UserAgent, SMSC: value.SMSC, IMSDomain: value.IMSDomain,
	}
}

func imsIdentityResultFromInternal(value internalprofile.IMSIdentityResult) IMSIdentityResult {
	return IMSIdentityResult{
		RequestedSource:  IMSIdentitySource(value.RequestedSource),
		ActualSource:     IMSIdentitySource(value.ActualSource),
		AKAAppPreference: AKAAppPreference(value.AKAAppPreference), Applied: value.Applied,
		IMPI: value.IMPI, IMPU: value.IMPU, Domain: value.Domain,
	}
}

type startupIdentityProvider struct{ provider IMSIdentityProvider }

func (adapter startupIdentityProvider) GetISIMIdentity() (internalprofile.Identity, error) {
	identity, err := adapter.provider.GetISIMIdentity()
	return internalprofile.Identity{
		IMPI: identity.IMPI, IMPU: append([]string(nil), identity.IMPU...), Domain: identity.Domain,
	}, err
}

func adaptIdentityProvider(provider IMSIdentityProvider) internalprofile.Provider {
	if provider == nil {
		return nil
	}
	return startupIdentityProvider{provider: provider}
}

type startupAccessAdapter struct{ access Access }

func (adapter startupAccessAdapter) Capabilities() internalaccess.Capabilities {
	providerAvailable := adapter.access != nil && adapter.access.IMSIdentityProvider() != nil
	result := internalaccess.Capabilities{ISIMIdentity: providerAvailable}
	if source, ok := adapter.access.(interface{ Capabilities() Capabilities }); ok {
		capabilities := source.Capabilities()
		result.SIM = capabilities.SIM || capabilities.HasUSIM
		result.ISIMIdentity = capabilities.ISIMIdentity || capabilities.HasISIM
		result.ISIMAKA = capabilities.ISIMAKA
		result.Modem = capabilities.Modem
		result.Reader = capabilities.Reader
	}
	return result
}

func (adapter startupAccessAdapter) IMSIdentityProvider() internalprofile.Provider {
	if adapter.access == nil {
		return nil
	}
	return adaptIdentityProvider(adapter.access.IMSIdentityProvider())
}

func adaptAccessAdapter(adapter Access) internalaccess.Adapter {
	if adapter == nil {
		return nil
	}
	return startupAccessAdapter{access: adapter}
}
