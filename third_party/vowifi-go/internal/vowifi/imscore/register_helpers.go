package imscore

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/policy"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

func formatAORForSIP(scheme, user, domain string) string {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	user = strings.TrimSpace(user)
	domain = strings.TrimSpace(domain)
	if scheme == "" {
		scheme = "sip"
	}
	if scheme == "tel" {
		return "tel:" + user
	}
	if domain != "" {
		return scheme + ":" + user + "@" + domain
	}
	return scheme + ":" + user
}

func registerExpiresForTemplate(template policy.IMSRegisterTemplate, fallback int) int {
	normalized := policy.NormalizeIMSRegisterTemplate(template)
	if normalized.Expires > 0 {
		return normalized.Expires
	}
	if fallback > 0 {
		return fallback
	}
	return policy.DefaultIMSRegisterTemplate().Expires
}

func registerPANIForTemplate(
	template policy.IMSRegisterTemplate,
	configured string,
	imsIdentity identity.IMSIdentity,
) string {
	normalized := policy.NormalizeIMSRegisterTemplate(template)
	if fixed := strings.TrimSpace(normalized.FixedPANI); fixed != "" {
		return fixed
	}
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return GenerateStablePAccessNetworkInfoByIdentity(imsIdentity)
}

func registerAllowHeader(template policy.IMSRegisterTemplate) string {
	return strings.TrimSpace(policy.NormalizeIMSRegisterTemplate(template).AllowHeader)
}

func registerAccessType(configured string, template policy.IMSRegisterTemplate) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return policy.NormalizeIMSRegisterTemplate(template).AccessType
}

func securityClientMechanismCount(value string) int {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	return len(strings.Split(value, ","))
}

func formatHostForSIP(host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func pickAuthRealm(configured, challenge, fallback string) string {
	return firstNonBlank(challenge, configured, fallback)
}

func registerHeaderPortForTemplate(
	actualPort,
	configuredHeaderPort int,
	template policy.IMSRegisterTemplate,
) int {
	if configuredHeaderPort < 1 && actualPort > 0 {
		return actualPort
	}
	if policy.NormalizeIMSRegisterTemplate(template).ForceHeaderPort5060 {
		return defaultSIPPort
	}
	if configuredHeaderPort > 0 {
		return configuredHeaderPort
	}
	if actualPort > 0 {
		return actualPort
	}
	return defaultSIPPort
}

func isAddressInUseError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "address already in use")
}

func pickAuthHeader(response *sipResponse) (string, string) {
	if response == nil {
		return "", ""
	}
	if value := strings.TrimSpace(response.Header("WWW-Authenticate")); value != "" {
		return "WWW-Authenticate", value
	}
	if value := strings.TrimSpace(response.Header("Proxy-Authenticate")); value != "" {
		return "Proxy-Authenticate", value
	}
	return "", ""
}

func summarizeSIPFailure(response *sipResponse) (string, string, string, string) {
	if response == nil {
		return "", "", "", ""
	}
	return strings.TrimSpace(response.Header("Warning")), strings.TrimSpace(string(response.Body)),
		strings.TrimSpace(response.Header("WWW-Authenticate")), strings.TrimSpace(response.Header("Proxy-Authenticate"))
}

func parseCSeq(value string) int {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0
	}
	parsed, _ := strconv.Atoi(fields[0])
	return parsed
}

func parseExpiresHeader(value string, fallback uint32) uint32 {
	seconds, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil || seconds == 0 {
		return fallback
	}
	return uint32(seconds)
}

func parseRegisterExpiresFromResponse(response *sipResponse, fallback uint32) uint32 {
	if response == nil {
		return fallback
	}
	if seconds := parseExpiresHeader(response.Header("Expires"), 0); seconds > 0 {
		return seconds
	}
	for _, header := range response.HeaderValues("Contact") {
		for _, contact := range splitSIPHeaderValues(header) {
			if seconds := parseContactExpiresParam(contact); seconds > 0 {
				return seconds
			}
		}
	}
	return fallback
}

func parseContactExpiresParam(contact string) uint32 {
	lower := strings.ToLower(contact)
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], "expires=")
		if index < 0 {
			return 0
		}
		start := offset + index + len("expires=")
		for start < len(lower) && (lower[start] == ' ' || lower[start] == '\t') {
			start++
		}
		end := start
		for end < len(lower) && lower[end] >= '0' && lower[end] <= '9' {
			end++
		}
		if end > start {
			if seconds := parseExpiresHeader(lower[start:end], 0); seconds > 0 {
				return seconds
			}
		}
		offset = start
	}
	return 0
}

func parseRemoteIPFromPath(path string) string {
	lower := strings.ToLower(path)
	index := strings.Index(lower, "sip:")
	if index < 0 {
		return ""
	}
	candidate := strings.TrimLeft(strings.TrimSpace(path[index+len("sip:"):]), "<")
	if boundary := strings.IndexAny(candidate, ";>?"); boundary >= 0 {
		candidate = candidate[:boundary]
	}
	if at := strings.LastIndex(candidate, "@"); at >= 0 {
		candidate = candidate[at+1:]
	}
	if host, _, err := net.SplitHostPort(candidate); err == nil {
		return strings.Trim(host, "[]")
	}
	if ip := net.ParseIP(strings.Trim(candidate, "[]")); ip != nil {
		return ip.String()
	}
	return strings.Trim(candidate, "[]")
}

func resolveHostIP(host string, preferIPv6 bool) (net.IP, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil, errors.New("host 为空")
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	if preferIPv6 {
		for _, ip := range ips {
			if ip != nil && ip.To4() == nil && ip.IsGlobalUnicast() {
				return ip, nil
			}
		}
		for _, ip := range ips {
			if ip != nil && ip.To4() == nil {
				return ip, nil
			}
		}
		return nil, errors.New("无法选择 IP")
	}
	for _, ip := range ips {
		if ip != nil && ip.To4() != nil {
			return ip, nil
		}
	}
	for _, ip := range ips {
		if ip != nil {
			return ip, nil
		}
	}
	return nil, errors.New("无法选择 IP")
}

func parseRegisterRetryHintsFromResponse(response *sipResponse) (time.Duration, uint32) {
	if response == nil {
		return 0, 0
	}
	retrySeconds, _ := strconv.Atoi(strings.TrimSpace(response.Header("Retry-After")))
	minExpires, _ := strconv.ParseUint(strings.TrimSpace(response.Header("Min-Expires")), 10, 32)
	if retrySeconds < 1 {
		retrySeconds = 0
	}
	return time.Duration(retrySeconds) * time.Second, uint32(minExpires)
}

func isTemporaryRegisterSIPResponse(registerPolicy policy.IMSRegisterPolicy, statusCode int) bool {
	return statusCodeIn(normalizedRegisterPolicy(registerPolicy).TemporaryStatusCodes, statusCode)
}

func isForbiddenRegisterSIPResponse(registerPolicy policy.IMSRegisterPolicy, statusCode int) bool {
	return statusCodeIn(normalizedRegisterPolicy(registerPolicy).ForbiddenStatusCodes, statusCode)
}

func temporaryRegisterRetryInterval(registerPolicy policy.IMSRegisterPolicy) time.Duration {
	seconds := normalizedRegisterPolicy(registerPolicy).TemporaryRetrySeconds
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second
}

func normalizedRegisterPolicy(registerPolicy policy.IMSRegisterPolicy) policy.IMSRegisterPolicy {
	normalized := policy.NormalizeIMSRegisterPolicy(registerPolicy)
	if normalized.ID == "" && len(normalized.TemporaryStatusCodes) == 0 &&
		len(normalized.ForbiddenStatusCodes) == 0 && normalized.TemporaryRetrySeconds == 0 {
		return policy.DefaultIMSRegisterPolicy()
	}
	return normalized
}

func effectiveRegisterPolicyID(template policy.IMSRegisterTemplate, source string) string {
	if id := strings.TrimSpace(policy.NormalizeIMSRegisterPolicy(template.RegisterPolicy).ID); id != "" {
		return id
	}
	if source = strings.TrimSpace(source); source != "" {
		return source
	}
	return "default"
}

func normalizeRegisterPolicySource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "preset":
		return "preset"
	case "override":
		return "override"
	default:
		return "default"
	}
}

func statusCodeIn(codes []int, statusCode int) bool {
	for _, code := range codes {
		if code == statusCode {
			return true
		}
	}
	return false
}
