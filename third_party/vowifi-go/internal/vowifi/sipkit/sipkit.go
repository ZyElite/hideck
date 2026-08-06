// Package sipkit provides the lightweight SIP URI/header helpers and request
// builders used by the IMS stack on top of sipgo.
//
// Reconstructed from the decompiled internal/vowifi/sipkit.
package sipkit

import (
	"fmt"
	"net"
	"strings"

	"github.com/emiago/sipgo/sip"
)

// --- URI helpers ---

// hasURIScheme reports whether s starts with a URI scheme (sip:, sips:, tel:).
func hasURIScheme(s string) bool {
	s = strings.TrimSpace(s)
	for _, scheme := range []string{"sip:", "sips:", "tel:"} {
		if strings.HasPrefix(strings.ToLower(s), scheme) {
			return true
		}
	}
	return false
}

// ParseURI parses a SIP URI string into a sip.Uri.
func ParseURI(s string) (*sip.Uri, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("sipkit: empty URI")
	}
	uri := &sip.Uri{}
	if err := sip.ParseUri(s, uri); err != nil {
		return nil, fmt.Errorf("sipkit: parse URI %q: %w", s, err)
	}
	return uri, nil
}

// ParseAORWithDefaultHost parses an address-of-record, defaulting the host to
// the given domain when absent.
func ParseAORWithDefaultHost(aor, defaultHost string) (*sip.Uri, error) {
	aor = strings.TrimSpace(aor)
	if !hasURIScheme(aor) {
		aor = "sip:" + aor
	}
	uri, err := ParseURI(aor)
	if err != nil {
		return nil, err
	}
	if uri.Host == "" {
		uri.Host = defaultHost
	}
	return uri, nil
}

// ExtractURIFromHeaderValue extracts the first URI from a header value.
func ExtractURIFromHeaderValue(v string) (*sip.Uri, error) {
	v = strings.TrimSpace(v)
	// Take the first token (up to a space, tab, semicolon or comma).
	if i := strings.IndexAny(v, " \t;,"); i >= 0 {
		v = v[:i]
	}
	// Strip angle brackets.
	v = strings.Trim(v, "<>")
	return ParseURI(v)
}

// ParseHostPortWithDefault parses a host[:port] string with a default port.
func ParseHostPortWithDefault(s string, defaultPort int) (string, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, fmt.Errorf("sipkit: empty host")
	}
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return s, defaultPort, nil
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, fmt.Errorf("sipkit: bad port %q", portStr)
	}
	return host, port, nil
}

// NormalizeHost normalizes a host for SIP: lower-cases and brackets IPv6.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() == nil && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

// --- header helpers ---

// FirstHeaderValue returns the first value of a header.
func FirstHeaderValue(msg sip.Message, name string) string {
	if msg == nil {
		return ""
	}
	headers := msg.GetHeaders(name)
	if len(headers) == 0 {
		return ""
	}
	return strings.TrimSpace(headers[0].Value())
}

// FirstLine returns the first line of a SIP message (request/status line).
func FirstLine(msg sip.Message) string {
	if msg == nil {
		return ""
	}
	switch m := msg.(type) {
	case *sip.Request:
		return m.StartLine()
	case *sip.Response:
		return m.StartLine()
	default:
		return ""
	}
}

// SanitizeBody redacts sensitive content in a SIP body.
func SanitizeBody(body string) string {
	// Redact Authorization-like lines.
	lines := strings.Split(body, "\r\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.ToLower(l), "authorization:") {
			lines[i] = "Authorization: [redacted]"
		}
	}
	return strings.Join(lines, "\r\n")
}

// --- transport ---

// normalizeSIPTransport normalizes a transport token.
func normalizeSIPTransport(t string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "TCP":
		return "TCP"
	case "TLS":
		return "TLS"
	default:
		return "UDP"
	}
}

// pickTransport selects the transport for a request.
func pickTransport(transport string, secure bool) string {
	if secure {
		return "TLS"
	}
	return normalizeSIPTransport(transport)
}

// SetRequestTransport sets the transport on a request's Via and Route.
func SetRequestTransport(req *sip.Request, transport string) {
	if req == nil {
		return
	}
	transport = normalizeSIPTransport(transport)
	applyRequestTransport(req, transport)
}

// applyRequestTransport rewrites the Via/Route transport parameters.
func applyRequestTransport(req *sip.Request, transport string) {
	if req == nil {
		return
	}
	if via := req.Via(); via != nil {
		via.Transport = transport
	}
	for _, route := range req.GetHeaders("Route") {
		_ = route
	}
}

// --- Via / Route ---

// normalizeViaRouteTransport normalizes a Via/Route transport token.
func normalizeViaRouteTransport(t string) string {
	return normalizeSIPTransport(t)
}

// defaultViaRoutePort returns the default port for a transport.
func defaultViaRoutePort(transport string) int {
	switch normalizeSIPTransport(transport) {
	case "TLS":
		return 5061
	default:
		return 5060
	}
}

// normalizeHostForVia normalizes a host for a Via header.
func normalizeHostForVia(host string) string {
	return NormalizeHost(host)
}

// parseHostPortFromLocalAddr parses a local address string into host/port.
func parseHostPortFromLocalAddr(addr string) (string, int, error) {
	return ParseHostPortWithDefault(addr, 5060)
}

// ResolveViaRoute resolves the Via/Route for a request given the local address
// and transport.
func ResolveViaRoute(localAddr, transport string) (host string, port int, err error) {
	host, port, err = parseHostPortFromLocalAddr(localAddr)
	if err != nil {
		return "", 0, err
	}
	if port == 0 {
		port = defaultViaRoutePort(transport)
	}
	return host, port, nil
}

// chooseRouteSet selects the route set for a request.
func chooseRouteSet(serviceRoute, path []string) []string {
	if len(serviceRoute) > 0 {
		return serviceRoute
	}
	return path
}

// --- header policy ---

// securityModeIsIPSec3GPP reports whether the security mode is 3GPP IPsec.
func securityModeIsIPSec3GPP(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), "ipsec-3gpp")
}

// requiresPANI reports whether a request needs the P-Access-Network-Info header.
func requiresPANI(method string) bool {
	switch strings.ToUpper(method) {
	case "REGISTER", "INVITE", "SUBSCRIBE", "MESSAGE", "OPTIONS":
		return true
	default:
		return false
	}
}

// requiresPPI reports whether a request needs the P-Preferred-Identity header.
func requiresPPI(method string) bool {
	switch strings.ToUpper(method) {
	case "INVITE", "MESSAGE", "SUBSCRIBE", "REFER":
		return true
	default:
		return false
	}
}

// requiresSecurityClient reports whether a request needs Security-Client.
func requiresSecurityClient(method string) bool {
	return strings.EqualFold(method, "REGISTER")
}

// requiresSecurityVerify reports whether a request needs Security-Verify.
func requiresSecurityVerify(method string) bool {
	return strings.EqualFold(method, "REGISTER")
}

// allowHeaderPolicy returns the Allow header value for a method.
func allowHeaderPolicy(method string) string {
	switch strings.ToUpper(method) {
	case "INVITE":
		return "INVITE, ACK, CANCEL, BYE, PRACK, UPDATE, REFER, NOTIFY, MESSAGE, OPTIONS, INFO, SUBSCRIBE"
	default:
		return "INVITE, ACK, CANCEL, BYE, PRACK, UPDATE, REFER, NOTIFY, MESSAGE, OPTIONS, INFO, SUBSCRIBE"
	}
}

// ResolveSIPHeaderPolicy resolves the header policy for a request method.
func ResolveSIPHeaderPolicy(method string) map[string]bool {
	return map[string]bool{
		"P-Access-Network-Info": requiresPANI(method),
		"P-Preferred-Identity":  requiresPPI(method),
		"Security-Client":       requiresSecurityClient(method),
		"Security-Verify":       requiresSecurityVerify(method),
	}
}

// --- CSV helpers ---

// splitCSV splits a comma-separated value list.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// containsTokenFold reports whether a CSV list contains a token (case-folded).
func containsTokenFold(csv, token string) bool {
	for _, t := range splitCSV(csv) {
		if strings.EqualFold(t, token) {
			return true
		}
	}
	return false
}

// removeCSVTokenFold removes a token from a CSV list (case-folded).
func removeCSVTokenFold(csv, token string) string {
	var kept []string
	for _, t := range splitCSV(csv) {
		if !strings.EqualFold(t, token) {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, ", ")
}

// ensureCSVToken ensures a token is present in a CSV list.
func ensureCSVToken(csv, token string) string {
	if containsTokenFold(csv, token) {
		return csv
	}
	if strings.TrimSpace(csv) == "" {
		return token
	}
	return csv + ", " + token
}

// --- dialog helpers ---

// isDialogOwnedHeader reports whether a header is dialog-owned (not copied
// into in-dialog requests).
func isDialogOwnedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "route", "record-route", "contact", "expires", "allow", "supported", "require":
		return true
	default:
		return false
	}
}

// applyDialogPANI applies the P-Access-Network-Info header to a dialog request.
func applyDialogPANI(req *sip.Request, pani string) {
	if req == nil || pani == "" {
		return
	}
	req.AppendHeader(sip.NewHeader("P-Access-Network-Info", pani))
}

// applyDialogSecurityHeaders applies the security headers to a dialog request.
func applyDialogSecurityHeaders(req *sip.Request, securityClient, securityVerify string) {
	if req == nil {
		return
	}
	if securityClient != "" {
		req.AppendHeader(sip.NewHeader("Security-Client", securityClient))
	}
	if securityVerify != "" {
		req.AppendHeader(sip.NewHeader("Security-Verify", securityVerify))
	}
}

// pickUserAgent returns the User-Agent value.
func pickUserAgent(ua string) string {
	if ua == "" {
		return "vowifi"
	}
	return ua
}

// pickSecurityVerify returns the Security-Verify value.
func pickSecurityVerify(server string) string {
	return server
}

// --- request builders ---

// BuildIMSRequest builds an IMS request with the given method, URI and headers.
func BuildIMSRequest(method string, uri *sip.Uri, headers []sip.Header) (*sip.Request, error) {
	if uri == nil {
		return nil, fmt.Errorf("sipkit: nil request URI")
	}
	req := sip.NewRequest(sip.RequestMethod(method), *uri)
	for _, h := range headers {
		req.AppendHeader(h)
	}
	return req, nil
}

// BuildMinimalDialogRequest builds a minimal in-dialog request.
func BuildMinimalDialogRequest(method string, uri *sip.Uri, callID, fromTag, toTag string, cseq int) (*sip.Request, error) {
	req, err := BuildIMSRequest(method, uri, nil)
	if err != nil {
		return nil, err
	}
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("From", fromTag))
	req.AppendHeader(sip.NewHeader("To", toTag))
	req.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d %s", cseq, method)))
	return req, nil
}

// BuildCancelFromInvite builds a CANCEL request from an INVITE.
func BuildCancelFromInvite(invite *sip.Request) (*sip.Request, error) {
	if invite == nil {
		return nil, fmt.Errorf("sipkit: nil INVITE")
	}
	cancel := sip.NewRequest(sip.CANCEL, invite.Recipient)
	// Copy the dialog identifiers.
	if hs := invite.GetHeaders("Call-ID"); len(hs) > 0 {
		cancel.AppendHeader(hs[0])
	}
	if hs := invite.GetHeaders("From"); len(hs) > 0 {
		cancel.AppendHeader(hs[0])
	}
	if hs := invite.GetHeaders("To"); len(hs) > 0 {
		cancel.AppendHeader(hs[0])
	}
	if hs := invite.GetHeaders("CSeq"); len(hs) > 0 {
		cancel.AppendHeader(sip.NewHeader("CSeq", strings.Replace(hs[0].Value(), "INVITE", "CANCEL", 1)))
	}
	return cancel, nil
}

// ApplyAutoHeaders applies the auto-generated headers to a request.
func ApplyAutoHeaders(req *sip.Request, opts map[string]string) {
	if req == nil {
		return
	}
	if ua, ok := opts["user_agent"]; ok {
		req.AppendHeader(sip.NewHeader("User-Agent", pickUserAgent(ua)))
	}
	if pani, ok := opts["pani"]; ok && pani != "" {
		req.AppendHeader(sip.NewHeader("P-Access-Network-Info", pani))
	}
	if ppi, ok := opts["ppi"]; ok && ppi != "" {
		req.AppendHeader(sip.NewHeader("P-Preferred-Identity", ppi))
	}
}

// --- response routing ---

// DispatchResponseByVia routes a response to the transport via the Via header.
func DispatchResponseByVia(resp *sip.Response, send func(addr string, data []byte) error) error {
	if resp == nil {
		return fmt.Errorf("sipkit: nil response")
	}
	via := resp.Via()
	if via == nil {
		return fmt.Errorf("sipkit: response has no Via")
	}
	addr := via.Host
	if via.Port != 0 {
		addr = fmt.Sprintf("%s:%d", via.Host, via.Port)
	}
	return send(addr, []byte(resp.String()))
}

// WriteResponseByVia writes a response to the Via address.
func WriteResponseByVia(resp *sip.Response, write func(addr string, data []byte) error) error {
	return DispatchResponseByVia(resp, write)
}
