// Package imsheaders builds the IMS-specific SIP headers (Contact, Route,
// P-Preferred-Identity, sec-agree) used by the registration and dialog flows.
//
// Reconstructed from the decompiled internal/vowifi/imsheaders.
package imsheaders

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// icsiRefValue returns the ICSI (IMS Communication Service Identifier) ref
// value for a service, e.g. "urn:urn-7:3gpp-service.ims.icsi.mmtel".
func icsiRefValue(icsi string) string {
	icsi = strings.TrimSpace(icsi)
	if icsi == "" {
		return ""
	}
	if strings.HasPrefix(icsi, "urn:") {
		return icsi
	}
	return "urn:urn-7:3gpp-service.ims.icsi." + icsi
}

// formatHostForSIP formats a host for a SIP URI: IPv6 addresses are wrapped in
// brackets.
func formatHostForSIP(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() == nil {
		return "[" + host + "]"
	}
	return host
}

// sipInstanceIMEIDigits extracts the IMEI digits for the +sip.instance param.
func sipInstanceIMEIDigits(imei string) string {
	var digits []byte
	for _, c := range imei {
		if c >= '0' && c <= '9' {
			digits = append(digits, byte(c))
			continue
		}
		if c != '-' {
			return ""
		}
	}
	return string(digits)
}

// NormalizeSipInstance normalizes an IMEI into the GSMA URN used by 3GPP IMS.
// Other URNs are only enclosed in angle brackets; their namespace is retained.
func NormalizeSipInstance(instance string) string {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return ""
	}
	if strings.HasPrefix(instance, "<") && strings.HasSuffix(instance, ">") {
		return instance
	}
	if strings.HasPrefix(instance, "urn:gsma:imei:") {
		return "<" + instance + ">"
	}
	digits := sipInstanceIMEIDigits(instance)
	if len(digits) != 14 && len(digits) != 15 {
		return "<" + instance + ">"
	}
	if len(digits) == 14 {
		digits += string(rune('0' + imeiCheckDigit(digits)))
	}
	return fmt.Sprintf("<urn:gsma:imei:%s-%s-%s>", digits[:8], digits[8:14], digits[14:])
}

func imeiCheckDigit(digits string) rune {
	sum := 0
	double := true
	for i := len(digits) - 1; i >= 0; i-- {
		value := int(digits[i] - '0')
		if double {
			value *= 2
			if value > 9 {
				value -= 9
			}
		}
		sum += value
		double = !double
	}
	return rune((10 - sum%10) % 10)
}

// ContactParams builds the Contact header parameters for a registration.
func ContactParams(instance string, expires int, regID int) string {
	params := []string{}
	if instance != "" {
		params = append(params, `+sip.instance="`+NormalizeSipInstance(instance)+`"`)
	}
	if expires >= 0 {
		params = append(params, fmt.Sprintf("expires=%d", expires))
	}
	if regID > 0 {
		params = append(params, fmt.Sprintf("reg-id=%d", regID))
	}
	return strings.Join(params, ";")
}

// ContactURIWithOptions builds a Contact URI with the given options.
func ContactURIWithOptions(uri string, instance string, expires int, regID int) string {
	base := "<" + uri + ">"
	params := ContactParams(instance, expires, regID)
	if params != "" {
		return base + ";" + params
	}
	return base
}

// IMSContactOptions describes the IMS feature parameters advertised by a
// registration Contact binding.
type IMSContactOptions struct {
	Transport  string
	AccessType string
	Instance   string
	ICSIRef    string
	ParamOrder []string
}

// IMSContactURI builds a Contact value using the carrier-defined parameter
// order. The transport parameter belongs to the SIP URI; feature parameters
// belong to the Contact header.
func IMSContactURI(uri string, options IMSContactOptions) string {
	uri = strings.TrimSpace(uri)
	if transport := strings.ToLower(strings.TrimSpace(options.Transport)); transport != "" {
		uri += ";transport=" + transport
	}
	params := orderedIMSContactParams(options)
	if len(params) == 0 {
		return "<" + uri + ">"
	}
	return "<" + uri + ">;" + strings.Join(params, ";")
}

func orderedIMSContactParams(options IMSContactOptions) []string {
	params := make([]string, 0, len(options.ParamOrder))
	for _, name := range options.ParamOrder {
		if param := imsContactParam(strings.ToLower(strings.TrimSpace(name)), options); param != "" {
			params = append(params, param)
		}
	}
	return params
}

func imsContactParam(name string, options IMSContactOptions) string {
	switch name {
	case "access_type":
		return quotedContactParam("+g.3gpp.accesstype", options.AccessType)
	case "sip_instance":
		return quotedContactParam("+sip.instance", NormalizeSipInstance(options.Instance))
	case "audio":
		return "audio"
	case "smsip":
		return "+g.3gpp.smsip"
	case "icsi_ref":
		return quotedContactParam("+g.3gpp.icsi-ref", options.ICSIRef)
	case "mid_call":
		return "+g.3gpp.mid-call"
	case "srvcc_alerting":
		return "+g.3gpp.srvcc-alerting"
	case "ps2cs_srvcc_orig_pre_alerting":
		return "+g.3gpp.ps2cs-srvcc-orig-pre-alerting"
	default:
		return ""
	}
}

func quotedContactParam(name, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return name + "=" + value
	}
	return name + `="` + value + `"`
}

// PickAssociatedMSISDN picks the preferred MSISDN from a list of associated
// URIs (RFC 3455 P-Associated-URI).
func PickAssociatedMSISDN(uris []string) string {
	for _, u := range uris {
		if phone := ExtractPhoneFromAssociatedMSISDN(u); phone != "" {
			return phone
		}
	}
	return ""
}

// phoneRe matches a tel: or sip: URI carrying a phone number.
var phoneRe = regexp.MustCompile(`(?i)^(?:tel|sip):\+?([0-9]+)`)

// ExtractPhoneFromAssociatedMSISDN extracts the phone number from an
// associated URI.
func ExtractPhoneFromAssociatedMSISDN(uri string) string {
	m := phoneRe.FindStringSubmatch(strings.TrimSpace(uri))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// PreferredIdentityHeaderValue builds the P-Preferred-Identity header value
// (RFC 3325) from a phone number and domain.
func PreferredIdentityHeaderValue(phone, domain string) string {
	if phone == "" {
		return ""
	}
	if domain == "" {
		return "tel:" + phone
	}
	return "sip:" + phone + "@" + domain
}

// RouteSet builds a Route header set from a service route list.
func RouteSet(routes []string) string {
	var out []string
	for _, r := range routes {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if strings.HasPrefix(r, "<") {
			out = append(out, r)
		} else {
			out = append(out, "<"+r+">")
		}
	}
	return strings.Join(out, ", ")
}

// SecAgreeProtectedHeaders returns the headers protected by the security
// agreement (RFC 3329): the headers that must be integrity-protected.
func SecAgreeProtectedHeaders() []string {
	return []string{
		"Proxy-Authorization",
		"Proxy-Require",
		"Authorization",
		"P-Access-Network-Info",
		"P-Charging-Vector",
		"P-Charging-Function-Addresses",
		"Security-Client",
		"Security-Server",
		"Security-Verify",
		"Path",
		"Service-Route",
	}
}
