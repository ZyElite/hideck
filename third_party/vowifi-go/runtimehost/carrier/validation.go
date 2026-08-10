package carrier

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
)

const maxIMSExpiresSeconds = int64(1<<63-1) / int64(time.Second)

var knownContactParameters = map[string]struct{}{
	"access_type": {}, "sip_instance": {}, "audio": {}, "smsip": {},
	"icsi_ref": {}, "mid_call": {}, "srvcc_alerting": {},
	"ps2cs_srvcc_orig_pre_alerting": {},
}

// ValidateEffectiveCarrierConfig rejects values that cannot reach the wire.
func ValidateEffectiveCarrierConfig(config EffectiveCarrierConfig) error {
	if err := validatePLMN(config.MCC, config.MNC); err != nil {
		return err
	}
	if err := validateTunnelConfig(config); err != nil {
		return err
	}
	if err := validateProposalConfig(config); err != nil {
		return err
	}
	if err := validateE911Config(config.E911); err != nil {
		return err
	}
	return validateIMSConfig(config)
}

func validatePLMN(mcc, mnc string) error {
	if !isDecimalWidth(strings.TrimSpace(mcc), 3, 3) {
		return fmt.Errorf("carrier: invalid MCC %q", mcc)
	}
	if !isDecimalWidth(strings.TrimSpace(mnc), 2, 3) {
		return fmt.Errorf("carrier: invalid MNC %q", mnc)
	}
	return nil
}

func validateTunnelConfig(config EffectiveCarrierConfig) error {
	switch strings.ToLower(strings.TrimSpace(config.IPStackType)) {
	case "ipv4", "ipv6", "ipv4v6":
	default:
		return fmt.Errorf("carrier: unsupported IP stack %q", config.IPStackType)
	}
	if strings.TrimSpace(config.EPDGAddr) == "" || config.EPDGPort == 0 {
		return fmt.Errorf("carrier: ePDG address and port are required")
	}
	if config.NATKeepaliveSeconds <= 0 {
		return fmt.Errorf("carrier: NAT keepalive must be positive")
	}
	if config.DPDIntervalSeconds < 20 || config.DPDIntervalSeconds > 1800 {
		return fmt.Errorf("carrier: DPD interval must be between 20 and 1800 seconds")
	}
	if config.ReauthIntervalSeconds < 0 {
		return fmt.Errorf("carrier: reauth interval must not be negative")
	}
	return nil
}

func validateProposalConfig(config EffectiveCarrierConfig) error {
	if len(config.IKEProposals) == 0 || len(config.ESPProposals) == 0 {
		return fmt.Errorf("carrier: IKE and ESP proposal lists must not be empty")
	}
	if err := rejectDuplicateStrings("IKE proposal", config.IKEProposals); err != nil {
		return err
	}
	if err := rejectDuplicateStrings("ESP proposal", config.ESPProposals); err != nil {
		return err
	}
	return swu.ValidateProposalConfig(&swu.Config{
		AlgorithmPolicy:      config.AlgorithmPolicy,
		IKEProposals:         cloneStrings(config.IKEProposals),
		ESPProposals:         cloneStrings(config.ESPProposals),
		EnableLegacyCiphers:  config.EnableLegacyCiphers,
		AllowedLegacyCiphers: cloneStrings(config.AllowedLegacyCiphers),
	})
}

func validateE911Config(config E911Config) error {
	if !config.Enabled {
		return nil
	}
	if strings.TrimSpace(config.Provider) == "" {
		return fmt.Errorf("carrier: enabled E911 has no provider")
	}
	endpoints := []struct{ name, value string }{
		{"entitlement URL", config.EntitlementURL},
		{"websheet", config.Websheet},
		{"entitlement endpoint", config.EntitlementEndpoint},
	}
	found := false
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.value) == "" {
			continue
		}
		found = true
		if !isHTTPURL(endpoint.value) {
			return fmt.Errorf("carrier: E911 %s must be an HTTP URL", endpoint.name)
		}
	}
	if !found {
		return fmt.Errorf("carrier: enabled E911 has no entitlement endpoint")
	}
	return nil
}

func validateIMSConfig(config EffectiveCarrierConfig) error {
	if err := validateIMSTransport(config.IMSTransport); err != nil {
		return err
	}
	if config.IMS.Transport != "" {
		if err := validateIMSTransport(config.IMS.Transport); err != nil {
			return err
		}
	}
	if config.IMSLocalPort < 1 || config.IMSLocalPort > 65535 {
		return fmt.Errorf("carrier: IMS local port is out of range")
	}
	if config.IMSTCPKeepaliveSeconds <= 0 || config.IMSOptionsPingIntervalSeconds <= 0 {
		return fmt.Errorf("carrier: IMS keepalive intervals must be positive")
	}
	if err := validateIMSRegisterTemplate(config.IMSRegisterTemplate); err != nil {
		return err
	}
	return validateIMSRegisterTemplate(config.IMS)
}

func validateIMSTransport(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "tcp", "udp":
		return nil
	default:
		return fmt.Errorf("carrier: unsupported IMS transport %q", value)
	}
}

func validateIMSRegisterTemplate(template IMSRegisterTemplate) error {
	if err := validateRegisterTemplateFields(template); err != nil {
		return err
	}
	expires := template.Expires
	if template.expiresSet || expires == 0 {
		expires = template.ExpiresSeconds
	}
	if expires <= 0 {
		return fmt.Errorf("carrier: IMS registration expiry must be positive")
	}
	if int64(expires) > maxIMSExpiresSeconds {
		return fmt.Errorf("carrier: IMS registration expiry %d seconds overflows duration", expires)
	}
	if mode := strings.TrimSpace(template.ContactMode); mode != "legacy" && mode != "android_default" {
		return fmt.Errorf("carrier: unsupported IMS Contact mode %q", template.ContactMode)
	}
	if len(template.ContactParamOrder) > 0 {
		if err := validateContactOrder(template.ContactParamOrder); err != nil {
			return err
		}
	}
	if len(template.ContactOrder) > 0 {
		return validateContactOrder(template.ContactOrder)
	}
	return fmt.Errorf("carrier: IMS Contact parameter order is empty")
}

func validateContactOrder(values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("carrier: IMS Contact parameter order is empty")
	}
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if _, ok := knownContactParameters[value]; !ok {
			return fmt.Errorf("carrier: unsupported IMS Contact parameter %q", raw)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("carrier: duplicate IMS Contact parameter %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func rejectDuplicateStrings(name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" {
			return fmt.Errorf("carrier: empty %s", name)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("carrier: duplicate %s %q", name, raw)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func isDecimalWidth(value string, minWidth, maxWidth int) bool {
	if len(value) < minWidth || len(value) > maxWidth {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
