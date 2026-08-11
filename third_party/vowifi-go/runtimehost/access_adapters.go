package runtimehost

import (
	enginesim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

func (adapter accessAdapter) Capabilities() access.Capabilities {
	if adapter.host == nil {
		return access.Capabilities{}
	}
	value := adapter.host.Capabilities()
	return access.Capabilities{
		SIM: value.SIM || value.HasUSIM, ISIMIdentity: value.ISIMIdentity || value.HasISIM,
		ISIMAKA: value.ISIMAKA, Modem: value.Modem, Reader: value.Reader,
	}
}

func (adapter accessAdapter) IMSIdentityProvider() profile.Provider {
	if adapter.host == nil {
		return nil
	}
	provider := adapter.host.IMSIdentityProvider()
	if provider == nil {
		return nil
	}
	return identityProviderAdapter{provider: provider}
}

func (adapter identityProviderAdapter) GetISIMIdentity() (profile.Identity, error) {
	if adapter.provider == nil {
		return profile.Identity{}, nil
	}
	value, err := adapter.provider.GetISIMIdentity()
	if err != nil {
		return profile.Identity{}, err
	}
	return profile.Identity{
		IMPI: value.IMPI, IMPU: append([]string(nil), value.IMPU...), Domain: value.Domain,
	}, nil
}

func (adapter modemAccessAdapter) Capabilities() ModemCapabilities {
	if adapter.modem == nil {
		return ModemCapabilities{}
	}
	_, hasIdentity := adapter.modem.(IMSIdentityProvider)
	_, hasISIMAKA := adapter.modem.(enginesim.ISIMAKAProvider)
	return ModemCapabilities{
		SIM: true, ISIMIdentity: hasIdentity, ISIMAKA: hasISIMAKA, Modem: true,
		HasISIM: hasIdentity, HasUSIM: true,
	}
}

func (adapter modemAccessAdapter) IMSIdentityProvider() IMSIdentityProvider {
	if adapter.modem == nil {
		return nil
	}
	provider, _ := adapter.modem.(IMSIdentityProvider)
	return provider
}
