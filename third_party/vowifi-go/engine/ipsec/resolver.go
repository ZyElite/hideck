package ipsec

import (
	"context"
	"net"
	"strings"

	vowifidns "github.com/iniwex5/vowifi-go/internal/vowifi/dns"
)

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
	ips, err := vowifidns.LookupHostIPStaged(context.Background(), host, dnsServer)
	if err != nil {
		return nil, nil, err
	}
	preferred := preferIPv4(ips)
	return &net.UDPAddr{IP: preferred, Port: port}, ips, nil
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
