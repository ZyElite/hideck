package identity

import "errors"

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
	return Capabilities{HasISIM: true}
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
func preparedSessionFromInternal(p PreparedSession) PreparedSession {
	return p
}
