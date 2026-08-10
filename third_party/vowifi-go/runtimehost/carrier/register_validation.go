package carrier

import (
	"fmt"
	"strings"
)

func validateRegisterTemplateFields(template IMSRegisterTemplate) error {
	if err := validateSMSReceiverTransport(template.SMSReceiverTransport, true); err != nil {
		return err
	}
	if err := validateSecAgreeMode(template.SecAgreeMode, true); err != nil {
		return err
	}
	if err := validateSecurityMechanisms(template.SecurityClientMechanisms, true); err != nil {
		return err
	}
	return validateRegisterPolicy(template.RegisterPolicy)
}

func validateRegisterTemplateOverrideFields(template IMSRegisterTemplate) error {
	if err := validateSMSReceiverTransport(template.SMSReceiverTransport, false); err != nil {
		return err
	}
	if err := validateSecAgreeMode(template.SecAgreeMode, false); err != nil {
		return err
	}
	if err := validateSecurityMechanisms(template.SecurityClientMechanisms, false); err != nil {
		return err
	}
	return validateRegisterPolicy(template.RegisterPolicy)
}

func validateSMSReceiverTransport(value string, required bool) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dual", "tcp", "udp", "none", "off", "disable", "disabled":
		return nil
	case "":
		if !required {
			return nil
		}
	}
	return fmt.Errorf("carrier: unsupported SMS receiver transport %q", value)
}

func validateSecAgreeMode(value string, required bool) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "on", "true", "enable", "enabled", "strict", "require", "required",
		"off", "none", "false", "disable", "disabled":
		return nil
	case "":
		if !required {
			return nil
		}
	}
	return fmt.Errorf("carrier: unsupported sec-agree mode %q", value)
}

func validateRegisterPolicy(policy IMSRegisterPolicy) error {
	if policy.TemporaryRetrySeconds < 0 {
		return fmt.Errorf("carrier: REGISTER temporary retry must not be negative")
	}
	lists := []struct {
		name   string
		values []int
	}{
		{"temporary status", policy.TemporaryStatusCodes},
		{"forbidden status", policy.ForbiddenStatusCodes},
		{"initial fallback status", policy.InitialRejectFallbackStatusCodes},
	}
	for _, list := range lists {
		if err := validateSIPStatusCodes(list.name, list.values); err != nil {
			return err
		}
	}
	return nil
}

func validateSIPStatusCodes(name string, values []int) error {
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value < 100 || value > 699 {
			return fmt.Errorf("carrier: invalid REGISTER %s code %d", name, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("carrier: duplicate REGISTER %s code %d", name, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateSecurityMechanisms(values []IPSec3GPPSecurityMechanism, required bool) error {
	if len(values) == 0 {
		if required {
			return fmt.Errorf("carrier: Security-Client mechanism list is empty")
		}
		return nil
	}
	seen := make(map[[4]string]struct{}, len(values))
	for index, value := range values {
		key, err := canonicalSecurityMechanism(value)
		if err != nil {
			return fmt.Errorf("carrier: Security-Client mechanism %d: %w", index, err)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("carrier: duplicate Security-Client mechanism %d", index)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func canonicalSecurityMechanism(value IPSec3GPPSecurityMechanism) ([4]string, error) {
	auth := canonicalToken(value.Alg, map[string]string{
		"hmac(md5)": "hmac-md5-96", "hmac-md5-96": "hmac-md5-96",
		"hmac(sha1)": "hmac-sha-1-96", "hmac-sha-1-96": "hmac-sha-1-96",
	})
	encryption := canonicalToken(value.EAlg, map[string]string{
		"aes-cbc": "aes-cbc", "cbc(aes)": "aes-cbc",
		"3des-cbc": "des-ede3-cbc", "des-ede3-cbc": "des-ede3-cbc", "cbc(des3_ede)": "des-ede3-cbc",
		"null": "null", "cipher_null": "null", "ecb(cipher_null)": "null",
	})
	protocol := canonicalToken(value.Prot, map[string]string{"esp": "esp"})
	mode := canonicalToken(value.Mode, map[string]string{"trans": "trans"})
	if auth == "" || encryption == "" || protocol == "" || mode == "" {
		return [4]string{}, fmt.Errorf("unsupported alg=%q ealg=%q prot=%q mode=%q", value.Alg, value.EAlg, value.Prot, value.Mode)
	}
	return [4]string{auth, encryption, protocol, mode}, nil
}

func canonicalToken(value string, accepted map[string]string) string {
	return accepted[strings.ToLower(strings.TrimSpace(value))]
}
