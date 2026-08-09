package runtimecore

import (
	"context"
	"errors"
	"net"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/netstack"
)

const defaultIMSMTU = 1400

func NewUserspaceIMSNetwork(
	ctx context.Context,
	session *swu.Session,
	snapshot swu.SessionSnapshot,
) (*netstack.Network, error) {
	if session == nil || session.InnerPacketEndpoint() == nil {
		return nil, errors.New("userspace netstack: inner packet endpoint is nil")
	}
	if snapshot.IPv4 == nil && snapshot.IPv6 == nil {
		return nil, errors.New("userspace netstack: snapshot has no inner IP")
	}
	prefix := snapshot.IPv6Prefix
	if prefix == 0 {
		prefix = 64
	}
	mtu := int(snapshot.MTU)
	if mtu == 0 {
		mtu = defaultIMSMTU
	}
	var dns []net.IP
	if snapshot.IPv4 != nil {
		dns = append(cloneIPs(snapshot.DNSv4), snapshot.DNSv6...)
	} else {
		dns = append(cloneIPs(snapshot.DNSv6), snapshot.DNSv4...)
	}
	return netstack.NewNetwork(
		ctx, cloneIP(snapshot.IPv4), cloneIP(snapshot.IPv6), prefix, mtu,
		session.InnerPacketEndpoint(), nil, dns,
	)
}

func snapshotLocalAddress(snapshot swu.SessionSnapshot) string {
	if snapshot.IPv4 != nil {
		return snapshot.IPv4.String()
	}
	return snapshot.IPv6.String()
}

func cloneIP(value net.IP) net.IP { return append(net.IP(nil), value...) }

func cloneIPs(values []net.IP) []net.IP {
	result := make([]net.IP, 0, len(values))
	for _, value := range values {
		result = append(result, cloneIP(value))
	}
	return result
}
