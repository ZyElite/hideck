package logging

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	smsLogContentEnvironment = "VOHIVE_SMS_LOG_CONTENT"
	redactedHeaderSuffix     = ": [REDACTED]\r"
	redactedAuthorization    = "Authorization: [REDACTED]\r"
)

var longDigitPattern = regexp.MustCompile(`\d{8,}`)

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// RedactSIPRaw removes credentials and masks long digit runs in a SIP packet.
func RedactSIPRaw(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	for index, line := range lines {
		normalized := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(normalized, "authorization:") &&
			!strings.HasPrefix(normalized, "proxy-authorization:") {
			lines[index] = longDigitPattern.ReplaceAllStringFunc(line, maskLongDigits)
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			lines[index] = parts[0] + redactedHeaderSuffix
			continue
		}
		lines[index] = redactedAuthorization
	}
	return strings.Join(lines, "\n")
}

// RedactSMSContent hides content unless the explicit diagnostic switch is on.
func RedactSMSContent(content string) string {
	if envEnabled(smsLogContentEnvironment) {
		return content
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "[REDACTED len=0]"
	}
	return fmt.Sprintf("[REDACTED len=%d]", utf8.RuneCountInString(trimmed))
}

func maskLongDigits(digits string) string {
	if len(digits) < 7 {
		return strings.Repeat("*", len(digits))
	}
	return digits[:3] + strings.Repeat("*", len(digits)-5) + digits[len(digits)-2:]
}
