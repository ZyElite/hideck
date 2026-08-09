package policy

import (
	"net"
	"reflect"
	"strings"
)

func NormalizeIMSRegisterPolicy(policy IMSRegisterPolicy) IMSRegisterPolicy {
	if isZeroIMSRegisterPolicy(policy) {
		return policy
	}
	defaults := DefaultIMSRegisterPolicy()
	policy.ID = strings.TrimSpace(policy.ID)
	if policy.ID == "" {
		policy.ID = defaults.ID
	}
	policy.TemporaryStatusCodes = normalizeSIPStatusCodeList(policy.TemporaryStatusCodes)
	if len(policy.TemporaryStatusCodes) == 0 {
		policy.TemporaryStatusCodes = defaults.TemporaryStatusCodes
	}
	policy.ForbiddenStatusCodes = normalizeSIPStatusCodeList(policy.ForbiddenStatusCodes)
	policy.InitialRejectFallbackStatusCodes = normalizeSIPStatusCodeList(policy.InitialRejectFallbackStatusCodes)
	if len(policy.InitialRejectFallbackStatusCodes) == 0 {
		policy.InitialRejectFallbackStatusCodes = defaults.InitialRejectFallbackStatusCodes
	}
	return policy
}

func NormalizeIMSRegisterTemplate(template IMSRegisterTemplate) IMSRegisterTemplate {
	defaults := DefaultIMSRegisterTemplate()
	if isZeroIMSRegisterTemplate(template) {
		return defaults
	}
	template.ID = valueOr(strings.TrimSpace(template.ID), defaults.ID)
	template.ContactMode = normalizeContactMode(template.ContactMode, defaults.ContactMode)
	template.FixedPANI = strings.TrimSpace(template.FixedPANI)
	template.SupportedHeader = valueOr(strings.TrimSpace(template.SupportedHeader), defaults.SupportedHeader)
	template.AllowHeader = valueOr(strings.TrimSpace(template.AllowHeader), defaults.AllowHeader)
	template.AccessType = valueOr(strings.TrimSpace(template.AccessType), defaults.AccessType)
	template.ICSIRef = valueOr(strings.TrimSpace(template.ICSIRef), defaults.ICSIRef)
	template.VoiceSupportedHeader = valueOr(strings.TrimSpace(template.VoiceSupportedHeader), defaults.VoiceSupportedHeader)
	template.VoiceAllowHeader = valueOr(strings.TrimSpace(template.VoiceAllowHeader), defaults.VoiceAllowHeader)
	template.VoiceAcceptContact = valueOr(strings.TrimSpace(template.VoiceAcceptContact), defaults.VoiceAcceptContact)
	template.VoicePPreferredService = valueOr(strings.TrimSpace(template.VoicePPreferredService), defaults.VoicePPreferredService)
	template.SecAgreeMode = normalizeIMSRegisterSecAgreeMode(template.SecAgreeMode)
	if template.SecAgreeMode == "" {
		template.SecAgreeMode = defaults.SecAgreeMode
		if template.UsePlainDigestPlaceholder {
			template.SecAgreeMode = "required"
		}
	}
	template.SMSReceiverTransport = NormalizeSMSReceiverTransport(template.SMSReceiverTransport)
	template.ContactParamOrder = normalizeContactParamOrder(template.ContactParamOrder)
	if len(template.ContactParamOrder) == 0 {
		template.ContactParamOrder = defaults.ContactParamOrder
	}
	if template.Expires < 1 {
		template.Expires = defaults.Expires
	}
	template.SecurityClientMechanisms = normalizeIPSec3GPPMechanismList(template.SecurityClientMechanisms)
	if len(template.SecurityClientMechanisms) == 0 {
		template.SecurityClientMechanisms = defaults.SecurityClientMechanisms
	}
	template.RegisterPolicy = NormalizeIMSRegisterPolicy(template.RegisterPolicy)
	if isZeroIMSRegisterPolicy(template.RegisterPolicy) {
		template.RegisterPolicy = defaults.RegisterPolicy
	}
	normalizeCompatibilityTemplate(&template)
	return template
}

func IMSRegisterTemplateSecAgreeMode(template IMSRegisterTemplate) string {
	mode := normalizeIMSRegisterSecAgreeMode(NormalizeIMSRegisterTemplate(template).SecAgreeMode)
	if mode == "" {
		return "auto"
	}
	return mode
}

func IMSRegisterTemplateInitialSecAgreeEnabled(template IMSRegisterTemplate) bool {
	return IMSRegisterTemplateSecAgreeMode(template) != "disabled"
}

func FallbackIMSRegisterTemplate(template IMSRegisterTemplate) IMSRegisterTemplate {
	return NormalizeIMSRegisterTemplate(template)
}

func NormalizeIMSIdentitySource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "isim", "derived":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "derived"
	}
}

func NormalizeIMSDomain(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "sips:") {
		value = value[len("sips:"):]
	} else if strings.HasPrefix(lower, "sip:") {
		value = value[len("sip:"):]
	}
	if index := strings.IndexAny(value, ";?/"); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func NormalizeIMSTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tcp", "udp", "auto":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func NormalizeSMSReceiverTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "none", "disable", "disabled":
		return "none"
	case "tcp":
		return "tcp"
	case "udp":
		return "udp"
	default:
		return "dual"
	}
}

func NormalizeSMSRoutingMethod(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tel", "tel_uri", "tel_uri_smsc":
		return "tel_uri_smsc"
	case "sm_gw", "ipsmgw", "ip_sm_gw":
		return "ip_sm_gw"
	case "no_user_phone", "sip_no_user_phone", "sip_uri_no_user_phone":
		return "sip_uri_no_user_phone"
	default:
		return "sip_uri_smsc"
	}
}

func NormalizeCarrierDNSServer(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultDNS
	}
	if _, _, err := net.SplitHostPort(value); err == nil {
		return value
	}
	host := strings.Trim(value, "[]")
	if parsed := net.ParseIP(host); parsed != nil {
		host = parsed.String()
	}
	return net.JoinHostPort(host, "53")
}

func normalizeIMSRegisterSecAgreeMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "true", "enable", "strict", "enabled", "require", "required":
		return "required"
	case "off", "none", "false", "disable", "disabled":
		return "disabled"
	case "auto":
		return "auto"
	default:
		return ""
	}
}

func normalizeSIPStatusCodeList(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value < 100 || value > 699 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeIPSec3GPPMechanismList(values []IPSec3GPPSecurityMechanism) []IPSec3GPPSecurityMechanism {
	result := make([]IPSec3GPPSecurityMechanism, 0, len(values))
	seen := make(map[IPSec3GPPSecurityMechanism]struct{}, len(values))
	for _, value := range values {
		value.Alg = strings.ToLower(strings.TrimSpace(value.Alg))
		value.EAlg = strings.ToLower(strings.TrimSpace(value.EAlg))
		value.Prot = strings.ToLower(strings.TrimSpace(value.Prot))
		value.Mode = strings.ToLower(strings.TrimSpace(value.Mode))
		if value.Alg == "" || value.EAlg == "" || value.Prot == "" || value.Mode == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeContactParamOrder(values []string) []string { return normalizeStringList(values) }

func normalizeStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeContactMode(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "legacy" || value == "android_default" {
		return value
	}
	return fallback
}

func normalizeCompatibilityTemplate(template *IMSRegisterTemplate) {
	template.Domain = NormalizeIMSDomain(template.Domain)
	template.EPDGAddr = strings.TrimSpace(template.EPDGAddr)
	template.Transport = NormalizeIMSTransport(template.Transport)
	template.SMSRoutingMethod = NormalizeSMSRoutingMethod(template.SMSRoutingMethod)
	template.IdentitySource = NormalizeIMSIdentitySource(template.IdentitySource)
	template.DNSServer = NormalizeCarrierDNSServer(template.DNSServer)
	if template.ExpiresSeconds < 1 {
		template.ExpiresSeconds = template.Expires
	}
	template.ContactOrder = normalizeStringList(template.ContactOrder)
	if len(template.ContactOrder) == 0 {
		template.ContactOrder = cloneStrings(template.ContactParamOrder)
	}
	if template.RegisterPolicyMode == "" {
		template.RegisterPolicyMode = template.RegisterPolicy.ID
	}
	template.SecAgreeEnabled = IMSRegisterTemplateSecAgreeModeWithoutNormalize(*template) != "disabled"
}

func IMSRegisterTemplateSecAgreeModeWithoutNormalize(template IMSRegisterTemplate) string {
	mode := normalizeIMSRegisterSecAgreeMode(template.SecAgreeMode)
	if mode == "" {
		return "auto"
	}
	return mode
}

func isZeroIMSRegisterPolicy(value IMSRegisterPolicy) bool {
	return value.ID == "" && len(value.TemporaryStatusCodes) == 0 && len(value.ForbiddenStatusCodes) == 0 &&
		len(value.InitialRejectFallbackStatusCodes) == 0 && value.TemporaryRetrySeconds == 0
}

func isZeroIMSRegisterTemplate(value IMSRegisterTemplate) bool {
	return reflect.DeepEqual(value, IMSRegisterTemplate{})
}

func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
