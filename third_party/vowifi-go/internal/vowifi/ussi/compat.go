package ussi

import (
	"context"
	"fmt"
	"strings"
)

// BuildInitialInvite is retained for callers that inspect the recovered API.
// Network operations use the fully configured builder in service.go.
func BuildInitialInvite(aor, domain, localIP, callID string) string {
	return fmt.Sprintf("INVITE sip:%s@%s SIP/2.0\r\nCall-ID: %s\r\nContact: <sip:%s>\r\nContent-Length: 0\r\n\r\n", aor, domain, callID, localIP)
}

// BuildInfo is retained for source compatibility.
func BuildInfo(callID, sessionID, input string) string {
	body, _ := requestXML(input)
	return fmt.Sprintf("INFO sip:ussi@localhost SIP/2.0\r\nCall-ID: %s\r\nX-USSI-Session: %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s", callID, sessionID, ContentType, len(body), body)
}

func buildDialogRequestCompatibility(method, aor, domain, _ string, callID string) string {
	return fmt.Sprintf("%s sip:%s@%s SIP/2.0\r\nCall-ID: %s\r\n", method, aor, domain, callID)
}

func dialogRequestURI(aor, domain string) string { return "sip:" + aor + "@" + domain }

func taggedAddress(address, tag string) string {
	if tag == "" {
		return address
	}
	return address + ";tag=" + tag
}

func splitLocalAddr(address string) (string, string) {
	if index := strings.LastIndexByte(address, ':'); index > 0 {
		return strings.Trim(address[:index], "[]"), address[index+1:]
	}
	return address, "5060"
}

func contactHeader(localIP, port, instance string) string {
	header := fmt.Sprintf("<sip:%s:%s>", localIP, port)
	if instance != "" {
		header += `;+sip.instance="urn:uuid:` + instance + `"`
	}
	return header
}

func contextFromEndpoint(domain string) context.Context {
	return context.WithValue(context.Background(), endpointDomainKey{}, domain)
}

type endpointDomainKey struct{}

func domainFromAOR(aor string) string {
	if index := strings.LastIndexByte(aor, '@'); index >= 0 {
		return aor[index+1:]
	}
	return ""
}

// Profile is the recovered USSI profile surface.
type Profile struct {
	ContactParams string
	IMPI          string
	Domain        string
}

func (p *Profile) ApplyInitialInvite(contact string) {
	if p != nil {
		p.ContactParams = contact
	}
}

func (p *Profile) ContactHeaderParams() string {
	if p == nil {
		return ""
	}
	return p.ContactParams
}
