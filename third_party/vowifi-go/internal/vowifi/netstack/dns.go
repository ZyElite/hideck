package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

const dnsQueryTimeout = 2 * time.Second

func (n *Network) ResolveIP(ctx context.Context, host string, preferIPv6 bool) (net.IP, error) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	if len(n.dnsServers()) > 0 {
		if ip, err := n.resolveViaDNSServers(ctx, host, preferIPv6); err == nil {
			return ip, nil
		}
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	return selectAddress(addresses, host, preferIPv6)
}

func selectAddress(addresses []net.IPAddr, host string, preferIPv6 bool) (net.IP, error) {
	if preferIPv6 {
		for _, address := range addresses {
			if address.IP != nil && address.IP.To4() == nil {
				return address.IP, nil
			}
		}
	}
	for _, address := range addresses {
		if address.IP.To4() != nil {
			return address.IP, nil
		}
	}
	if len(addresses) > 0 {
		return addresses[0].IP, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host}
}

func (n *Network) resolveViaDNSServers(
	ctx context.Context,
	host string,
	preferIPv6 bool,
) (net.IP, error) {
	queryTypes := []uint16{mdns.TypeA, mdns.TypeAAAA}
	if preferIPv6 {
		queryTypes[0], queryTypes[1] = queryTypes[1], queryTypes[0]
	}
	var lastErr error
	for _, queryType := range queryTypes {
		for _, server := range n.dnsServers() {
			ip, err := n.queryDNS(ctx, server, host, queryType)
			if err == nil && ip != nil {
				return ip, nil
			}
			if err != nil {
				lastErr = err
			}
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("netstack: no DNS answer for %s", host)
}

func (n *Network) queryDNS(
	ctx context.Context,
	server net.IP,
	host string,
	queryType uint16,
) (net.IP, error) {
	query := new(mdns.Msg)
	query.SetQuestion(mdns.Fqdn(host), queryType)
	response, err := n.exchangeDNS(ctx, server, query)
	if err != nil {
		return nil, err
	}
	for _, answer := range response.Answer {
		switch record := answer.(type) {
		case *mdns.A:
			if queryType == mdns.TypeA && record.A != nil {
				return append(net.IP(nil), record.A...), nil
			}
		case *mdns.AAAA:
			if queryType == mdns.TypeAAAA && record.AAAA != nil {
				return append(net.IP(nil), record.AAAA...), nil
			}
		}
	}
	return nil, fmt.Errorf("netstack: empty DNS answer for %s", host)
}

func (n *Network) exchangeDNS(
	ctx context.Context,
	server net.IP,
	query *mdns.Msg,
) (*mdns.Msg, error) {
	payload, err := query.Pack()
	if err != nil {
		return nil, err
	}
	timeout := contextTimeout(ctx, dnsQueryTimeout)
	connection, err := n.DialContext(ctx, "udp", nil, net.JoinHostPort(server.String(), "53"), imscore.DialOptions{Timeout: int64(timeout)})
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if _, err := connection.Write(payload); err != nil {
		return nil, err
	}
	responsePayload := make([]byte, 4096)
	count, err := connection.Read(responsePayload)
	if err != nil {
		return nil, err
	}
	response := new(mdns.Msg)
	if err := response.Unpack(responsePayload[:count]); err != nil {
		return nil, err
	}
	return response, nil
}

func contextTimeout(ctx context.Context, maximum time.Duration) time.Duration {
	if ctx == nil {
		return maximum
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return maximum
	}
	remaining := time.Until(deadline)
	if remaining > 0 && remaining < maximum {
		return remaining
	}
	return maximum
}

func (n *Network) dnsServers() []net.IP {
	if n == nil {
		return nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return cloneIPs(n.dns)
}

// LookupSRV retains current registrar discovery on the restored SWu DNS path.
func (n *Network) LookupSRV(ctx context.Context, service, proto, name string) (string, uint16, error) {
	var lastErr error
	for _, server := range n.dnsServers() {
		_, records, err := n.resolver(server).LookupSRV(ctx, service, proto, name)
		if err == nil && len(records) > 0 {
			return strings.TrimSuffix(records[0].Target, "."), records[0].Port, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("netstack: empty SRV answer")
		}
	}
	if lastErr == nil {
		lastErr = errors.New("netstack: no DNS servers assigned by ePDG")
	}
	return "", 0, lastErr
}

func (n *Network) resolver(server net.IP) *net.Resolver {
	serverAddress := net.JoinHostPort(server.String(), "53")
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return n.DialContext(
			ctx, network, nil, serverAddress,
			imscore.DialOptions{Timeout: int64(dnsQueryTimeout)},
		)
	}}
}
