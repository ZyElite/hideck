// Package identity prepares the IMS identity for a VoWiFi session: it reads
// the ISIM/USIM identity from the modem, applies the carrier profile and
// produces a PreparedSession consumed by the runtime host.
//
// Reconstructed from the decompiled engine/runtimehost/identity.
package identity

import "github.com/iniwex5/vowifi-go/runtimehost/carrier"

// Profile is the raw IMS profile of a device.
type Profile struct {
	IMSI      string
	MCC       string
	MNC       string
	IMEI      string
	UserAgent string
	SMSC      string
	IMSDomain string
}

// IMSIdentitySource is where the IMS identity was read from.
type IMSIdentitySource = string

const (
	IMSIdentitySourceISIM    IMSIdentitySource = "isim"
	IMSIdentitySourceUSIM    IMSIdentitySource = "usim"
	IMSIdentitySourceIMEI    IMSIdentitySource = "imei"
	IMSIdentitySourceDerived IMSIdentitySource = "derived"
)

// AKAAppPreference selects the SIM application used for AKA.
type AKAAppPreference = string

const (
	AKAAppPreferenceISIMStrict AKAAppPreference = "isim_strict"
	AKAAppPreferenceUSIMStrict AKAAppPreference = "usim_strict"
	AKAAppPreferenceAuto       AKAAppPreference = "auto"
)

// IMSIdentityResult is the recovered startup identity projection.
type IMSIdentityResult struct {
	RequestedSource  string
	ActualSource     string
	AKAAppPreference string
	Applied          bool
	IMPI             string
	IMPU             string
	Domain           string
}

// IMSIdentity preserves the name added by the current host API.
type IMSIdentity = IMSIdentityResult

// AuthPlan selects the SIM applications used by SWu and IMS authentication.
type AuthPlan struct {
	EPDGApp string
	IMSApp  string
}

// EffectiveCarrier preserves the current name for the recovered full config.
type EffectiveCarrier = carrier.EffectiveCarrierConfig

// StartupState carries the network state at startup.
type StartupState struct {
	NetworkMode string
}

// PreparedSession is the outcome of PrepareStart.
type PreparedSession struct {
	Profile            Profile
	EffectiveCarrier   carrier.EffectiveCarrierConfig
	IMSIdentity        IMSIdentityResult
	AuthPlan           AuthPlan
	EPDGAddr           string
	EPDGSource         string
	IdentityIMEISource string

	// Additive current-host projections follow the recovered field prefix.
	CarrierConfig carrier.EffectiveCarrierConfig
	NetworkMode   string
	StartupState  StartupState
}

// PrepareStartInput is the input to PrepareStart.
type PrepareStartInput struct {
	DeviceID            string
	Profile             Profile
	RuntimeEPDGOverride string
	IMSIdentityResult   IMSIdentityResult
	IdentityProvider    IMSIdentityProvider
	Access              AccessAdapter
}

// AccessCapabilities describes the modem identity and AKA facilities.
type AccessCapabilities struct {
	SIM          bool
	ISIMIdentity bool
	ISIMAKA      bool
	Modem        bool
	Reader       bool

	// These names preserve the current host projection.
	HasISIM bool
	HasUSIM bool
}

// Capabilities preserves the name added by the current host API.
type Capabilities = AccessCapabilities

// AccessAdapter is the recovered modem identity access surface.
type AccessAdapter interface {
	Capabilities() AccessCapabilities
	IMSIdentityProvider() IMSIdentityProvider
}

// Access preserves the minimal provider surface added by the current host API.
type Access interface {
	IMSIdentityProvider() IMSIdentityProvider
}

// IMSIdentityProvider reads the ISIM identity from the SIM.
type IMSIdentityProvider interface {
	GetISIMIdentity() (Identity, error)
}

// Identity is a raw ISIM identity.
type Identity struct {
	IMPI   string
	IMPU   []string
	Domain string

	// IMSI was added by the current host API.
	IMSI string
}

// accessAdapter adapts the public host surface to startup internals.
type accessAdapter struct {
	host AccessAdapter
}

// identityProviderAdapter adapts an IMSIdentityProvider.
type identityProviderAdapter struct {
	provider IMSIdentityProvider
}

type currentAccessAdapter struct{ access Access }
type currentIdentityProviderAdapter struct{ provider IMSIdentityProvider }

// ResolvedCarrierConfig selects the recovered field before the current alias.
func (session PreparedSession) ResolvedCarrierConfig() carrier.EffectiveCarrierConfig {
	if session.EffectiveCarrier.MCC != "" || session.EffectiveCarrier.MNC != "" {
		return session.EffectiveCarrier
	}
	return session.CarrierConfig
}
