package identity

import (
	"errors"
	"strings"

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

// ReadISIMIdentity reads the ISIM identity through the modem access surface.
func ReadISIMIdentity(access Access) (Identity, error) {
	if access == nil {
		return Identity{}, errors.New("identity: no modem access")
	}
	provider := access.IMSIdentityProvider()
	if provider == nil {
		return Identity{}, errors.New("identity: no IMS identity provider")
	}
	return provider.GetISIMIdentity()
}

// PrepareStart converts the public host input to the restored startup boundary.
func PrepareStart(input PrepareStartInput) (PreparedSession, error) {
	prepared, err := startup.PrepareStart(
		input.DeviceID,
		profileToInternal(input.Profile),
		input.RuntimeEPDGOverride,
		imsIdentityResultToInternal(input.IMSIdentityResult),
		adaptIdentityProvider(input.IdentityProvider),
		adaptAccessAdapter(input.Access),
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
		RequestedSource: string(value.RequestedSource), ActualSource: string(value.ActualSource),
		AKAAppPreference: string(value.AKAAppPreference), Applied: value.Applied,
		IMPI: value.IMPI, IMPU: value.IMPU, Domain: value.Domain,
	}
}
