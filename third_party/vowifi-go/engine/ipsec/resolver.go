package ipsec

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

const dnsLookupTimeout = 3 * time.Second

// ResolveUDPAddrAll resolves the legacy host:port input and returns both the
// preferred endpoint and every distinct address for IKE failover.
func ResolveUDPAddrAll(addr, dnsServer string) (*net.UDPAddr, []net.IP, error) {
	host, portName, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return nil, nil, err
	}
	port, err := net.LookupPort("udp", portName)
	if err != nil {
		return nil, nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		ip = ipv4Compat(ip)
		return &net.UDPAddr{IP: ip, Port: port}, []net.IP{ip}, nil
	}
	ips, err := lookupIPCandidates(host, dnsServer)
	if err != nil {
		return nil, nil, err
	}
	preferred := preferIPv4(ips)
	return &net.UDPAddr{IP: preferred, Port: port}, ips, nil
}

func lookupIPCandidates(host, dnsServer string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()
	resolver, err := resolverForServer(dnsServer)
	if err != nil {
		return nil, err
	}
	resolved, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(resolved))
	ips := make([]net.IP, 0, len(resolved))
	for _, candidate := range resolved {
		ip := ipv4Compat(candidate.IP)
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, append(net.IP(nil), ip...))
	}
	if len(ips) == 0 {
		return nil, errors.New("DNS returned no usable IP addresses")
	}
	return ips, nil
}

func resolverForServer(server string) (*net.Resolver, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return net.DefaultResolver, nil
	}
	if _, _, err := net.SplitHostPort(server); err != nil {
		if net.ParseIP(server) == nil {
			return nil, errors.New("invalid DNS server address")
		}
		server = net.JoinHostPort(server, "53")
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", server)
		},
	}, nil
}

func preferIPv4(ips []net.IP) net.IP {
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip
		}
	}
	return ips[0]
}

func ipv4Compat(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}
