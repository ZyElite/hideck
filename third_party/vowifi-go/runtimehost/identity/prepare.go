package identity

import (
	"errors"
	"strings"

	internalaccess "github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	internalprofile "github.com/iniwex5/vowifi-go/internal/vowifi/profile"
	"github.com/iniwex5/vowifi-go/internal/vowifi/startup"
)

// ErrISIMUnavailable means the card authoritatively has no ISIM application.
// Callers may select USIM AKA for this case; all other read errors stay fatal.
var ErrISIMUnavailable = errors.New("identity: ISIM application unavailable")

// NormalizeProfile trims and normalises the profile fields.
func NormalizeProfile(p Profile) Profile {
	return Profile{
		IMSI: strings.TrimSpace(p.IMSI), MCC: strings.TrimSpace(p.MCC), MNC: strings.TrimSpace(p.MNC),
		IMEI: strings.TrimSpace(p.IMEI), UserAgent: strings.TrimSpace(p.UserAgent),
		SMSC: strings.TrimSpace(p.SMSC), IMSDomain: policy.NormalizeIMSDomain(p.IMSDomain),
	}
}

// ReadISIMIdentityFromAccess preserves the current provider-based entrypoint.
func ReadISIMIdentityFromAccess(access Access) (Identity, error) {
	if access == nil {
		return Identity{}, errors.New("identity: no modem access")
	}
	provider := access.IMSIdentityProvider()
	if provider == nil {
		return Identity{}, errors.New("identity: no IMS identity provider")
	}
	return provider.GetISIMIdentity()
}

type internalPrepareStartInput struct {
	deviceID            string
	profile             internalprofile.Profile
	runtimeEPDGOverride string
	identity            internalprofile.IMSIdentityResult
	provider            internalprofile.Provider
	access              internalaccess.Adapter
}

func (input PrepareStartInput) toInternal() internalPrepareStartInput {
	return internalPrepareStartInput{
		deviceID: input.DeviceID, profile: profileToInternal(input.Profile),
		runtimeEPDGOverride: input.RuntimeEPDGOverride,
		identity:            imsIdentityResultToInternal(input.IMSIdentityResult),
		provider:            adaptIdentityProvider(input.IdentityProvider),
		access:              adaptAccessAdapter(input.Access),
	}
}

// PrepareStart converts the recovered public input to the startup boundary.
func PrepareStart(input PrepareStartInput) (PreparedSession, error) {
	converted := input.toInternal()
	prepared, err := startup.PrepareStart(
		converted.deviceID, converted.profile, converted.runtimeEPDGOverride,
		converted.identity, converted.provider, converted.access,
	)
	if err != nil {
		return PreparedSession{}, err
	}
	return preparedSessionFromInternal(prepared), nil
}

func profileToInternal(value Profile) internalprofile.Profile {
	return internalprofile.Profile{
		IMSI: value.IMSI, MCC: value.MCC, MNC: value.MNC, IMEI: value.IMEI,
		UserAgent: value.UserAgent, SMSC: value.SMSC, IMSDomain: value.IMSDomain,
	}
}

func imsIdentityResultToInternal(value IMSIdentityResult) internalprofile.IMSIdentityResult {
	return internalprofile.IMSIdentityResult{
		RequestedSource: value.RequestedSource, ActualSource: value.ActualSource,
		AKAAppPreference: value.AKAAppPreference, Applied: value.Applied,
		IMPI: value.IMPI, IMPU: value.IMPU, Domain: value.Domain,
	}
}
