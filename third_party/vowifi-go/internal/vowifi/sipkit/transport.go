package sipkit

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/emiago/sipgo/sip"
)

const (
	defaultSIPPort = 5060
	defaultTLSPort = 5061
	transportTCP   = "TCP"
	transportTLS   = "TLS"
	transportUDP   = "UDP"
	transportParam = "transport"
)

func normalizeSIPTransport(transport string) string {
	switch strings.ToUpper(strings.TrimSpace(transport)) {
	case transportTCP, transportUDP:
		return strings.ToUpper(strings.TrimSpace(transport))
	default:
		return transportTCP
	}
}

func normalizeViaRouteTransport(transport string) string {
	switch strings.ToUpper(strings.TrimSpace(transport)) {
	case transportTCP, transportTLS, transportUDP:
		return strings.ToUpper(strings.TrimSpace(transport))
	default:
		return transportUDP
	}
}

func defaultViaRoutePort(transport string) int {
	if normalizeViaRouteTransport(transport) == transportTLS {
		return defaultTLSPort
	}
	return defaultSIPPort
}

// SetRequestTransport applies transport consistently to the request, Via, and
// Request-URI.
func SetRequestTransport(request *sip.Request, transport string) {
	applyRequestTransport(request, transport, false)
}

func applyRequestTransport(request *sip.Request, transport string, omitURITransport bool) {
	if request == nil {
		return
	}
	transport = normalizeSIPTransport(transport)
	request.SetTransport(transport)
	if via := request.Via(); via != nil {
		via.Transport = transport
	}
	request.Recipient.UriParams.Remove(transportParam)
	if omitURITransport || transport == transportUDP {
		return
	}
	if request.Recipient.UriParams == nil {
		request.Recipient.UriParams = sip.NewParams()
	}
	request.Recipient.UriParams.Add(transportParam, strings.ToLower(transport))
}

func normalizeHostForVia(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if strings.Count(host, ":") > 1 {
		return "[" + host + "]"
	}
	return host
}

func parseHostPortFromLocalAddr(address string) (string, int) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", 0
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return strings.Trim(address, "[]"), 0
	}
	port, _ := strconv.Atoi(portText)
	return strings.Trim(host, "[]"), port
}

// ResolveViaRoute resolves the response network and destination from the top
// Via header. The source result reports whether sent-by or received won.
func ResolveViaRoute(via *sip.ViaHeader) (transport, destination, source string, err error) {
	if via == nil {
		return "", "", "", errors.New("Via 头为空")
	}
	transport = normalizeViaRouteTransport(via.Transport)
	host, source := strings.TrimSpace(via.Host), "sent-by"
	if received, ok := via.Params.Get("received"); ok && strings.TrimSpace(received) != "" {
		host, source = strings.TrimSpace(received), "received"
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return "", "", "", errors.New("missing Via host")
	}
	port := via.Port
	if rport, ok := via.Params.Get("rport"); ok && strings.TrimSpace(rport) != "" {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(rport)); parseErr == nil && parsed > 0 {
			port = parsed
		}
	}
	if port <= 0 {
		port = defaultViaRoutePort(transport)
	}
	return transport, net.JoinHostPort(host, strconv.Itoa(port)), source, nil
}
