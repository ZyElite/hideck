package carrier

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultIMSExpiresSeconds  = 600000
	defaultIMSContactMode     = "android_default"
	defaultIMSAccessType      = "wlan1"
	giffgaffPresetID          = "giffgaff_23410"
	giffgaffDeviceModel       = "rmx3366"
	defaultIMSSupportedHeader = "path,sec-agree"
	defaultIMSAllowHeader     = "OPTIONS, REGISTER, SUBSCRIBE, NOTIFY, PUBLISH, INVITE, ACK, BYE, CANCEL, UPDATE, PRACK, REFER, INFO, MESSAGE"
	defaultIMSICSIRef         = "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.msg," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.sms"
	maxIMSExpiresSeconds = int64(1<<63-1) / int64(time.Second)
	attE911Websheet      = "https://www.att.com/acctmgmt/wireless/e911"
	attE911Endpoint      = "https://sentitlement2.mobile.att.net/WFC"
)

const (
	// IKEProposalAES256SHA512PRFSHA512MODP2048 is the recovered strong IKE proposal name.
	IKEProposalAES256SHA512PRFSHA512MODP2048 = "aes256-sha512-prfsha512-modp2048"
	// ESPProposalAES256SHA512 is the recovered strong ESP proposal name.
	ESPProposalAES256SHA512 = "aes256-sha512"
)

var defaultContactOrder = []string{
	"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
}

var giffgaffContactOrder = []string{
	"access_type", "sip_instance", "audio", "smsip", "icsi_ref",
	"mid_call", "srvcc_alerting", "ps2cs_srvcc_orig_pre_alerting",
}

func defaultCarrierConfig(input EffectiveCarrierConfigInput) EffectiveCarrierConfig {
	cfg := EffectiveCarrierConfig{
		MCC: strings.TrimSpace(input.MCC), MNC: strings.TrimSpace(input.MNC),
		IMS: defaultIMSRegisterTemplate(),
	}
	switch {
	case samePLMN(cfg.MCC, cfg.MNC, "310", "280"), samePLMN(cfg.MCC, cfg.MNC, "310", "410"):
		cfg.PresetID = "att"
		cfg.E911 = E911Config{
			Enabled:             true,
			Provider:            "att-ts43",
			Websheet:            attE911Websheet,
			EntitlementEndpoint: attE911Endpoint,
		}
	case samePLMN(cfg.MCC, cfg.MNC, "234", "10"):
		applyGiffgaffPreset(&cfg)
	}
	return cfg
}

func defaultIMSRegisterTemplate() IMSRegisterTemplate {
	return IMSRegisterTemplate{
		ExpiresSeconds: defaultIMSExpiresSeconds, Transport: "auto", SupportedHeader: defaultIMSSupportedHeader,
		AllowHeader: defaultIMSAllowHeader, ContactMode: defaultIMSContactMode,
		AccessType: defaultIMSAccessType, ICSIRef: defaultIMSICSIRef,
		ContactOrder: append([]string(nil), defaultContactOrder...),
	}
}

func applyGiffgaffPreset(cfg *EffectiveCarrierConfig) {
	cfg.PresetID = giffgaffPresetID
	cfg.DeviceModel = giffgaffDeviceModel
	cfg.IKEProposals = []string{IKEProposalAES256SHA512PRFSHA512MODP2048}
	cfg.ESPProposals = []string{ESPProposalAES256SHA512}
	cfg.IMS.ContactOrder = append([]string(nil), giffgaffContactOrder...)
	cfg.IMS.IncludePANIAuthenticated = true
	cfg.IMS.StrictSecurityServerOffer = true
}

func applyCarrierOverride(cfg *EffectiveCarrierConfig, override CarrierOverride) {
	if value := strings.TrimSpace(override.PresetID); value != "" {
		cfg.PresetID = value
	}
	if value := strings.TrimSpace(override.DeviceModel); value != "" {
		cfg.DeviceModel = value
	}
	if len(override.IKEProposals) > 0 {
		cfg.IKEProposals = cloneStrings(override.IKEProposals)
	}
	if len(override.ESPProposals) > 0 {
		cfg.ESPProposals = cloneStrings(override.ESPProposals)
	}
	if override.ReauthIntervalSeconds != 0 {
		cfg.ReauthIntervalSeconds = override.ReauthIntervalSeconds
	}
	if override.E911.Enabled {
		cfg.E911.Enabled = true
	}
	setStringIfPresent(&cfg.E911.Provider, override.E911.Provider)
	setStringIfPresent(&cfg.E911.Websheet, override.E911.Websheet)
	setStringIfPresent(&cfg.E911.EntitlementEndpoint, override.E911.EntitlementEndpoint)
	mergeIMSRegisterTemplate(&cfg.IMS, override.IMS)
}

func mergeIMSRegisterTemplate(target *IMSRegisterTemplate, override IMSRegisterTemplate) {
	if override.expiresSet || override.ExpiresSeconds != 0 {
		target.ExpiresSeconds = override.ExpiresSeconds
	}
	setStringIfPresent(&target.Transport, override.Transport)
	setStringIfPresent(&target.SupportedHeader, override.SupportedHeader)
	setStringIfPresent(&target.AllowHeader, override.AllowHeader)
	setStringIfPresent(&target.ContactMode, override.ContactMode)
	setStringIfPresent(&target.AccessType, override.AccessType)
	setStringIfPresent(&target.ICSIRef, override.ICSIRef)
	if len(override.ContactOrder) > 0 {
		target.ContactOrder = cloneStrings(override.ContactOrder)
	}
	if override.IncludePANIAuthenticated {
		target.IncludePANIAuthenticated = true
	}
	if override.StrictSecurityServerOffer {
		target.StrictSecurityServerOffer = true
	}
}

func setStringIfPresent(target *string, source string) {
	if value := strings.TrimSpace(source); value != "" {
		*target = value
	}
}

// ValidateEffectiveCarrierConfig rejects unsupported carrier wire settings.
func ValidateEffectiveCarrierConfig(cfg EffectiveCarrierConfig) error {
	if cfg.ReauthIntervalSeconds < 0 {
		return fmt.Errorf("carrier: reauth interval must not be negative")
	}
	if err := validateE911Config(cfg.E911); err != nil {
		return err
	}
	return validateIMSRegisterTemplate(cfg.IMS)
}

func validateE911Config(cfg E911Config) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return fmt.Errorf("carrier: enabled E911 has no provider")
	}
	for name, value := range map[string]string{
		"websheet": cfg.Websheet, "entitlement endpoint": cfg.EntitlementEndpoint,
	} {
		parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("carrier: E911 %s must be an HTTP URL", name)
		}
	}
	return nil
}

func validateIMSRegisterTemplate(template IMSRegisterTemplate) error {
	if template.ExpiresSeconds <= 0 {
		return fmt.Errorf("carrier: IMS registration expiry must be positive")
	}
	if int64(template.ExpiresSeconds) > maxIMSExpiresSeconds {
		return fmt.Errorf("carrier: IMS registration expiry %d seconds overflows duration", template.ExpiresSeconds)
	}
	switch strings.ToLower(strings.TrimSpace(template.Transport)) {
	case "auto", "tcp", "udp":
	default:
		return fmt.Errorf("carrier: unsupported IMS transport %q", template.Transport)
	}
	if strings.TrimSpace(template.ContactMode) != defaultIMSContactMode {
		return fmt.Errorf("carrier: unsupported IMS Contact mode %q", template.ContactMode)
	}
	known := map[string]bool{
		"access_type": true, "sip_instance": true, "audio": true, "smsip": true,
		"icsi_ref": true, "mid_call": true, "srvcc_alerting": true,
		"ps2cs_srvcc_orig_pre_alerting": true,
	}
	seen := make(map[string]bool, len(template.ContactOrder))
	for _, raw := range template.ContactOrder {
		name := strings.TrimSpace(raw)
		if !known[name] {
			return fmt.Errorf("carrier: unsupported IMS Contact parameter %q", raw)
		}
		if seen[name] {
			return fmt.Errorf("carrier: duplicate IMS Contact parameter %q", name)
		}
		seen[name] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("carrier: IMS Contact parameter order is empty")
	}
	return nil
}

func cloneEffectiveCarrierConfig(cfg EffectiveCarrierConfig) EffectiveCarrierConfig {
	cfg.IKEProposals = cloneStrings(cfg.IKEProposals)
	cfg.ESPProposals = cloneStrings(cfg.ESPProposals)
	cfg.IMS.ContactOrder = cloneStrings(cfg.IMS.ContactOrder)
	return cfg
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func samePLMN(leftMCC, leftMNC, rightMCC, rightMNC string) bool {
	return strings.TrimSpace(leftMCC) == strings.TrimSpace(rightMCC) &&
		canonicalMNC(leftMNC) == canonicalMNC(rightMNC)
}

func canonicalMNC(value string) string {
	value = strings.TrimSpace(value)
	for len(value) > 2 && strings.HasPrefix(value, "0") {
		value = strings.TrimPrefix(value, "0")
	}
	return value
}
