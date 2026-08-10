package identity

import (
	"errors"

	"github.com/iniwex5/vowifi-go/internal/runtimehostcarrier"
	internalaccess "github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

var errNoProvider = errors.New("identity: no identity provider")

func (a accessAdapter) Capabilities() internalaccess.Capabilities {
	if a.host == nil {
		return internalaccess.Capabilities{}
	}
	value := a.host.Capabilities()
	return internalaccess.Capabilities{
		SIM: value.SIM || value.HasUSIM, ISIMIdentity: value.ISIMIdentity || value.HasISIM,
		ISIMAKA: value.ISIMAKA, Modem: value.Modem, Reader: value.Reader,
	}
}

func (a accessAdapter) IMSIdentityProvider() internalprofile.Provider {
	if a.host == nil {
		return nil
	}
	provider := a.host.IMSIdentityProvider()
	if provider == nil {
		return nil
	}
	return &identityProviderAdapter{provider: provider}
}

func (a identityProviderAdapter) GetISIMIdentity() (internalprofile.Identity, error) {
	if a.provider == nil {
		return internalprofile.Identity{}, nil
	}
	value, err := a.provider.GetISIMIdentity()
	if err != nil {
		return internalprofile.Identity{}, err
	}
	return internalprofile.Identity{
		IMPI: value.IMPI, IMPU: append([]string(nil), value.IMPU...), Domain: value.Domain,
	}, nil
}

// NewAccessAdapter adapts an Access surface.
func NewAccessAdapter(access Access) *currentAccessAdapter {
	return &currentAccessAdapter{access: access}
}

// NewIdentityProviderAdapter adapts an IMSIdentityProvider.
func NewIdentityProviderAdapter(provider IMSIdentityProvider) *currentIdentityProviderAdapter {
	return &currentIdentityProviderAdapter{provider: provider}
}

func (a *currentAccessAdapter) Capabilities() AccessCapabilities {
	if a == nil || a.access == nil {
		return AccessCapabilities{}
	}
	providerAvailable := a.access.IMSIdentityProvider() != nil
	return AccessCapabilities{ISIMIdentity: providerAvailable, HasISIM: providerAvailable}
}

func (a *currentAccessAdapter) IMSIdentityProvider() IMSIdentityProvider {
	if a == nil || a.access == nil {
		return nil
	}
	return a.access.IMSIdentityProvider()
}

func (a *currentIdentityProviderAdapter) GetISIMIdentity() (Identity, error) {
	if a == nil || a.provider == nil {
		return Identity{}, errNoProvider
	}
	value, err := a.provider.GetISIMIdentity()
	value.IMPU = append([]string(nil), value.IMPU...)
	return value, err
}

// preparedSessionFromInternal converts an internal prepared session.
func preparedSessionFromInternal(value internalprofile.PreparedSession) PreparedSession {
	internalCarrier := policy.EffectiveCarrierConfigFromCarrierPlan(value.CarrierPlan)
	carrierConfig := runtimehostcarrier.FromInternal(internalCarrier)
	return PreparedSession{
		Profile:          profileFromInternal(value.Profile),
		EffectiveCarrier: carrierConfig,
		IMSIdentity:      imsIdentityResultFromInternal(value.IMSIdentity),
		AuthPlan: AuthPlan{
			EPDGApp: value.AuthPlan.EPDGApp, IMSApp: value.AuthPlan.IMSApp,
		},
		EPDGAddr: value.EPDGAddr, EPDGSource: value.EPDGSource,
		IdentityIMEISource: value.IdentityIMEISource,
		CarrierConfig:      runtimehostcarrier.FromInternal(internalCarrier),
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
		RequestedSource: value.RequestedSource, ActualSource: value.ActualSource,
		AKAAppPreference: value.AKAAppPreference, Applied: value.Applied,
		IMPI: value.IMPI, IMPU: value.IMPU, Domain: value.Domain,
	}
}

func adaptIdentityProvider(provider IMSIdentityProvider) internalprofile.Provider {
	if provider == nil {
		return nil
	}
	return &identityProviderAdapter{provider: provider}
}

func adaptAccessAdapter(adapter AccessAdapter) internalaccess.Adapter {
	if adapter == nil {
		return nil
	}
	return &accessAdapter{host: adapter}
}
