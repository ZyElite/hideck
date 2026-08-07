package carrier

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultIMSExpiresSeconds  = 600000
	defaultIMSContactMode     = "android_default"
	defaultIMSAccessType      = "wlan1"
	giffgaffPresetID          = "giffgaff_23410"
	giffgaffDeviceModel       = "rmx3366"
	giffgaffReauthSeconds     = 180
	defaultIMSSupportedHeader = "path,sec-agree"
	defaultIMSAllowHeader     = "OPTIONS, REGISTER, SUBSCRIBE, NOTIFY, PUBLISH, INVITE, ACK, BYE, CANCEL, UPDATE, PRACK, REFER, INFO, MESSAGE"
	defaultIMSICSIRef         = "urn%3Aurn-7%3A3gpp-service.ims.icsi.mmtel," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.msg," +
		"urn%3Aurn-7%3A3gpp-service.ims.icsi.oma.cpm.sms"
	maxIMSExpiresSeconds = int64(1<<63-1) / int64(time.Second)
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
	case samePLMN(cfg.MCC, cfg.MNC, "310", "280"):
		cfg.PresetID = "att"
		cfg.E911 = E911Config{Enabled: true, Provider: "att"}
	case samePLMN(cfg.MCC, cfg.MNC, "234", "10"):
		applyGiffgaffPreset(&cfg)
	}
	return cfg
}

func defaultIMSRegisterTemplate() IMSRegisterTemplate {
	return IMSRegisterTemplate{
		ExpiresSeconds: defaultIMSExpiresSeconds, SupportedHeader: defaultIMSSupportedHeader,
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
	cfg.ReauthIntervalSeconds = giffgaffReauthSeconds
	cfg.IMS.ContactOrder = append([]string(nil), giffgaffContactOrder...)
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
	if override.E911.Enabled || strings.TrimSpace(override.E911.Provider) != "" {
		cfg.E911 = override.E911
	}
	mergeIMSRegisterTemplate(&cfg.IMS, override.IMS)
}

func mergeIMSRegisterTemplate(target *IMSRegisterTemplate, override IMSRegisterTemplate) {
	if override.expiresSet || override.ExpiresSeconds != 0 {
		target.ExpiresSeconds = override.ExpiresSeconds
	}
	setStringIfPresent(&target.SupportedHeader, override.SupportedHeader)
	setStringIfPresent(&target.AllowHeader, override.AllowHeader)
	setStringIfPresent(&target.ContactMode, override.ContactMode)
	setStringIfPresent(&target.AccessType, override.AccessType)
	setStringIfPresent(&target.ICSIRef, override.ICSIRef)
	if len(override.ContactOrder) > 0 {
		target.ContactOrder = cloneStrings(override.ContactOrder)
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
	return validateIMSRegisterTemplate(cfg.IMS)
}

func validateIMSRegisterTemplate(template IMSRegisterTemplate) error {
	if template.ExpiresSeconds <= 0 {
		return fmt.Errorf("carrier: IMS registration expiry must be positive")
	}
	if int64(template.ExpiresSeconds) > maxIMSExpiresSeconds {
		return fmt.Errorf("carrier: IMS registration expiry %d seconds overflows duration", template.ExpiresSeconds)
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
