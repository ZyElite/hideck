package sipkit

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// ParseURI validates a SIP, SIPS, TEL, or URN URI.
func ParseURI(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("URI 为空")
	}
	if strings.HasPrefix(strings.ToLower(value), "tel:") {
		if strings.TrimSpace(value[len("tel:"):]) == "" {
			return errors.New("tel URI 为空")
		}
		return nil
	}
	_, err := parseURIValue(value)
	return err
}

// ParseAORWithDefaultHost validates an address-of-record, supplying its host
// when only a user part is provided.
func ParseAORWithDefaultHost(aor, defaultHost string) error {
	aor = strings.TrimSpace(aor)
	if aor == "" {
		return errors.New("AOR 为空")
	}
	if strings.HasPrefix(strings.ToLower(aor), "tel:") {
		return ParseURI(aor)
	}
	if strings.Contains(aor, "@") {
		return validateAORWithHost(aor, defaultHost)
	}
	return validateBareAOR(aor, defaultHost)
}

func validateAORWithHost(aor, defaultHost string) error {
	uri, err := parseURIValue(aor)
	if err != nil {
		return err
	}
	if strings.TrimSpace(uri.Host) != "" {
		return nil
	}
	uri.Host = strings.Trim(NormalizeHost(defaultHost), "[]")
	return ParseURI(uri.String())
}

func validateBareAOR(aor, defaultHost string) error {
	scheme := "sip"
	lower := strings.ToLower(aor)
	if strings.HasPrefix(lower, "sip:") {
		aor = strings.TrimSpace(aor[4:])
	} else if strings.HasPrefix(lower, "sips:") {
		scheme, aor = "sips", strings.TrimSpace(aor[5:])
	}
	host := strings.Trim(NormalizeHost(defaultHost), "[]")
	if host == "" {
		return errors.New("AOR host 为空")
	}
	boundary := strings.IndexAny(aor, ";?")
	user, suffix := aor, ""
	if boundary >= 0 {
		user, suffix = aor[:boundary], aor[boundary:]
	}
	return ParseURI(scheme + ":" + strings.TrimSpace(user) + "@" + NormalizeHost(host) + suffix)
}

// ExtractURIFromHeaderValue extracts and validates the URI in a SIP header.
func ExtractURIFromHeaderValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("header URI 为空")
	}
	if start := strings.IndexByte(value, '<'); start >= 0 {
		if end := strings.IndexByte(value[start+1:], '>'); end >= 0 {
			uri := strings.TrimSpace(value[start+1 : start+1+end])
			return uri, ParseURI(uri)
		}
	}
	if separator := strings.IndexByte(value, ';'); separator >= 0 {
		value = value[:separator]
	}
	value = strings.TrimSpace(value)
	if !hasURIScheme(value) {
		value = "sip:" + value
	}
	return value, ParseURI(value)
}

// ParseHostPortWithDefault parses either a SIP URI or host[:port].
func ParseHostPortWithDefault(value string, defaultPort int) (string, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, errors.New("host 为空")
	}
	if hasURIScheme(value) {
		uri, err := parseURIValue(value)
		if err != nil {
			return "", 0, err
		}
		return NormalizeHost(uri.Host), defaultIfZero(uri.Port, defaultPort), nil
	}
	if boundary := strings.IndexAny(value, ";?"); boundary >= 0 {
		value = value[:boundary]
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return NormalizeHost(strings.Trim(value, "[]")), defaultPort, nil
	}
	port, _ := strconv.Atoi(portText)
	return NormalizeHost(host), defaultIfZero(port, defaultPort), nil
}

func defaultIfZero(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// NormalizeHost removes existing brackets and brackets colon-bearing hosts.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func hasURIScheme(value string) bool {
	value = strings.TrimSpace(value)
	separator := strings.IndexByte(value, ':')
	if separator <= 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value[:separator])) {
	case "sip", "sips", "tel", "urn":
		return true
	default:
		return false
	}
}

func parseURIValue(value string) (*sip.Uri, error) {
	value = strings.TrimSpace(value)
	if !hasURIScheme(value) {
		value = "sip:" + value
	}
	uri := &sip.Uri{}
	if err := sip.ParseUri(value, uri); err != nil {
		return nil, err
	}
	return uri, nil
}
