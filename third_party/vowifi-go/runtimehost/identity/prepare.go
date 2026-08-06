package identity

import (
	"errors"
	"fmt"
	"strings"
)

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

	ident, err := ReadISIMIdentity(input.Access)
	if err != nil {
		return PreparedSession{}, fmt.Errorf("identity: read ISIM identity: %w", err)
	}

	// A partial ISIM identity (IMPI without IMPU) cannot drive a session.
	if ident.IMPI != "" && len(ident.IMPU) == 0 {
		return PreparedSession{}, errors.New("ISIM 身份不完整: IMPU 缺失")
	}

	// Build the IMS identity: IMPI = IMSI@domain, IMPU = sip:IMSI@domain.
	domain := ident.Domain
	if domain == "" {
		domain = defaultDomain(profile)
	}
	impi := ident.IMPI
	if impi == "" {
		impi = profile.IMSI + "@" + domain
	}
	impu := ""
	if len(ident.IMPU) > 0 {
		impu = ident.IMPU[0]
	}
	if impu == "" {
		impu = "sip:" + profile.IMSI + "@" + domain
	}

	imsIdentity := IMSIdentity{
		RequestedSource:  IMSIdentitySourceISIM,
		ActualSource:     IMSIdentitySourceISIM,
		AKAAppPreference: AKAAppPreferenceISIMStrict,
		Applied:          true,
		IMPI:             impi,
		IMPU:             impu,
		Domain:           domain,
	}

	carrier := EffectiveCarrier{MCC: profile.MCC, MNC: profile.MNC}

	// Resolve the ePDG endpoint.
	epdgAddr, epdgSource := resolveEPDG(input.RuntimeEPDGOverride, carrier)

	return PreparedSession{
		Profile:            profile,
		IMSIdentity:        imsIdentity,
		EffectiveCarrier:   carrier,
		EPDGSource:         epdgSource,
		EPDGAddr:           epdgAddr,
		IdentityIMEISource: string(IMSIdentitySourceISIM),
		NetworkMode:        "",
		StartupState:       StartupState{},
	}, nil
}

// defaultDomain derives the IMS domain from the carrier (3GPP TS 23.003).
func defaultDomain(p Profile) string {
	if p.MCC == "" || p.MNC == "" {
		return "ims.mnc000.mcc000.3gppnetwork.org"
	}
	return fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", p.MNC, p.MCC)
}

// resolveEPDG returns the ePDG FQDN for the carrier, honouring a runtime
// override.
func resolveEPDG(override string, carrier EffectiveCarrier) (addr, source string) {
	if override = strings.TrimSpace(override); override != "" {
		return override, "redirect"
	}
	if carrier.MCC != "" && carrier.MNC != "" {
		return fmt.Sprintf("epdg.epc.mnc%s.mcc%s.pub.3gppnetwork.org", carrier.MNC, carrier.MCC), "carrier"
	}
	return "", "none"
}
