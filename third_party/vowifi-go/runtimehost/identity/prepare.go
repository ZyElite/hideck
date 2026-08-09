package identity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/runtimehostcarrier"
	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

// ErrISIMUnavailable means the card authoritatively has no ISIM application.
// Callers may select USIM AKA for this case; all other read errors stay fatal.
var ErrISIMUnavailable = errors.New("identity: ISIM application unavailable")

// NormalizeProfile trims and normalises the profile fields.
func NormalizeProfile(p Profile) Profile {
	return Profile{
		IMSI: strings.TrimSpace(p.IMSI),
		MCC:  strings.TrimSpace(p.MCC),
		MNC:  strings.TrimSpace(p.MNC),
		IMEI: strings.TrimSpace(p.IMEI),
		SMSC: strings.TrimSpace(p.SMSC),
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

// PrepareStart prepares the IMS identity and session profile for a VoWiFi
// start. It reads the ISIM identity from the modem, applies the carrier
// profile and resolves the ePDG endpoint.
func PrepareStart(input PrepareStartInput) (PreparedSession, error) {
	profile := NormalizeProfile(input.Profile)
	if profile.IMSI == "" {
		return PreparedSession{}, errors.New("identity: empty IMSI in profile")
	}

	imsIdentity, err := resolveIMSIdentity(input.Access, profile)
	if err != nil {
		return PreparedSession{}, err
	}

	carrierConfig := runtimehostcarrier.FromInternal(policy.ResolveEffectiveCarrierConfig(profile.MCC, profile.MNC))
	effectiveCarrier := EffectiveCarrier{
		MCC: carrierConfig.MCC, MNC: carrierConfig.MNC, PresetID: carrierConfig.PresetID,
	}

	// Resolve the ePDG endpoint.
	epdgAddr, epdgSource := resolveConfiguredEPDG(input.RuntimeEPDGOverride, carrierConfig)

	return PreparedSession{
		Profile:            profile,
		IMSIdentity:        imsIdentity,
		EffectiveCarrier:   effectiveCarrier,
		CarrierConfig:      carrierConfig,
		EPDGSource:         epdgSource,
		EPDGAddr:           epdgAddr,
		IdentityIMEISource: string(imsIdentity.ActualSource),
		NetworkMode:        "",
		StartupState:       StartupState{},
	}, nil
}

func resolveIMSIdentity(access Access, profile Profile) (IMSIdentity, error) {
	ident, err := ReadISIMIdentity(access)
	if errors.Is(err, ErrISIMUnavailable) {
		return derivedUSIMIdentity(profile), nil
	}
	if err != nil {
		return IMSIdentity{}, fmt.Errorf("identity: read ISIM identity: %w", err)
	}
	if strings.TrimSpace(ident.IMPI) == "" || len(trimIdentityValues(ident.IMPU)) == 0 || strings.TrimSpace(ident.Domain) == "" {
		return IMSIdentity{}, fmt.Errorf("ISIM 身份不完整: impi=%t impu=%d domain=%t",
			strings.TrimSpace(ident.IMPI) != "", len(trimIdentityValues(ident.IMPU)), strings.TrimSpace(ident.Domain) != "")
	}
	return IMSIdentity{
		RequestedSource:  IMSIdentitySourceISIM,
		ActualSource:     IMSIdentitySourceISIM,
		AKAAppPreference: AKAAppPreferenceISIMStrict,
		Applied:          true,
		IMPI:             strings.TrimSpace(ident.IMPI),
		IMPU:             trimIdentityValues(ident.IMPU)[0],
		Domain:           strings.TrimSpace(ident.Domain),
	}, nil
}

func derivedUSIMIdentity(profile Profile) IMSIdentity {
	domain := defaultDomain(profile)
	return IMSIdentity{
		RequestedSource:  IMSIdentitySourceISIM,
		ActualSource:     IMSIdentitySourceUSIM,
		AKAAppPreference: AKAAppPreferenceUSIMStrict,
		Applied:          true,
		IMPI:             profile.IMSI + "@" + domain,
		IMPU:             "sip:" + profile.IMSI + "@" + domain,
		Domain:           domain,
	}
}

// defaultDomain derives the IMS domain from the carrier (3GPP TS 23.003).
func defaultDomain(p Profile) string {
	if p.MCC == "" || p.MNC == "" {
		return "ims.mnc000.mcc000.3gppnetwork.org"
	}
	return fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", paddedMNC(p.MNC), p.MCC)
}

// resolveEPDG returns the ePDG FQDN for the carrier, honouring a runtime
// override.
func resolveEPDG(override string, carrier EffectiveCarrier) (addr, source string) {
	if override = strings.TrimSpace(override); override != "" {
		return override, "redirect"
	}
	if carrier.MCC != "" && carrier.MNC != "" {
		return fmt.Sprintf("epdg.epc.mnc%s.mcc%s.pub.3gppnetwork.org", paddedMNC(carrier.MNC), carrier.MCC), "carrier"
	}
	return "", "none"
}

func resolveConfiguredEPDG(override string, config carrier.EffectiveCarrierConfig) (addr, source string) {
	if override = strings.TrimSpace(override); override != "" {
		return override, "redirect"
	}
	if addr = strings.TrimSpace(config.EPDGAddr); addr != "" {
		source = strings.TrimSpace(config.EPDGAddrSource)
		if source == "" {
			source = "carrier"
		}
		return addr, source
	}
	return resolveEPDG("", EffectiveCarrier{MCC: config.MCC, MNC: config.MNC, PresetID: config.PresetID})
}

func paddedMNC(mnc string) string {
	return common.Plmn3(mnc)
}
