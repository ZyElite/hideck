package policy

import "strings"

func NormalizeE911Policy(policy E911Policy) E911Policy {
	policy.Provider = strings.TrimSpace(policy.Provider)
	policy.EntitlementURL = strings.TrimSpace(policy.EntitlementURL)
	policy.WebsheetHostPolicy = strings.TrimSpace(policy.WebsheetHostPolicy)
	policy.Websheet = strings.TrimSpace(policy.Websheet)
	policy.EntitlementEndpoint = strings.TrimSpace(policy.EntitlementEndpoint)
	return policy
}

func normalizeE911PolicyOverride(policy E911PolicyOverride) E911PolicyOverride {
	policy.Provider = strings.TrimSpace(policy.Provider)
	policy.EntitlementURL = strings.TrimSpace(policy.EntitlementURL)
	policy.WebsheetHostPolicy = strings.TrimSpace(policy.WebsheetHostPolicy)
	policy.Websheet = strings.TrimSpace(policy.Websheet)
	policy.EntitlementEndpoint = strings.TrimSpace(policy.EntitlementEndpoint)
	if policy.Enabled != nil && *policy.Enabled && policy.WebsheetHostPolicy == "" {
		policy.WebsheetHostPolicy = "public_https"
	}
	return policy
}

func hasExplicitE911Policy(policy E911Policy) bool {
	return policy.Enabled || strings.TrimSpace(policy.Provider) != "" ||
		strings.TrimSpace(policy.EntitlementURL) != "" || strings.TrimSpace(policy.WebsheetHostPolicy) != "" ||
		strings.TrimSpace(policy.Websheet) != "" || strings.TrimSpace(policy.EntitlementEndpoint) != ""
}

func applyE911PolicyOverride(target E911Policy, override E911PolicyOverride) E911Policy {
	target = NormalizeE911Policy(target)
	override = normalizeE911PolicyOverride(override)
	if override.Enabled != nil {
		target.Enabled = *override.Enabled
	}
	setStringIfPresent(&target.Provider, override.Provider)
	setStringIfPresent(&target.EntitlementURL, override.EntitlementURL)
	setStringIfPresent(&target.WebsheetHostPolicy, override.WebsheetHostPolicy)
	setStringIfPresent(&target.Websheet, override.Websheet)
	setStringIfPresent(&target.EntitlementEndpoint, override.EntitlementEndpoint)
	return target
}

func setStringIfPresent(target *string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		*target = value
	}
}
