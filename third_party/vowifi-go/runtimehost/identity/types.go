// Package identity prepares the IMS identity for a VoWiFi session: it reads
// the ISIM/USIM identity from the modem, applies the carrier profile and
// produces a PreparedSession consumed by the runtime host.
//
// Reconstructed from the decompiled engine/runtimehost/identity.
package identity

import "github.com/iniwex5/vowifi-go/runtimehost/carrier"

// Profile is the raw IMS profile of a device.
type Profile struct {
	IMSI string
	MCC  string
	MNC  string
	IMEI string
	SMSC string
}

// IMSIdentitySource is where the IMS identity was read from.
type IMSIdentitySource string

const (
	IMSIdentitySourceISIM IMSIdentitySource = "isim"
	IMSIdentitySourceUSIM IMSIdentitySource = "usim"
	IMSIdentitySourceIMEI IMSIdentitySource = "imei"
)

// AKAAppPreference selects the SIM application used for AKA.
type AKAAppPreference string

const (
	AKAAppPreferenceISIMStrict AKAAppPreference = "isim_strict"
	AKAAppPreferenceUSIMStrict AKAAppPreference = "usim_strict"
	AKAAppPreferenceAuto       AKAAppPreference = "auto"
)

// IMSIdentity is the resolved IMS identity.
type IMSIdentity struct {
	RequestedSource  IMSIdentitySource
	ActualSource     IMSIdentitySource
	AKAAppPreference AKAAppPreference
	Applied          bool
	IMPI             string
	IMPU             string
	Domain           string
}

// EffectiveCarrier is the resolved carrier (PLMN) for the session.
type EffectiveCarrier struct {
	MCC      string
	MNC      string
	PresetID string
}

// StartupState carries the network state at startup.
type StartupState struct {
	NetworkMode string
}

// PreparedSession is the outcome of PrepareStart.
type PreparedSession struct {
	Profile            Profile
	IMSIdentity        IMSIdentity
	EffectiveCarrier   EffectiveCarrier
	CarrierConfig      carrier.EffectiveCarrierConfig
	EPDGSource         string
	EPDGAddr           string
	IdentityIMEISource string
	NetworkMode        string
	StartupState       StartupState
}

// PrepareStartInput is the input to PrepareStart.
type PrepareStartInput struct {
	DeviceID            string
	Profile             Profile
	RuntimeEPDGOverride string
	Access              Access
}

// Access is the modem access surface used to read the identity.
type Access interface {
	IMSIdentityProvider() IMSIdentityProvider
}

// Capabilities describes the modem's identity capabilities.
type Capabilities struct {
	HasISIM bool
	HasUSIM bool
}

// IMSIdentityProvider reads the ISIM identity from the SIM.
type IMSIdentityProvider interface {
	GetISIMIdentity() (Identity, error)
}

// Identity is a raw ISIM identity.
type Identity struct {
	IMSI   string
	IMPI   string
	IMPU   []string
	Domain string
}

// accessAdapter adapts an Access to the identity provider surface.
type accessAdapter struct {
	access Access
}

// identityProviderAdapter adapts an IMSIdentityProvider.
type identityProviderAdapter struct {
	provider IMSIdentityProvider
}
