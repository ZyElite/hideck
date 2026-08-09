// Package netstack provides the user-space network surface used by IMS.
package netstack

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/loopback"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/internal/vowifi/ipsec3gpp"
)

const (
	imsNICID        tcpip.NICID = 1
	loopbackNICID   tcpip.NICID = 2
	packetQueueSize             = 1024
	defaultMTU                  = 1400
)

// Network owns a dual-stack gVisor stack connected to an SWu endpoint.
type Network struct {
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	stack  *stack.Stack
	link   *channel.Endpoint
	nicID  int32
	ipv4   net.IP
	ipv6   net.IP
	dns    []net.IP
	bridge *PacketBridge

	outboundPackets      atomic.Uint64
	inboundPackets       atomic.Uint64
	ipsecPolicyInstalled atomic.Bool
	outboundBytes        atomic.Uint64
	inboundBytes         atomic.Uint64
}

// PacketIO is the additive packet boundary retained for current host callers.
type PacketIO interface {
	ReadPacketContext(context.Context) ([]byte, error)
	WritePacketContext(context.Context, []byte) error
}

// NewNetwork constructs the original gVisor network and starts its packet
// bridge when endpoint is non-nil.
func NewNetwork(
	ctx context.Context,
	ipv4Address net.IP,
	ipv6Address net.IP,
	prefixLen int,
	mtu int,
	endpoint swu.InnerPacketEndpoint,
	transformer *ipsec3gpp.Transport,
	dnsServers []net.IP,
) (*Network, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if mtu == 0 {
		mtu = defaultMTU
	}
	if ipv4Address == nil && ipv6Address == nil {
		return nil, errors.New("netstack: at least one local IP is required")
	}
	ipv4Address = normalizeIPv4(ipv4Address)
	ipv6Address = normalizeIPv6(ipv6Address)

	networkCtx, cancel := context.WithCancel(ctx)
	networkStack := newStack()
	link := channel.New(packetQueueSize, uint32(mtu), "")
	if err := createNICs(networkStack, link); err != nil {
		cancel()
		networkStack.Close()
		return nil, err
	}
	n := &Network{
		ctx: networkCtx, cancel: cancel, stack: networkStack, link: link,
		nicID: int32(imsNICID), ipv4: ipv4Address, ipv6: ipv6Address,
		dns: cloneIPs(dnsServers),
	}
	if err := n.configureAddresses(prefixLen); err != nil {
		_ = n.Close()
		return nil, err
	}
	n.configureRoutes()
	if endpoint != nil {
		var packetTransformer PacketTransformer
		if transformer != nil {
			packetTransformer = transformer
		}
		n.bridge = NewPacketBridge(networkCtx, link, endpoint, packetTransformer, n)
	}
	return n, nil
}

func newStack() *stack.Stack {
	return stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: transportProtocols(),
	})
}

func createNICs(networkStack *stack.Stack, link *channel.Endpoint) error {
	if err := networkStack.CreateNIC(imsNICID, link); err != nil {
		return tcpipError(err)
	}
	if err := networkStack.CreateNIC(loopbackNICID, loopback.New()); err != nil {
		return tcpipError(err)
	}
	return nil
}

func normalizeIPv4(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if normalized := ip.To4(); normalized != nil {
		return append(net.IP(nil), normalized...)
	}
	return nil
}

func normalizeIPv6(ip net.IP) net.IP {
	if ip == nil || ip.To4() != nil {
		return nil
	}
	if normalized := ip.To16(); normalized != nil {
		return append(net.IP(nil), normalized...)
	}
	return nil
}

func cloneIPs(ips []net.IP) []net.IP {
	cloned := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip != nil {
			cloned = append(cloned, append(net.IP(nil), ip...))
		}
	}
	return cloned
}

// LocalIP returns IPv4 first, matching the original preference.
func (n *Network) LocalIP() net.IP {
	if n == nil {
		return nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.ipv4 != nil {
		return append(net.IP(nil), n.ipv4...)
	}
	return append(net.IP(nil), n.ipv6...)
}

func (n *Network) HasLocalIP(ip net.IP) bool {
	if n == nil || ip == nil {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.ipv4 != nil && n.ipv4.Equal(ip) || n.ipv6 != nil && n.ipv6.Equal(ip)
}

func (n *Network) IPSec3GPPPolicyInstalled() bool {
	return n != nil && n.ipsecPolicyInstalled.Load()
}

func (n *Network) Stats() Stats {
	if n == nil {
		return Stats{}
	}
	outboundPackets := n.outboundPackets.Load()
	inboundPackets := n.inboundPackets.Load()
	stats := Stats{
		OutboundPackets: outboundPackets,
		InboundPackets:  inboundPackets,
		PacketsOut:      outboundPackets,
		PacketsIn:       inboundPackets,
		BytesOut:        n.outboundBytes.Load(),
		BytesIn:         n.inboundBytes.Load(),
	}
	if n.bridge != nil {
		stats.Bridge = n.bridge.Stats()
	}
	return stats
}

func (n *Network) Close() error {
	if n == nil {
		return nil
	}
	n.cancel()
	if n.bridge != nil {
		n.bridge.Close()
	}
	if n.link != nil {
		n.link.Close()
	}
	if n.stack != nil {
		n.stack.Close()
	}
	return nil
}
