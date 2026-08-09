package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// DiscoveryResult is the error-reporting form of the legacy discovery result.
type DiscoveryResult struct {
	Registrar string
	Source    string
}

func registrarTransportCandidates(transport string) []string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "udp":
		return []string{"udp"}
	default:
		return []string{"tcp", "udp"}
	}
}

// DiscoverRegistrarAutoViaDNS restores the legacy registrar discovery API.
func DiscoverRegistrarAutoViaDNS(
	ctx context.Context,
	domain, transport string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
) (string, string, bool) {
	result, err := DiscoverRegistrarAutoViaDNSResult(ctx, domain, transport, preferIPv6, bindIP, servers)
	return result.Registrar, result.Source, err == nil
}

// DiscoverRegistrarAutoViaDNSResult preserves discovery errors for new callers.
func DiscoverRegistrarAutoViaDNSResult(
	ctx context.Context,
	domain, transport string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
) (DiscoveryResult, error) {
	return discoverRegistrarWithOptions(
		ctx, domain, transport, preferIPv6, bindIP, servers,
		true, ReadSystemDNSServers(), true, nil, newServerResolver,
	)
}

// DiscoverRegistrarAutoViaDNSWithOptions restores the legacy explicit-stage API.
func DiscoverRegistrarAutoViaDNSWithOptions(
	ctx context.Context,
	domain, transport string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
	includeSystem bool,
	systemServers []net.IP,
	includePublic bool,
	publicServers []net.IP,
) (string, string, bool) {
	result, err := discoverRegistrarWithOptions(
		ctx, domain, transport, preferIPv6, bindIP, servers,
		includeSystem, systemServers, includePublic, publicServers, newServerResolver,
	)
	return result.Registrar, result.Source, err == nil
}

func discoverRegistrarWithOptions(
	ctx context.Context,
	domain, transport string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
	includeSystem bool,
	systemServers []net.IP,
	includePublic bool,
	publicServers []net.IP,
	factory resolverFactory,
) (DiscoveryResult, error) {
	stages := registrarDiscoveryDNSServerStages(
		servers, bindIP, includeSystem, systemServers, includePublic, publicServers,
	)
	var failures []error
	for _, stage := range stages {
		registrar, err := discoverRegistrarViaDNS(
			ctx, domain, transport, preferIPv6, bindIP, stage.Servers, factory,
		)
		if err == nil {
			return DiscoveryResult{Registrar: registrar, Source: stage.Source}, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", stage.Source, err))
	}
	if len(stages) == 0 {
		return DiscoveryResult{}, errors.New("dns: no DNS servers match the bind address")
	}
	return DiscoveryResult{}, fmt.Errorf("dns: discover registrar for %s: %w", domain, errors.Join(failures...))
}

// DiscoverRegistrarViaDNS restores direct SRV, NAPTR and address discovery.
func DiscoverRegistrarViaDNS(
	ctx context.Context,
	domain, transport string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
) (string, bool) {
	registrar, err := discoverRegistrarViaDNS(
		ctx, domain, transport, preferIPv6, bindIP, servers, newServerResolver,
	)
	return registrar, err == nil
}

func discoverRegistrarViaDNS(
	ctx context.Context,
	domain, transport string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
	factory resolverFactory,
) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", errors.New("dns: empty registrar domain")
	}
	ordered := OrderDNSServersByPreference(servers, preferIPv6)
	if len(ordered) == 0 {
		return "", errors.New("dns: no DNS servers")
	}
	var failures []error
	for _, transportCandidate := range registrarTransportCandidates(transport) {
		for _, server := range ordered {
			if !serverUsableForBind(server, bindIP) {
				continue
			}
			registrar, err := discoverRegistrarFromServer(
				ctx, factory(bindIP, server), domain, transportCandidate, preferIPv6,
			)
			if err == nil {
				return registrar, nil
			}
			failures = append(failures, fmt.Errorf("%s via %s: %w", transportCandidate, server, err))
		}
	}
	if len(failures) == 0 {
		return "", errors.New("dns: no DNS servers match the bind address")
	}
	return "", errors.Join(failures...)
}
