package policy

import (
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/common"
)

func GetGlobalDefaultConfig(mcc, mnc string) EffectiveCarrierConfig {
	mcc, mnc = common.Plmn3(mcc), common.Plmn3(mnc)
	template := DefaultIMSRegisterTemplate()
	config := EffectiveCarrierConfig{
		MCC: mcc, MNC: mnc, IPStackType: "ipv4v6",
		EPDGAddr: DefaultCarrierStandardEPDGAddr(mcc, mnc), EPDGAddrSource: "standard",
		EPDGPort: 500, APN: "ims", DNSServer: defaultDNS,
		NATKeepaliveSeconds: 20, DPDIntervalSeconds: 120,
		AKAChallengeMode: "minimal", IKEIdentityMode: "epc_nai", AKAIdentityMode: "epc_nai",
		IKEProposals: DefaultCarrierIKEProposals(), ESPProposals: DefaultCarrierESPProposals(),
		DeviceModel: "iphone15,4", IMSDomain: DefaultCarrierIMSDomain(mcc, mnc),
		IMSRealm: DefaultCarrierIMSDomain(mcc, mnc), IMSTransport: "auto", IMSIdentitySource: "derived",
		IMSLocalPort: 5060, IMSTCPKeepaliveSeconds: 30, IMSOptionsPingIntervalSeconds: 45,
		IMSRegisterTemplate: template, IMSRegisterPolicySource: "default",
	}
	syncCompatibilityProjection(&config)
	return config
}

func (config *EffectiveCarrierConfig) MergeFromPreset(preset CarrierPreset) {
	if config == nil {
		return
	}
	config.PresetID = strings.TrimSpace(preset.ID)
	config.MatchedTemplate = config.PresetID
	if hasExplicitE911Policy(preset.E911) {
		config.E911 = NormalizeE911Policy(preset.E911)
	}
	mergePresetEPDG(config, preset)
	mergePresetIKE(config, preset)
	mergePresetIMS(config, preset)
	if value := strings.TrimSpace(preset.SMSRoutingMethod); value != "" {
		config.SMSRoutingMethod = NormalizeSMSRoutingMethod(value)
	}
	setStringIfPresent(&config.SMSRoutingGW, preset.SMSRoutingGW)
	if preset.ForceSMSCAuth != nil {
		config.ForceSMSCAuth = *preset.ForceSMSCAuth
	}
	syncCompatibilityProjection(config)
}

func mergePresetEPDG(config *EffectiveCarrierConfig, preset CarrierPreset) {
	if value := strings.TrimSpace(preset.IPStackType); value != "" {
		config.IPStackType = strings.ToLower(value)
	}
	if value := strings.TrimSpace(preset.CustomEPDG); value != "" {
		config.EPDGAddr, config.EPDGAddrSource = value, "preset"
	}
	if preset.EPDGPort != nil && *preset.EPDGPort > 0 && *preset.EPDGPort <= 65535 {
		config.EPDGPort = uint16(*preset.EPDGPort)
	}
	setStringIfPresent(&config.APN, preset.APN)
	if value := strings.TrimSpace(preset.DNSServer); value != "" {
		config.DNSServer = NormalizeCarrierDNSServer(value)
	}
}

func mergePresetIKE(config *EffectiveCarrierConfig, preset CarrierPreset) {
	setStringIfPresent(&config.AlgorithmPolicy, preset.AlgorithmPolicy)
	setStringIfPresent(&config.DeviceModel, preset.DeviceModel)
	setStringIfPresent(&config.AKAChallengeMode, preset.AKAChallengeMode)
	setStringIfPresent(&config.IKEIdentityMode, preset.IKEIdentityMode)
	setStringIfPresent(&config.AKAIdentityMode, preset.AKAIdentityMode)
	setStringIfPresent(&config.DeviceIdentityIMEI, preset.DeviceIdentityIMEI)
	applyIntPtr(&config.NATKeepaliveSeconds, preset.NATKeepaliveSeconds)
	if preset.DPDIntervalSeconds != nil && *preset.DPDIntervalSeconds >= 20 && *preset.DPDIntervalSeconds <= 1800 {
		config.DPDIntervalSeconds = *preset.DPDIntervalSeconds
	}
	applyBoolPtr(&config.DeviceIdentityEnabled, preset.DeviceIdentityEnabled)
	applyBoolPtr(&config.EnableLegacyCiphers, preset.EnableLegacyCiphers)
	if len(preset.AllowedLegacyCiphers) > 0 {
		config.AllowedLegacyCiphers = normalizeStringList(preset.AllowedLegacyCiphers)
	}
	if len(preset.IKEProposals) > 0 {
		config.IKEProposals = normalizeStringList(preset.IKEProposals)
	}
	if len(preset.ESPProposals) > 0 {
		config.ESPProposals = normalizeStringList(preset.ESPProposals)
	}
	if preset.DPDKeepaliveIntervalSeconds > 0 {
		config.DPDKeepaliveIntervalSeconds = preset.DPDKeepaliveIntervalSeconds
	}
	if preset.ReauthIntervalSeconds > 0 {
		config.ReauthIntervalSeconds = preset.ReauthIntervalSeconds
	}
}

func mergePresetIMS(config *EffectiveCarrierConfig, preset CarrierPreset) {
	if value := NormalizeIMSDomain(preset.IMSDomain); value != "" {
		config.IMSDomain = value
	}
	if value := NormalizeIMSDomain(preset.IMSRealm); value != "" {
		config.IMSRealm = value
	}
	setStringIfPresent(&config.IMSRegistrar, preset.IMSRegistrar)
	setStringIfPresent(&config.IMSPCSCF, preset.IMSPCSCF)
	setStringIfPresent(&config.IMSUserAgent, preset.IMSUserAgent)
	if value := strings.TrimSpace(preset.IMSTransport); value != "" {
		config.IMSTransport = NormalizeIMSTransport(value)
	}
	if value := strings.TrimSpace(preset.IMSIdentitySource); value != "" {
		config.IMSIdentitySource = NormalizeIMSIdentitySource(value)
	}
	if preset.IMSLocalPort != nil && *preset.IMSLocalPort > 0 && *preset.IMSLocalPort <= 65535 {
		config.IMSLocalPort = *preset.IMSLocalPort
	}
	if preset.IMSTCPKeepaliveSeconds != nil && *preset.IMSTCPKeepaliveSeconds > 0 {
		config.IMSTCPKeepaliveSeconds = *preset.IMSTCPKeepaliveSeconds
	}
	if preset.IMSOptionsPingIntervalSeconds != nil && *preset.IMSOptionsPingIntervalSeconds > 0 {
		config.IMSOptionsPingIntervalSeconds = *preset.IMSOptionsPingIntervalSeconds
	}
	if !isZeroIMSRegisterTemplate(preset.IMSRegisterTemplate) {
		config.IMSRegisterTemplate = NormalizeIMSRegisterTemplate(preset.IMSRegisterTemplate)
		if !registerPolicyEquals(config.IMSRegisterTemplate.RegisterPolicy, DefaultIMSRegisterPolicy()) {
			config.IMSRegisterPolicySource = "preset"
		}
	}
}

func applyIntPtr(target *int, value *int) {
	if value != nil {
		*target = *value
	}
}
func applyBoolPtr(target *bool, value *bool) {
	if value != nil {
		*target = *value
	}
}

func syncCompatibilityProjection(config *EffectiveCarrierConfig) {
	template := cloneIMSRegisterTemplate(config.IMSRegisterTemplate)
	template.Domain = config.IMSDomain
	template.EPDGAddr = config.EPDGAddr
	template.Transport = config.IMSTransport
	template.SMSRoutingMethod = config.SMSRoutingMethod
	template.IdentitySource = config.IMSIdentitySource
	template.DNSServer = config.DNSServer
	template.ExpiresSeconds = template.Expires
	template.ContactOrder = cloneStrings(template.ContactParamOrder)
	template.RegisterPolicyMode = template.RegisterPolicy.ID
	template.SecAgreeEnabled = IMSRegisterTemplateSecAgreeModeWithoutNormalize(template) != "disabled"
	config.IMS = template
}
