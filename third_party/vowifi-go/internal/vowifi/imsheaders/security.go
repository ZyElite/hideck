package imsheaders

import "strings"

// Header is one IMS-specific SIP header.
type Header struct {
	Name  string
	Value string
}

// HasSecurityVerify reports whether a usable Security-Verify value exists.
func HasSecurityVerify(value string) bool {
	return strings.TrimSpace(value) != ""
}

// SecurityVerifyHeader builds a normalized Security-Verify header.
func SecurityVerifyHeader(value string) Header {
	return Header{Name: "Security-Verify", Value: strings.TrimSpace(value)}
}

// SecAgreeProtectedHeaders returns the sec-agree headers for a protected SIP
// request. An empty Security-Verify value means no protected agreement exists.
func SecAgreeProtectedHeaders(securityVerify string) []Header {
	if !HasSecurityVerify(securityVerify) {
		return nil
	}
	return []Header{
		{Name: "Require", Value: "sec-agree"},
		{Name: "Proxy-Require", Value: "sec-agree"},
		SecurityVerifyHeader(securityVerify),
	}
}
