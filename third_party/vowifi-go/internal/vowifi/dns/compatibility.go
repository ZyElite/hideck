package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// RegistrarCandidate retains the structured candidate API added after v1.5.5.
type RegistrarCandidate struct {
	Host      string
	Port      int
	Transport string
	Priority  uint16
	Weight    uint16
}

// DiscoverOptions retains the post-v1.5.5 structured discovery options.
type DiscoverOptions struct {
	Domain     string
	DNSServers []string
	Timeout    time.Duration
}

// DiscoverRegistrarCandidatesAutoViaDNS is the structured counterpart of the legacy API.
func DiscoverRegistrarCandidatesAutoViaDNS(domain string) ([]RegistrarCandidate, error) {
	return DiscoverRegistrarCandidatesAutoViaDNSWithOptions(domain, DiscoverOptions{})
}

// DiscoverRegistrarCandidatesAutoViaDNSWithOptions discovers structured candidates.
func DiscoverRegistrarCandidatesAutoViaDNSWithOptions(
	domain string,
	options DiscoverOptions,
) ([]RegistrarCandidate, error) {
	servers := options.DNSServers
	if len(servers) == 0 {
		for _, server := range ReadSystemDNSServers() {
			servers = append(servers, server.String())
		}
	}
	if len(servers) == 0 {
		servers = []string{"1.1.1.1"}
	}
	return DiscoverRegistrarCandidatesViaDNS(domain, servers, options.Timeout)
}

// DiscoverRegistrarCandidatesViaDNS performs structured SRV discovery.
func DiscoverRegistrarCandidatesViaDNS(
	domain string,
	servers []string,
	timeout time.Duration,
) ([]RegistrarCandidate, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, errors.New("dns: empty registrar domain")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var candidates []RegistrarCandidate
	var failures []error
	for _, transport := range []string{"udp", "tcp", "tls"} {
		found, err := lookupStructuredSRV(ctx, domain, transport, servers)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		candidates = append(candidates, found...)
	}
	if len(candidates) > 0 {
		return candidates, nil
	}
	addresses, addressErr := lookupStructuredAddresses(ctx, domain, servers)
	if addressErr == nil {
		return addresses, nil
	}
	failures = append(failures, addressErr)
	if len(failures) == 0 {
		failures = append(failures, errors.New("no DNS servers configured"))
	}
	return nil, fmt.Errorf("dns: no registrar found for %s: %w", domain, errors.Join(failures...))
}

func lookupStructuredSRV(
	ctx context.Context,
	domain, transport string,
	servers []string,
) ([]RegistrarCandidate, error) {
	var failures []error
	for _, endpoint := range servers {
		resolver, err := resolverForEndpoint(endpoint)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		_, records, err := resolver.LookupSRV(ctx, "sip", transport, domain)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		result := make([]RegistrarCandidate, 0, len(records))
		for _, record := range records {
			result = append(result, RegistrarCandidate{
				Host: strings.TrimSuffix(record.Target, "."), Port: int(record.Port),
				Transport: transport, Priority: record.Priority, Weight: record.Weight,
			})
		}
		if len(result) > 0 {
			return result, nil
		}
	}
	if len(failures) == 0 {
		return nil, errors.New("dns: no DNS servers configured")
	}
	return nil, errors.Join(failures...)
}

func lookupStructuredAddresses(
	ctx context.Context,
	domain string,
	servers []string,
) ([]RegistrarCandidate, error) {
	var failures []error
	for _, endpoint := range servers {
		resolver, err := resolverForEndpoint(endpoint)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		addresses, err := resolver.LookupIPAddr(ctx, domain)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		result := make([]RegistrarCandidate, 0, len(addresses))
		seen := make(map[string]struct{})
		for _, address := range addresses {
			host := address.IP.String()
			if address.IP == nil {
				continue
			}
			if _, exists := seen[host]; exists {
				continue
			}
			seen[host] = struct{}{}
			result = append(result, RegistrarCandidate{
				Host: host, Port: defaultRegistrarSIPPort, Transport: "udp",
			})
		}
		if len(result) > 0 {
			return result, nil
		}
		failures = append(failures, fmt.Errorf("dns: no address records for %s", domain))
	}
	if len(failures) == 0 {
		return nil, errors.New("dns: no DNS servers configured")
	}
	return nil, errors.Join(failures...)
}

func resolverForEndpoint(endpoint string) (queryResolver, error) {
	endpoint = strings.TrimSpace(endpoint)
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		host, port = endpoint, defaultDNSPort
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if ip := net.ParseIP(host); ip != nil {
		return newServerResolverAt(nil, ip, port), nil
	}
	if err != nil || host == "" {
		return nil, fmt.Errorf("dns: DNS server must be an IP address or host:port: %q", endpoint)
	}
	return newServerResolverForAddress(nil, net.JoinHostPort(host, port)), nil
}

// NormalizeRegistrarCandidates retains structured defaulting and deduplication.
func NormalizeRegistrarCandidates(candidates []RegistrarCandidate) []RegistrarCandidate {
	seen := make(map[string]struct{})
	result := make([]RegistrarCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Port == 0 {
			candidate.Port = defaultRegistrarSIPPort
			if candidate.Transport == "tls" {
				candidate.Port = 5061
			}
		}
		key := net.JoinHostPort(candidate.Host, strconv.Itoa(candidate.Port)) + ":" + candidate.Transport
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

// LookupHostIP resolves through the system resolver.
func LookupHostIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}
