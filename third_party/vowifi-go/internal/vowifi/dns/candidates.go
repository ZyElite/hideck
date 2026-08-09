package dns

import (
	"context"
	"net"
	"strconv"
	"strings"
)

// ExpandRegistrarCandidates resolves host candidates and preserves their ports.
func ExpandRegistrarCandidates(
	ctx context.Context,
	candidates string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
) string {
	candidates = strings.TrimSpace(candidates)
	if candidates == "" {
		return ""
	}
	result := make([]string, 0, strings.Count(candidates, ",")+1)
	seen := make(map[string]struct{})
	cache := make(map[string][]net.IP)
	for _, candidate := range strings.Split(candidates, ",") {
		host, port, ok := parserRegistrarHostPort(candidate, defaultRegistrarSIPPort)
		if !ok {
			continue
		}
		normalized := strings.Trim(strings.TrimSpace(host), "[]")
		if ip := net.ParseIP(normalized); ip != nil {
			appendUniqueEndpoints(&result, seen, []string{ip.String()}, port)
			continue
		}
		addresses, exists := cache[normalized]
		if !exists {
			addresses = LookupHostIPViaDNSServers(ctx, normalized, preferIPv6, bindIP, servers)
			cache[normalized] = addresses
		}
		if len(addresses) == 0 {
			appendUniqueEndpoints(&result, seen, []string{normalized}, port)
			continue
		}
		hosts := make([]string, 0, len(addresses))
		for _, address := range addresses {
			hosts = append(hosts, address.String())
		}
		appendUniqueEndpoints(&result, seen, hosts, port)
	}
	return strings.Join(result, ",")
}

func parserRegistrarHostPort(candidate string, defaultPort int) (string, int, bool) {
	candidate = strings.TrimSpace(candidate)
	if strings.HasPrefix(candidate, "sip:") {
		candidate = candidate[len("sip:"):]
	}
	if strings.HasPrefix(candidate, "sips:") {
		candidate = candidate[len("sips:"):]
	}
	if index := strings.IndexByte(candidate, ';'); index >= 0 {
		candidate = candidate[:index]
	}
	if index := strings.IndexByte(candidate, '?'); index >= 0 {
		candidate = candidate[:index]
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", 0, false
	}
	host, portText, err := net.SplitHostPort(candidate)
	if err != nil {
		return strings.TrimSpace(candidate), defaultPort, true
	}
	port, _ := strconv.Atoi(portText)
	return strings.TrimSpace(host), port, true
}
