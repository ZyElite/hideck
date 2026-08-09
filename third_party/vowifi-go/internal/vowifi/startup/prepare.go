package startup

import (
	"errors"
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/access"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

const redirectEPDGSource = "redirect"

func PrepareStart(
	deviceID string,
	value profile.Profile,
	runtimeEPDGOverride string,
	identity profile.IMSIdentityResult,
	provider profile.Provider,
	adapter access.Adapter,
) (profile.PreparedSession, error) {
	built, err := profile.Build(value, "")
	if err != nil {
		return profile.PreparedSession{}, err
	}
	if built.MCC == "" || built.MNC == "" {
		return profile.PreparedSession{}, fmt.Errorf("缺少 SIM 归属 MCC/MNC: %s", built.IMSI)
	}
	if policy.IsVoWiFiBlockedMCC(built.MCC) {
		return profile.PreparedSession{}, policy.NewVoWiFiBlockedMCCError(built.MCC)
	}
	carrierPlan := policy.CarrierPlanFromEffectiveConfig(
		policy.ResolveEffectiveCarrierConfig(built.MCC, built.MNC),
	)
	applyCarrierProfile(&built, carrierPlan)
	resolvedIMEI, imeiSource := profile.ResolveIdentityIMEI(
		built.IMSI, built.IMEI, built.UserAgent, carrierPlan,
	)
	if resolvedIMEI = strings.TrimSpace(resolvedIMEI); resolvedIMEI == "" {
		return profile.PreparedSession{}, errors.New("无法确定 VoWiFi 身份 IMEI")
	}
	built.IMEI = resolvedIMEI
	identity, err = resolveIdentity(carrierPlan, identity, provider, adapter)
	if err != nil {
		return profile.PreparedSession{}, err
	}
	epdgAddr, epdgSource := selectEPDG(runtimeEPDGOverride, carrierPlan)
	return profile.PreparedSession{
		Profile: profile.Normalize(built), CarrierPlan: carrierPlan,
		IMSIdentity: identity,
		AuthPlan: profile.AuthPlan{
			EPDGApp: profile.AKAAppUSIM,
			IMSApp:  profile.NormalizeAKAApp(identity.AKAAppPreference),
		},
		EPDGAddr: epdgAddr, EPDGSource: epdgSource, IdentityIMEISource: imeiSource,
	}, nil
}

func applyCarrierProfile(value *profile.Profile, plan policy.CarrierPlan) {
	if domain := policy.NormalizeIMSDomain(plan.IMS.Domain); domain != "" {
		value.IMSDomain = domain
	}
	if userAgent := strings.TrimSpace(plan.IMS.UserAgent); userAgent != "" {
		value.UserAgent = userAgent
	}
}

func resolveIdentity(
	plan policy.CarrierPlan,
	identity profile.IMSIdentityResult,
	provider profile.Provider,
	adapter access.Adapter,
) (profile.IMSIdentityResult, error) {
	identity = NormalizeIMSIdentity(identity)
	if HasIMSIdentityResolution(identity) {
		return identity, nil
	}
	if provider == nil && adapter != nil {
		provider = adapter.IMSIdentityProvider()
	}
	return resolveIdentitySource(plan.IMS.IdentitySource, provider)
}

func resolveIdentitySource(
	source string,
	provider profile.Provider,
) (profile.IMSIdentityResult, error) {
	source = policy.NormalizeIMSIdentitySource(source)
	if source == "derived" {
		return profile.IMSIdentityResult{}, nil
	}
	if provider == nil {
		if source == "auto" {
			return profile.IMSIdentityResult{}, nil
		}
		return profile.IMSIdentityResult{}, errors.New(
			"IMSIdentitySource=isim 但 provider 不支持 ISIM 身份读取",
		)
	}
	identity, err := provider.GetISIMIdentity()
	if err != nil {
		if source == "auto" {
			return profile.IMSIdentityResult{}, nil
		}
		return profile.IMSIdentityResult{}, err
	}
	result, err := normalizedISIMIdentity(source, identity)
	if err != nil && source == "auto" {
		return profile.IMSIdentityResult{}, nil
	}
	return result, err
}

func normalizedISIMIdentity(
	requestedSource string,
	identity profile.Identity,
) (profile.IMSIdentityResult, error) {
	impi := strings.TrimSpace(identity.IMPI)
	if impi == "" {
		return profile.IMSIdentityResult{}, errors.New("ISIM 身份不完整: 缺少 IMPI")
	}
	impu := firstIdentity(identity.IMPU)
	if impu == "" {
		return profile.IMSIdentityResult{}, errors.New("ISIM 身份不完整: 缺少 IMPU")
	}
	domain := policy.NormalizeIMSDomain(identity.Domain)
	if domain == "" {
		domain = identityDomain(impu, impi)
	}
	if domain == "" {
		return profile.IMSIdentityResult{}, errors.New("ISIM 身份不完整: 缺少 DOMAIN")
	}
	return profile.IMSIdentityResult{
		RequestedSource: requestedSource, ActualSource: "isim",
		AKAAppPreference: profile.AKAAppISIMStrict, Applied: true,
		IMPI: impi, IMPU: impu, Domain: domain,
	}, nil
}

func firstIdentity(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func identityDomain(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "tel:") {
			continue
		}
		if strings.HasPrefix(lower, "sips:") {
			value = strings.TrimSpace(value[len("sips:"):])
		} else if strings.HasPrefix(lower, "sip:") {
			value = strings.TrimSpace(value[len("sip:"):])
		}
		if index := strings.LastIndexByte(value, '@'); index >= 0 {
			domain := value[index+1:]
			if end := strings.IndexAny(domain, ";?"); end >= 0 {
				domain = domain[:end]
			}
			if domain = policy.NormalizeIMSDomain(strings.Trim(domain, "<>")); domain != "" {
				return domain
			}
		}
	}
	return ""
}

func selectEPDG(override string, plan policy.CarrierPlan) (string, string) {
	if override = strings.TrimSpace(override); override != "" {
		return override, redirectEPDGSource
	}
	return strings.TrimSpace(plan.EPDG.Addr), strings.TrimSpace(plan.EPDG.AddrSource)
}

func HasIMSIdentityResolution(identity profile.IMSIdentityResult) bool {
	return identity.Applied ||
		strings.TrimSpace(identity.RequestedSource) != "" ||
		strings.TrimSpace(identity.ActualSource) != "" ||
		strings.TrimSpace(identity.AKAAppPreference) != "" ||
		strings.TrimSpace(identity.IMPI) != "" ||
		strings.TrimSpace(identity.IMPU) != "" ||
		strings.TrimSpace(identity.Domain) != ""
}

func NormalizeIMSIdentity(identity profile.IMSIdentityResult) profile.IMSIdentityResult {
	if !HasIMSIdentityResolution(identity) {
		return identity
	}
	identity.RequestedSource = policy.NormalizeIMSIdentitySource(identity.RequestedSource)
	identity.ActualSource = policy.NormalizeIMSIdentitySource(identity.ActualSource)
	identity.AKAAppPreference = strings.TrimSpace(identity.AKAAppPreference)
	identity.IMPI = strings.TrimSpace(identity.IMPI)
	identity.IMPU = strings.TrimSpace(identity.IMPU)
	identity.Domain = policy.NormalizeIMSDomain(identity.Domain)
	return identity
}
