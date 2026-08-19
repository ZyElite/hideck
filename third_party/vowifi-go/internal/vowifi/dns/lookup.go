package dns

import (
	"context"
	"fmt"
	"net"
	"strings"

	miekgdns "github.com/miekg/dns"
)

type queryResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
	LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error)
	LookupNAPTR(context.Context, string) ([]*miekgdns.NAPTR, error)
}

type resolverFactory func(bindIP, server net.IP) queryResolver

type serverResolver struct {
	native  *net.Resolver
	client  *miekgdns.Client
	address string
}

func newServerResolver(bindIP, server net.IP) queryResolver {
	return newServerResolverAt(bindIP, server, defaultDNSPort)
}

func newServerResolverAt(bindIP, server net.IP, port string) queryResolver {
	address := net.JoinHostPort(server.String(), port)
	return newServerResolverForAddress(bindIP, address)
}

func newServerResolverForAddress(bindIP net.IP, address string) queryResolver {
	dialer := &net.Dialer{}
	if bindIP != nil {
		dialer.LocalAddr = &net.UDPAddr{IP: bindIP}
	}
	dialDNS := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "udp", address)
	}
	return &serverResolver{
		native:  &net.Resolver{PreferGo: true, Dial: dialDNS},
		client:  &miekgdns.Client{Net: "udp", Dialer: dialer, Timeout: registrarQueryTimeout},
		address: address,
	}
}

func (r *serverResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.native.LookupIPAddr(ctx, host)
}

func (r *serverResolver) LookupSRV(
	ctx context.Context,
	service, proto, name string,
) (string, []*net.SRV, error) {
	return r.native.LookupSRV(ctx, service, proto, name)
}

func (r *serverResolver) LookupNAPTR(ctx context.Context, name string) ([]*miekgdns.NAPTR, error) {
	message := new(miekgdns.Msg)
	message.SetQuestion(miekgdns.Fqdn(name), miekgdns.TypeNAPTR)
	response, _, err := r.client.ExchangeContext(ctx, message, r.address)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("dns: empty NAPTR response from %s", r.address)
	}
	records := make([]*miekgdns.NAPTR, 0, len(response.Answer))
	for _, answer := range response.Answer {
		if record, ok := answer.(*miekgdns.NAPTR); ok {
			records = append(records, record)
		}
	}
	return records, nil
}

// LookupHostIPViaDNSServers resolves both A and AAAA records through explicit servers.
func LookupHostIPViaDNSServers(
	ctx context.Context,
	host string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
) []net.IP {
	return lookupHostIPViaDNSServers(ctx, host, preferIPv6, bindIP, servers, newServerResolver)
}

func lookupHostIPViaDNSServers(
	ctx context.Context,
	host string,
	preferIPv6 bool,
	bindIP net.IP,
	servers []net.IP,
	factory resolverFactory,
) []net.IP {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" || len(servers) == 0 {
		return nil
	}
	orderedServers := OrderDNSServersByPreference(servers, preferIPv6)
	preferred, fallback := make([]net.IP, 0, 4), make([]net.IP, 0, 4)
	seen := make(map[string]struct{})
	for _, server := range orderedServers {
		if !serverUsableForBind(server, bindIP) {
			continue
		}
		addresses, err := factory(bindIP, server).LookupIPAddr(ctx, host)
		if err != nil {
			continue
		}
		preferred, fallback = collectResolvedIPs(addresses, preferIPv6, seen, preferred, fallback)
		if len(preferred) > 0 {
			break
		}
	}
	return append(preferred, fallback...)
}

func collectResolvedIPs(
	addresses []net.IPAddr,
	preferIPv6 bool,
	seen map[string]struct{},
	preferred, fallback []net.IP,
) ([]net.IP, []net.IP) {
	for _, address := range addresses {
		if address.IP == nil {
			continue
		}
		key := address.IP.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if (address.IP.To4() == nil) == preferIPv6 {
			preferred = append(preferred, address.IP)
		} else {
			fallback = append(fallback, address.IP)
		}
	}
	return preferred, fallback
}

func serverUsableForBind(server, bindIP net.IP) bool {
	return server != nil && (bindIP == nil || (server.To4() != nil) == (bindIP.To4() != nil))
}

// OrderDNSServersByPreference puts the requested address family first.
func OrderDNSServersByPreference(servers []net.IP, preferIPv6 bool) []net.IP {
	preferred := make([]net.IP, 0, len(servers))
	fallback := make([]net.IP, 0, len(servers))
	for _, server := range servers {
		if server == nil {
			continue
		}
		if (server.To4() == nil) == preferIPv6 {
			preferred = append(preferred, server)
		} else {
			fallback = append(fallback, server)
		}
	}
	return append(preferred, fallback...)
}
