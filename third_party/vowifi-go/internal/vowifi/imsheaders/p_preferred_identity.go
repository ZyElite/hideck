package imsheaders

import "strings"

// PreferredIdentityHeaderValue formats an IMS identity for
// P-Preferred-Identity.
func PreferredIdentityHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
		return value
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "sip:") || strings.HasPrefix(lower, "tel:") {
		return "<" + value + ">"
	}
	if strings.Contains(value, "@") {
		return "<sip:" + value + ">"
	}
	if strings.HasPrefix(value, "+") {
		return "<tel:" + value + ">"
	}
	return "<sip:" + value + ">"
}
