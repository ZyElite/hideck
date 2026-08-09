package imscore

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
)

const (
	compatibilityRegisterTemplateID = "compatibility"
	securityModePlain               = "plain"
	securityModeIPSec               = "ipsec3gpp"
	securitySelected                = "security_server_offer_selected"
	securityDisabled                = "configured_disabled"
	securityAutoFallback            = "security_server_missing_auto_disable"
	securityRequired                = "security_server_missing_required"
	securityMissingOffer            = "missing_usable_security_server_offer"
	securityOfferBonus              = 0.1
)

var errMissingUsableSecurityServer = errors.New(securityMissingOffer)

const missingSecurityClientForSecAgree = "missing security-client while sec-agree enabled"

type secAgreeDecision struct {
	useIPSec bool
	mode     string
	reason   string
	err      error
}

func resolveActiveIMSRegisterTemplate(cfg *IMSConfig) policy.IMSRegisterTemplate {
	if cfg == nil {
		return policy.DefaultIMSRegisterTemplate()
	}
	if !reflect.DeepEqual(cfg.IMSRegisterTemplate, policy.IMSRegisterTemplate{}) {
		return policy.NormalizeIMSRegisterTemplate(cfg.IMSRegisterTemplate)
	}
	template := policy.DefaultIMSRegisterTemplate()
	template.ID = compatibilityRegisterTemplateID
	template.SecAgreeMode = "required"
	template.EnableInitialRejectFallback = false
	template.SupportedHeader = "path, outbound"
	applyCompatibilityRegisterTemplate(&template, cfg.RegisterTemplate)
	return policy.NormalizeIMSRegisterTemplate(template)
}

func applyCompatibilityRegisterTemplate(template *policy.IMSRegisterTemplate, compatibility IMSRegisterTemplate) {
	if supported := strings.TrimSpace(compatibility.SupportedHeader); supported != "" {
		template.SupportedHeader = supported
	}
	if allow := strings.TrimSpace(compatibility.AllowHeader); allow != "" {
		template.AllowHeader = allow
	}
	if compatibility.StrictSecurityServerOffer {
		template.StrictSecurityServerOffer = true
	}
}

func effectiveSecAgreeMode(cfg *IMSConfig, template policy.IMSRegisterTemplate) string {
	if cfg == nil || !cfg.IPSec3GPPEnabled() {
		return "disabled"
	}
	return policy.IMSRegisterTemplateSecAgreeMode(template)
}

func normalizeMechanismToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func canonicalAuthAlg(value string) string {
	switch normalizeMechanismToken(value) {
	case "hmac(md5)", "hmac-md5-96":
		return "hmac-md5-96"
	case "hmac(sha1)", ipsec3gpp.AuthHMACSHA196:
		return ipsec3gpp.AuthHMACSHA196
	default:
		return ""
	}
}

func canonicalEncAlg(value string) string {
	switch normalizeMechanismToken(value) {
	case "aes-cbc", "cbc(aes)":
		return ipsec3gpp.EncryptionAES
	case "3des-cbc", "des-ede3-cbc", "cbc(des3_ede)":
		return ipsec3gpp.Encryption3DES
	case "null", "cipher_null", "ecb(cipher_null)":
		return ipsec3gpp.EncryptionNull
	default:
		return ""
	}
}

func canonicalProt(value string) string {
	value = normalizeMechanismToken(value)
	if value == "" {
		return ipsec3gpp.ProtocolESP
	}
	if value == ipsec3gpp.ProtocolESP {
		return value
	}
	return ""
}

func canonicalMode(value string) string {
	value = normalizeMechanismToken(value)
	if value == "" {
		return ipsec3gpp.ModeTransport
	}
	if value == ipsec3gpp.ModeTransport {
		return value
	}
	return ""
}

func supportedSecurityClientMechanisms(template policy.IMSRegisterTemplate) []securityMechanism {
	mechanisms := canonicalTemplateMechanisms(template.SecurityClientMechanisms)
	if len(mechanisms) != 0 {
		return mechanisms
	}
	return canonicalTemplateMechanisms(policy.DefaultIMSRegisterTemplate().SecurityClientMechanisms)
}

func canonicalTemplateMechanisms(values []policy.IPSec3GPPSecurityMechanism) []securityMechanism {
	result := make([]securityMechanism, 0, len(values))
	seen := make(map[[4]string]struct{}, len(values))
	for _, value := range values {
		mechanism, supported := normalizeSupportedSecurityMechanism(value)
		if !supported {
			continue
		}
		key := [4]string{mechanism.Auth, mechanism.Encryption, mechanism.Protocol, mechanism.Mode}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, mechanism)
	}
	return result
}

func normalizeSupportedSecurityMechanism(value policy.IPSec3GPPSecurityMechanism) (securityMechanism, bool) {
	mechanism := securityMechanism{
		Name: "ipsec-3gpp", Auth: canonicalAuthAlg(value.Alg), Encryption: canonicalEncAlg(value.EAlg),
		Protocol: canonicalProt(value.Prot), Mode: canonicalMode(value.Mode),
	}
	return mechanism, mechanismSupported(mechanism)
}

func securityClientHeaderValue(client securityMechanism, template policy.IMSRegisterTemplate) string {
	return buildTemplateSecurityClient(client, template)
}

func buildTemplateSecurityClient(client securityMechanism, template policy.IMSRegisterTemplate) string {
	offers := templateSecurityClientOffers(client, template)
	if template.SecurityClientIncludesServerParams {
		return securityClientHeaderValueWithServer(offers)
	}
	return securityClientHeaderValueWithoutServer(offers)
}

func templateSecurityClientOffers(
	client securityMechanism,
	template policy.IMSRegisterTemplate,
) []securityMechanism {
	offers := supportedSecurityClientMechanisms(template)
	for index, offer := range offers {
		offer.SPIC, offer.SPIS = client.SPIC, client.SPIS
		offer.PortC, offer.PortS = client.PortC, client.PortS
		offers[index] = offer
	}
	return offers
}

func securityClientHeaderValueWithServer(offers []securityMechanism) string {
	formatted := make([]string, 0, len(offers))
	for _, offer := range offers {
		formatted = append(formatted, formatSecurityClientOffer(offer, true))
	}
	return strings.Join(formatted, ",")
}

func securityClientHeaderValueWithoutServer(offers []securityMechanism) string {
	formatted := make([]string, 0, len(offers))
	for _, offer := range offers {
		formatted = append(formatted, formatSecurityClientOffer(offer, false))
	}
	return strings.Join(formatted, ", ")
}

func formatSecurityClientOffer(mechanism securityMechanism, includeServerParams bool) string {
	if includeServerParams {
		format := "ipsec-3gpp; alg=%s; ealg=%s; spi-c=%d; spi-s=%d; port-c=%d; port-s=%d"
		return fmt.Sprintf(format, mechanism.Auth, mechanism.Encryption,
			mechanism.SPIC, mechanism.SPIS, mechanism.PortC, mechanism.PortS)
	}
	value := fmt.Sprintf("ipsec-3gpp; alg=%s; ealg=%s; prot=%s", mechanism.Auth, mechanism.Encryption, mechanism.Protocol)
	if mechanism.Mode != ipsec3gpp.ModeTransport {
		value += "; mod=" + mechanism.Mode
	}
	return fmt.Sprintf("%s; spi-c=%d; port-c=%d", value, mechanism.SPIC, mechanism.PortC)
}

func selectSecurityServerForTemplate(header string, template policy.IMSRegisterTemplate) (*securityMechanism, string, error) {
	return selectSecurityServerOfferForTemplate(header, template)
}

func selectSecurityServerOfferForTemplate(header string, template policy.IMSRegisterTemplate) (*securityMechanism, string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, "", errors.New("imscore: AKA challenge missing Security-Server")
	}
	var selected *securityMechanism
	selectedScore := -1.0
	for _, value := range splitSecurityMechanisms(header) {
		parsed, err := parseSecurityMechanism(value)
		if err != nil {
			continue
		}
		mechanism, ok := resolveSecurityServerOfferForTemplate(parsed, template)
		if !ok {
			continue
		}
		score := mechanism.Priority
		if mechanism.Encryption != ipsec3gpp.EncryptionNull {
			score += securityOfferBonus
		}
		if score > selectedScore {
			candidate := mechanism
			selected = &candidate
			selectedScore = score
		}
	}
	if selected == nil {
		return nil, "", errors.New("imscore: Security-Server has no supported ipsec-3gpp offer")
	}
	return selected, header, nil
}

func resolveSecurityServerOfferForTemplate(
	mechanism securityMechanism,
	template policy.IMSRegisterTemplate,
) (securityMechanism, bool) {
	mechanism.Name = normalizeMechanismToken(mechanism.Name)
	mechanism.Auth = canonicalAuthAlg(mechanism.Auth)
	mechanism.Encryption = canonicalEncAlg(mechanism.Encryption)
	mechanism.Protocol = canonicalProt(mechanism.Protocol)
	mechanism.Mode = canonicalMode(mechanism.Mode)
	if !mechanismSupported(mechanism) {
		return securityMechanism{}, false
	}
	for _, supported := range supportedSecurityClientMechanisms(template) {
		if securityOfferEqual(mechanism, supported) {
			return mechanism, true
		}
	}
	if template.StrictSecurityServerOffer {
		return securityMechanism{}, false
	}
	return mechanism, true
}

func securityOfferEqual(left, right securityMechanism) bool {
	return left.Name == right.Name && left.Auth == right.Auth && left.Encryption == right.Encryption &&
		left.Protocol == right.Protocol && left.Mode == right.Mode
}

func ipsec3gppOfferEqualForSA(left, right securityMechanism) bool {
	return normalizeMechanismToken(left.Name) == normalizeMechanismToken(right.Name) &&
		normalizeMechanismToken(left.Auth) == normalizeMechanismToken(right.Auth) &&
		normalizeMechanismToken(left.Encryption) == normalizeMechanismToken(right.Encryption) &&
		canonicalProt(left.Protocol) == canonicalProt(right.Protocol) &&
		canonicalMode(left.Mode) == canonicalMode(right.Mode) &&
		left.SPIC == right.SPIC && left.SPIS == right.SPIS && left.PortC == right.PortC && left.PortS == right.PortS
}

func validateSecAgreeRegisterParams(enabled bool, securityClient string) error {
	if enabled && strings.TrimSpace(securityClient) == "" {
		return errors.New(missingSecurityClientForSecAgree)
	}
	return nil
}

func isMissingSecurityClientForSecAgree(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), missingSecurityClientForSecAgree)
}
