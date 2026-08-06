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
		}
		if len(digits) == 15 {
			break
		}
	}
	return string(digits)
}

// NormalizeSipInstance normalizes a +sip.instance value to the canonical
// "urn:uuid:<uuid>" form.
func NormalizeSipInstance(instance string) string {
	instance = strings.TrimSpace(instance)
	instance = strings.Trim(instance, "\"")
	if instance == "" {
		return ""
	}
	if strings.HasPrefix(instance, "urn:uuid:") {
		return instance
	}
	return "urn:uuid:" + instance
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
