// Package netstack provides the user-space network surface used by IMS.
package netstack

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
)

// Network owns a gVisor stack connected to the SWu inner-packet path.
type Network struct {
	mu        sync.RWMutex
	innerIP   net.IP
	innerIPv6 net.IP
	prefixLen int
	dns       []string
	backend   *gvisorNetwork
	initErr   error
	stats     networkStats
}

// PacketIO carries raw inner IP packets between gVisor and SWu.
type PacketIO interface {
	ReadPacketContext(context.Context) ([]byte, error)
	WritePacketContext(context.Context, []byte) error
}

type NetworkStats struct {
	PacketsIn  uint64
	PacketsOut uint64
	BytesIn    uint64
	BytesOut   uint64
}

type networkStats struct {
	PacketsIn  atomic.Uint64
	PacketsOut atomic.Uint64
	BytesIn    atomic.Uint64
	BytesOut   atomic.Uint64
}

// NewNetwork preserves the constructor API while exposing the missing packet
// path as an explicit error on first network use.
func NewNetwork(innerIP net.IP, prefixLen int, dns []string) *Network {
	return &Network{
		innerIP: append(net.IP(nil), innerIP...), prefixLen: prefixLen, dns: append([]string(nil), dns...),
		initErr: errors.New("netstack: SWu packet IO is required"),
	}
}

// NewTunnelNetwork creates a real user-space stack connected to SWu.
func NewTunnelNetwork(innerIP net.IP, prefixLen int, dns []string, packetIO PacketIO) (*Network, error) {
	network := &Network{
		innerIP: append(net.IP(nil), innerIP...), prefixLen: prefixLen, dns: append([]string(nil), dns...),
	}
	backend, err := newGVisorNetwork(network.innerIP, prefixLen, network.dns, packetIO, &network.stats)
	if err != nil {
		return nil, err
	}
	network.backend = backend
	return network, nil
}

func (n *Network) LocalIP() net.IP {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append(net.IP(nil), n.innerIP...)
}

func (n *Network) HasLocalIP(ip net.IP) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if ip == nil {
		return false
	}
	return n.innerIP != nil && n.innerIP.Equal(ip) || n.innerIPv6 != nil && n.innerIPv6.Equal(ip)
}

func (n *Network) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	if err := n.ready(); err != nil {
		return nil, err
	}
	return n.backend.ResolveIP(ctx, host)
}

// LookupSRV resolves a service through DNS assigned by the ePDG.
func (n *Network) LookupSRV(ctx context.Context, service, proto, name string) (string, uint16, error) {
	if err := n.ready(); err != nil {
		return "", 0, err
	}
	return n.backend.LookupSRV(ctx, service, proto, name)
}

func (n *Network) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if err := n.ready(); err != nil {
		return nil, err
	}
	return n.backend.DialContext(ctx, network, address)
}

func (n *Network) ListenTCP(address *net.TCPAddr) (net.Listener, error) {
	if err := n.ready(); err != nil {
		return nil, err
	}
	return n.backend.ListenTCP(address)
}

func (n *Network) ListenPacket(network string, address *net.UDPAddr) (net.PacketConn, error) {
	if err := n.ready(); err != nil {
		return nil, err
	}
	return n.backend.ListenPacket(network, address)
}

func (n *Network) InstallIPSec3GPP() error { return nil }

func (n *Network) IPSec3GPPPolicyInstalled() bool { return true }

func (n *Network) Stats() NetworkStats {
	return NetworkStats{
		PacketsIn: n.stats.PacketsIn.Load(), PacketsOut: n.stats.PacketsOut.Load(),
		BytesIn: n.stats.BytesIn.Load(), BytesOut: n.stats.BytesOut.Load(),
	}
}

func (n *Network) Close() error {
	if n == nil || n.backend == nil {
		return nil
	}
	n.backend.Close()
	return nil
}

func (n *Network) ready() error {
	if n == nil {
		return errors.New("netstack: nil network")
	}
	if n.initErr != nil {
		return n.initErr
	}
	if n.backend == nil {
		return errors.New("netstack: network not initialized")
	}
	return n.backend.ready()
}
