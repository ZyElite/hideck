package imscore

import (
	"errors"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/internal/vowifi/profile"
)

func ResolveIMSIdentitySource(source string, provider profile.Provider) (profile.IMSIdentityResult, error) {
	source = policy.NormalizeIMSIdentitySource(source)
	if source == "derived" {
		return profile.IMSIdentityResult{}, nil
	}
	if provider == nil {
		if source == "auto" {
			return profile.IMSIdentityResult{}, nil
		}
		return profile.IMSIdentityResult{}, errors.New("IMSIdentitySource=isim 但 provider 不支持 ISIM 身份读取")
	}
	identity, err := provider.GetISIMIdentity()
	if err != nil {
		if source == "auto" {
			return profile.IMSIdentityResult{}, nil
		}
		return profile.IMSIdentityResult{}, err
	}
	result, err := normalizeUsableISIMIdentity(source, identity)
	if err != nil && source == "auto" {
		return profile.IMSIdentityResult{}, nil
	}
	return result, err
}

func ApplyResolvedIMSIdentityToConfig(cfg *IMSConfig, identity profile.IMSIdentityResult, mcc string) error {
	if cfg == nil {
		return errors.New("IMSConfig 为空")
	}
	if !identity.Applied {
		return nil
	}
	impi := strings.TrimSpace(identity.IMPI)
	impu := strings.TrimSpace(identity.IMPU)
	domain := policy.NormalizeIMSDomain(identity.Domain)
	if impi == "" || impu == "" || domain == "" {
		return errors.New("ISIM 身份不完整: 缺少 IMPI/IMPU/DOMAIN")
	}
	applyNormalizedISIMIdentityToConfig(cfg, identity, impi, impu, domain, mcc)
	return nil
}

func normalizeUsableISIMIdentity(source string, identity profile.Identity) (profile.IMSIdentityResult, error) {
	impi := strings.TrimSpace(identity.IMPI)
	if impi == "" {
		return profile.IMSIdentityResult{}, errors.New("ISIM 身份不完整: 缺少 IMPI")
	}
	impu := firstNonBlank(identity.IMPU...)
	if impu == "" {
		return profile.IMSIdentityResult{}, errors.New("ISIM 身份不完整: 缺少 IMPU")
	}
	domain := policy.NormalizeIMSDomain(identity.Domain)
	if domain == "" {
		domain = domainFromISIMIdentity(identity)
	}
	if domain == "" {
		return profile.IMSIdentityResult{}, errors.New("ISIM 身份不完整: 缺少 DOMAIN")
	}
	return profile.IMSIdentityResult{
		RequestedSource: source, ActualSource: "isim", AKAAppPreference: profile.AKAAppISIMStrict,
		Applied: true, IMPI: impi, IMPU: impu, Domain: domain,
	}, nil
}

func applyNormalizedISIMIdentityToConfig(
	cfg *IMSConfig,
	identity profile.IMSIdentityResult,
	impi, impu, domain, mcc string,
) {
	cfg.Domain, cfg.Realm, cfg.IMPI, cfg.IMPU = domain, domain, impi, impu
	cfg.IMPUs = nil
	seed := stablePANIGenerationSeed([]string{identity.IMPI, identity.IMPU, identity.Domain, identity.ActualSource})
	cfg.PAccessNetworkInfo = AppendPAccessNetworkCountry(
		GenerateStablePAccessNetworkInfo(seed), CountryISO2FromMCC(mcc),
	)
}

func domainFromISIMIdentity(identity profile.Identity) string {
	if domain := domainFromIMSIdentityValue(firstNonBlank(identity.IMPU...)); domain != "" {
		return domain
	}
	return domainFromIMSIdentityValue(identity.IMPI)
}

func domainFromIMSIdentityValue(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if value == "" || strings.HasPrefix(lower, "tel:") {
		return ""
	}
	if strings.HasPrefix(lower, "sips:") {
		value = strings.TrimSpace(value[len("sips:"):])
	} else if strings.HasPrefix(lower, "sip:") {
		value = strings.TrimSpace(value[len("sip:"):])
	}
	index := strings.LastIndexByte(value, '@')
	if index < 0 || index+1 >= len(value) {
		return ""
	}
	domain := value[index+1:]
	if end := strings.IndexAny(domain, ";?"); end >= 0 {
		domain = domain[:end]
	}
	return policy.NormalizeIMSDomain(strings.Trim(domain, "<>"))
}
