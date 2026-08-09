package sipkit

import (
	"strings"

	"github.com/emiago/sipgo/sip"
)

func allowHeaderPolicy(method sip.RequestMethod, kind RequestKind, allow string) (bool, string) {
	if method == sip.INVITE {
		if kind == RequestKindInDialog {
			return false, ""
		}
		return true, strings.TrimSpace(allow)
	}
	if method == sip.REGISTER {
		return true, strings.TrimSpace(allow)
	}
	return false, ""
}

func securityModeIsIPSec3GPP(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), "ipsec3gpp")
}

// ResolveSIPHeaderPolicy resolves Allow/sec-agree and initial INVITE identity
// policy in the same order as the legacy builder.
func ResolveSIPHeaderPolicy(
	method sip.RequestMethod,
	kind RequestKind,
	effectiveSecurityMode string,
	allow string,
) (includeAllow bool, allowValue string, includePANI, includePPI bool) {
	includeAllow, allowValue = allowHeaderPolicy(method, kind, allow)
	includePANI = method == sip.INVITE && kind == RequestKindOutOfDialog
	includePPI = includePANI
	if !includeAllow {
		return includeAllow, allowValue, includePANI, includePPI
	}
	if securityModeIsIPSec3GPP(effectiveSecurityMode) {
		allowValue = ensureCSVToken(allowValue, "sec-agree")
	} else {
		allowValue = strings.TrimSpace(removeCSVTokenFold(allowValue, "sec-agree"))
	}
	return includeAllow, allowValue, includePANI, includePPI
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func containsTokenFold(values []string, token string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), token) {
			return true
		}
	}
	return false
}

func removeCSVTokenFold(value, token string) string {
	values := splitCSV(value)
	kept := values[:0]
	for _, candidate := range values {
		if !strings.EqualFold(strings.TrimSpace(candidate), token) {
			kept = append(kept, strings.TrimSpace(candidate))
		}
	}
	return strings.Join(kept, ", ")
}

func ensureCSVToken(value, token string) string {
	values := splitCSV(value)
	if !containsTokenFold(values, token) {
		values = append(values, token)
	}
	return strings.Join(values, ", ")
}

func requiresPANI(method sip.RequestMethod, value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	switch method {
	case sip.ACK, sip.CANCEL, sip.PRACK, sip.REFER:
		return false
	default:
		return true
	}
}

func requiresPPI(method sip.RequestMethod, value string) bool {
	if method == sip.REGISTER {
		return false
	}
	return requiresPANI(method, value)
}

func requiresSecurityClient(method sip.RequestMethod, value string) bool {
	return method == sip.REGISTER && strings.TrimSpace(value) != ""
}

func requiresSecurityVerify(method sip.RequestMethod, value string) bool {
	return method != sip.CANCEL && strings.TrimSpace(value) != ""
}

func ApplyAutoHeaders(
	request *sip.Request,
	method sip.RequestMethod,
	pani, preferredIdentity, securityVerify string,
) {
	if request == nil {
		return
	}
	appendHeaderWhen(request, "P-Access-Network-Info", pani, requiresPANI(method, pani))
	appendHeaderWhen(request, "P-Preferred-Identity", preferredIdentity, requiresPPI(method, preferredIdentity))
	appendHeaderWhen(request, "Security-Verify", securityVerify, requiresSecurityVerify(method, securityVerify))
}

func appendHeaderWhen(request *sip.Request, name, value string, include bool) {
	value = strings.TrimSpace(value)
	if requiresHeader(include, value) {
		request.AppendHeader(sip.NewHeader(name, value))
	}
}
