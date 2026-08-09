package profile

import (
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

// AKAAppPreference is retained for callers added after the original API.
type AKAAppPreference = string

const (
	AKAAppAuto       = "auto"
	AKAAppISIM       = "isim"
	AKAAppISIMStrict = "isim_strict"
	AKAAppUSIM       = "usim"

	AKAAppPreferenceAuto       = AKAAppAuto
	AKAAppPreferenceISIM       = AKAAppISIM
	AKAAppPreferenceISIMStrict = AKAAppISIMStrict
	AKAAppPreferenceUSIM       = AKAAppUSIM
	// USIM strict was introduced by the current host API. The original
	// profile normalizer deliberately maps it to USIM.
	AKAAppPreferenceUSIMStrict = "usim_strict"
)

type Profile struct {
	IMSI      string
	MCC       string
	MNC       string
	IMEI      string
	UserAgent string
	SMSC      string
	IMSDomain string
}

type Identity struct {
	IMPI   string
	IMPU   []string
	Domain string
}

// IMSIdentity preserves the name used by the reconstructed runtime.
type IMSIdentity = Identity

type Provider interface {
	GetISIMIdentity() (Identity, error)
}

type IMSIdentityResult struct {
	RequestedSource  string
	ActualSource     string
	AKAAppPreference string
	Applied          bool
	IMPI             string
	IMPU             string
	Domain           string
}

type AuthPlan struct {
	EPDGApp string
	IMSApp  string

	// These fields preserve the newer capability projection.
	AKAApp        AKAAppPreference
	ISIMAvailable bool
	USIMAvailable bool
}

func NewAuthPlan(epdgApp, imsApp string) AuthPlan {
	return AuthPlan{EPDGApp: epdgApp, IMSApp: imsApp}.Normalize()
}

func NormalizeAKAApp(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AKAAppAuto:
		return AKAAppAuto
	case AKAAppISIM:
		return AKAAppISIM
	case AKAAppISIMStrict:
		return AKAAppISIMStrict
	default:
		return AKAAppUSIM
	}
}

func (plan AuthPlan) Normalize() AuthPlan {
	plan.EPDGApp = NormalizeAKAApp(plan.EPDGApp)
	plan.IMSApp = NormalizeAKAApp(plan.IMSApp)
	if plan.AKAApp == "" {
		plan.AKAApp = plan.IMSApp
	}
	return plan
}

func (plan AuthPlan) IsZero() bool {
	return strings.TrimSpace(plan.EPDGApp) == "" && strings.TrimSpace(plan.IMSApp) == ""
}

type PreparedSession struct {
	Profile            Profile
	CarrierPlan        policy.CarrierPlan
	IMSIdentity        IMSIdentityResult
	AuthPlan           AuthPlan
	EPDGAddr           string
	EPDGSource         string
	IdentityIMEISource string
}

func (session PreparedSession) EffectiveAuthPlan() AuthPlan {
	if !session.AuthPlan.IsZero() {
		return session.AuthPlan.Normalize()
	}
	return AuthPlan{
		EPDGApp: AKAAppUSIM,
		IMSApp:  NormalizeAKAApp(session.IMSIdentity.AKAAppPreference),
	}.Normalize()
}
