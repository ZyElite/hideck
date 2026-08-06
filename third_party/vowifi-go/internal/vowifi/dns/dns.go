// Package dns implements IMS registrar discovery via DNS (RFC 3263) and the
// DNS-server selection helpers used by the IMS stack.
//
// Reconstructed from the decompiled internal/vowifi/dns.
package dns

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// RegistrarCandidate is one discovered registrar endpoint.
type RegistrarCandidate struct {
	Host string
	Port int
	// Transport is "udp", "tcp" or "tls".
	Transport string
	// Priority / Weight from the SRV record (RFC 2782).
	Priority uint16
	Weight   uint16
}

// DiscoverOptions tunes registrar discovery.
type DiscoverOptions struct {
	// Domain is the IMS domain (e.g. "ims.mnc026.mcc310.3gppnetwork.org").
	Domain string
	// DNSServers overrides the DNS servers used for the lookup.
	DNSServers []string
	// Timeout bounds each DNS query.
	Timeout time.Duration
}

// defaultRegistrarPublicDNSServers are the fallback public resolvers.
func defaultRegistrarPublicDNSServers() []string {
	return []string{"8.8.8.8:53", "1.1.1.1:53"}
}

// ReadSystemDNSServers reads the system DNS servers from /etc/resolv.conf.
func ReadSystemDNSServers() []string {
	cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range cfg.Servers {
		out = append(out, net.JoinHostPort(s, cfg.Port))
	}
	return out
}

// FilterDNSServersForBind keeps only servers usable for binding (non-empty,
// valid addresses).
func FilterDNSServersForBind(servers []string) []string {
	var out []string
	for _, s := range servers {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			host = s
		}
		if net.ParseIP(host) == nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

// OrderDNSServersByPreference orders DNS servers, preferring loopback and
// link-local resolvers first.
func OrderDNSServersByPreference(servers []string) []string {
	scored := make([]struct {
		s string
		n int
	}, 0, len(servers))
	for _, s := range servers {
		host, _, err := net.SplitHostPort(s)
		if err != nil {
			host = s
		}
		ip := net.ParseIP(host)
		n := 0
		if ip != nil && ip.IsLoopback() {
			n = 3
		} else if ip != nil && ip.IsLinkLocalUnicast() {
			n = 2
		} else if ip != nil && ip.IsPrivate() {
			n = 1
		}
		scored = append(scored, struct {
			s string
			n int
		}{s, n})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].n > scored[j].n })
	out := make([]string, 0, len(scored))
	for _, e := range scored {
		out = append(out, e.s)
	}
	return out
}

// registrarDiscoveryDNSServerStages returns the ordered DNS server stages:
// system servers first, then the public fallbacks.
func registrarDiscoveryDNSServerStages() [][]string {
	system := ReadSystemDNSServers()
	system = OrderDNSServersByPreference(FilterDNSServersForBind(system))
	return [][]string{system, defaultRegistrarPublicDNSServers()}
}

// registrarTransportCandidates are the transport schemes probed in order.
func registrarTransportCandidates() []string {
	return []string{"udp", "tcp", "tls"}
}

// DiscoverRegistrarAutoViaDNS discovers the IMS registrar for the domain using
// the system DNS servers, falling back to public resolvers.
func DiscoverRegistrarAutoViaDNS(domain string) ([]RegistrarCandidate, error) {
	return DiscoverRegistrarAutoViaDNSWithOptions(domain, DiscoverOptions{})
}

// DiscoverRegistrarAutoViaDNSWithOptions discovers the registrar with explicit
// options.
func DiscoverRegistrarAutoViaDNSWithOptions(domain string, opts DiscoverOptions) ([]RegistrarCandidate, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	var lastErr error
	for _, stage := range registrarDiscoveryDNSServerStages() {
		servers := opts.DNSServers
		if len(servers) == 0 {
			servers = stage
		}
		if len(servers) == 0 {
			continue
		}
		cands, err := DiscoverRegistrarViaDNS(domain, servers, opts.Timeout)
		if err == nil && len(cands) > 0 {
			return cands, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("dns: no registrar found for %s", domain)
	}
	return nil, lastErr
}

// DiscoverRegistrarViaDNS performs the SRV/NAPTR lookup for the registrar.
func DiscoverRegistrarViaDNS(domain string, servers []string, timeout time.Duration) ([]RegistrarCandidate, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var out []RegistrarCandidate
	for _, transport := range registrarTransportCandidates() {
		srvName := fmt.Sprintf("_sip._%s.%s", transport, domain)
		cands, err := lookupSRV(srvName, servers, timeout)
		if err == nil && len(cands) > 0 {
			out = append(out, cands...)
		}
	}
	if len(out) == 0 {
		// Fall back to an A/AAAA lookup of the domain itself.
		ips, err := LookupHostIPViaDNSServers(domain, servers, timeout)
		if err == nil {
			for _, ip := range ips {
				out = append(out, RegistrarCandidate{Host: ip.String(), Port: 5060, Transport: "udp"})
			}
		}
	}
	return out, nil
}

// lookupSRV queries the SRV records for a name.
func lookupSRV(name string, servers []string, timeout time.Duration) ([]RegistrarCandidate, error) {
	m := new(dns.Msg)
	m.SetQuestion(name, dns.TypeSRV)
	m.RecursionDesired = true
	var out []RegistrarCandidate
	var lastErr error
	for _, server := range servers {
		c := &dns.Client{Timeout: timeout}
		resp, _, err := c.Exchange(m, server)
		if err != nil {
			lastErr = err
			continue
		}
		for _, rr := range resp.Answer {
			if srv, ok := rr.(*dns.SRV); ok {
				out = append(out, RegistrarCandidate{
					Host:      strings.TrimSuffix(srv.Target, "."),
					Port:      int(srv.Port),
					Transport: transportOf(name),
					Priority:  srv.Priority,
					Weight:    srv.Weight,
				})
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("dns: no SRV records for %s", name)
	}
	return out, lastErr
}

// transportOf extracts the transport from an SRV name like "_sip._udp.x".
func transportOf(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) >= 2 {
		return strings.TrimPrefix(parts[1], "_")
	}
	return "udp"
}

// LookupHostIPViaDNSServers resolves a host to IPs using the given servers.
func LookupHostIPViaDNSServers(host string, servers []string, timeout time.Duration) ([]net.IP, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(host), dns.TypeA)
	m.RecursionDesired = true
	var out []net.IP
	var lastErr error
	for _, server := range servers {
		c := &dns.Client{Timeout: timeout}
		resp, _, err := c.Exchange(m, server)
		if err != nil {
			lastErr = err
			continue
		}
		for _, rr := range resp.Answer {
			if a, ok := rr.(*dns.A); ok {
				out = append(out, a.A)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("dns: no A records for %s", host)
	}
	return out, lastErr
}

// ExpandRegistrarCandidates expands a candidate list, adding default ports and
// deduplicating.
func ExpandRegistrarCandidates(cands []RegistrarCandidate) []RegistrarCandidate {
	seen := make(map[string]bool)
	var out []RegistrarCandidate
	for _, c := range cands {
		if c.Port == 0 {
			switch c.Transport {
			case "tls":
				c.Port = 5061
			default:
				c.Port = 5060
			}
		}
		key := fmt.Sprintf("%s:%d:%s", c.Host, c.Port, c.Transport)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// parserRegistrarHostPort parses a "host:port" string into host and port.
func parserRegistrarHostPort(s string) (string, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, fmt.Errorf("dns: empty registrar")
	}
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		// No port: default to 5060.
		return s, 5060, nil
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, fmt.Errorf("dns: bad port %q", portStr)
	}
	return host, port, nil
}

// LookupHostIP is a convenience wrapper using the system resolver.
func LookupHostIP(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return ips, nil
}

var _ = os.Getenv
