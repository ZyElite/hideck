package imsheaders

import (
	"regexp"
	"strings"
)

var (
	associatedTelPattern = regexp.MustCompile(`<tel:([^>]+)>`)
	associatedSIPPattern = regexp.MustCompile(`<sip:([^@>]+)@([^>]+)>`)
)

// PickAssociatedMSISDN selects the preferred identity from a complete
// P-Associated-URI header value.
func PickAssociatedMSISDN(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	sipPreferred, fallback := associatedSIPCandidates(header)
	telPreferred, fallback := associatedTelCandidates(header, fallback)
	if sipPreferred != "" {
		return sipPreferred
	}
	if telPreferred != "" {
		return telPreferred
	}
	if fallback == "" || strings.HasPrefix(fallback, "+") {
		return fallback
	}
	return "+" + fallback
}

func associatedSIPCandidates(header string) (preferred, fallback string) {
	for _, match := range associatedSIPPattern.FindAllStringSubmatch(header, -1) {
		if len(match) < 3 {
			continue
		}
		user := strings.TrimSpace(match[1])
		host := strings.TrimSpace(match[2])
		if separator := strings.IndexByte(host, ';'); separator >= 0 {
			host = host[:separator]
		}
		if user == "" || host == "" {
			continue
		}
		if strings.HasPrefix(user, "+") && preferred == "" {
			preferred = user + "@" + host
		} else if fallback == "" {
			fallback = user
		}
	}
	return preferred, fallback
}

func associatedTelCandidates(header, fallback string) (preferred, selectedFallback string) {
	selectedFallback = fallback
	for _, match := range associatedTelPattern.FindAllStringSubmatch(header, -1) {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "+") && preferred == "" {
			preferred = value
		} else if selectedFallback == "" {
			selectedFallback = value
		}
	}
	return preferred, selectedFallback
}

// ExtractPhoneFromAssociatedMSISDN extracts an international number from a
// tel, sip, or sips associated identity.
func ExtractPhoneFromAssociatedMSISDN(value string) string {
	value = associatedURIValue(strings.TrimSpace(value))
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "tel:"):
		value = value[len("tel:"):]
	case strings.HasPrefix(lower, "sips:"):
		value = value[len("sips:"):]
		value = strings.SplitN(value, "@", 2)[0]
	case strings.HasPrefix(lower, "sip:"):
		value = value[len("sip:"):]
		value = strings.SplitN(value, "@", 2)[0]
	default:
		return ""
	}
	if separator := strings.IndexAny(value, ";?"); separator >= 0 {
		value = value[:separator]
	}
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "+") {
		return value
	}
	return ""
}

func associatedURIValue(value string) string {
	start := strings.IndexByte(value, '<')
	if start < 0 {
		return value
	}
	end := strings.IndexByte(value[start+1:], '>')
	if end < 0 {
		return value
	}
	return value[start+1 : start+1+end]
}
