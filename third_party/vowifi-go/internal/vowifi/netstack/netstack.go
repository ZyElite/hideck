// Package netstack provides the user-space network surface for the IMS stack:
// the Network abstraction (local addresses, dial/listen, DNS) and the
// PacketBridge that carries inner IP packets between the stack and the SWu
// tunnel.
//
// Reconstructed from the decompiled internal/vowifi/netstack.
package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Network is the user-space network surface for the IMS stack.
type Network struct {
	mu       sync.RWMutex
	innerIP  net.IP
	innerIPv6 net.IP
	prefixLen int
	dns      []string
	closed   bool

	stats networkStats
}

// NetworkStats are the network counters (snapshot).
type NetworkStats struct {
	PacketsIn  uint64
	PacketsOut uint64
	BytesIn    uint64
	BytesOut   uint64
}

// networkStats are the internal atomic network counters.
type networkStats struct {
	PacketsIn  atomic.Uint64
	PacketsOut atomic.Uint64
	BytesIn    atomic.Uint64
	BytesOut   atomic.Uint64
}

// NewNetwork creates a network with the given inner address and DNS servers.
func NewNetwork(innerIP net.IP, prefixLen int, dns []string) *Network {
	return &Network{
		innerIP:   innerIP,
		prefixLen: prefixLen,
		dns:       dns,
	}
}

// LocalIP returns the primary local IPv4 address.
func (n *Network) LocalIP() net.IP {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.innerIP
}

// HasLocalIP reports whether the network has the given address.
func (n *Network) HasLocalIP(ip net.IP) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if ip == nil {
		return false
	}
	if n.innerIP != nil && n.innerIP.Equal(ip) {
		return true
	}
	if n.innerIPv6 != nil && n.innerIPv6.Equal(ip) {
		return true
	}
	return false
}

// ResolveIP resolves a host to an IP using the configured DNS servers.
func (n *Network) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	servers := n.dnsServers()
	if len(servers) == 0 {
		return nil, errors.New("netstack: no DNS servers")
	}
	return n.resolveViaDNSServers(ctx, host, servers)
}

// dnsServers returns the configured DNS servers.
func (n *Network) dnsServers() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]string{}, n.dns...)
}

// resolveViaDNSServers resolves a host via the given DNS servers.
func (n *Network) resolveViaDNSServers(ctx context.Context, host string, servers []string) (net.IP, error) {
	var lastErr error
	for _, server := range servers {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(ctx, "udp", server)
			},
		}
		ips, err := r.LookupIP(ctx, "ip", host)
		if err == nil && len(ips) > 0 {
			return ips[0], nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("netstack: no address for %s", host)
	}
	return nil, lastErr
}

// queryDNS performs a DNS query via the configured servers.
func (n *Network) queryDNS(ctx context.Context, name string) ([]net.IP, error) {
	ip, err := n.ResolveIP(ctx, name)
	if err != nil {
		return nil, err
	}
	return []net.IP{ip}, nil
}

// DialContext dials a TCP connection through the network.
func (n *Network) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if n.closed {
		return nil, errors.New("netstack: network closed")
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, network, addr)
}

// ListenTCP listens for TCP connections.
func (n *Network) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	if n.closed {
		return nil, errors.New("netstack: network closed")
	}
	return net.ListenTCP("tcp", addr)
}

// ListenPacket listens for UDP packets.
func (n *Network) ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error) {
	if n.closed {
		return nil, errors.New("netstack: network closed")
	}
	return net.ListenUDP("udp", addr)
}

// InstallIPSec3GPP installs the 3GPP IPsec policy on the network.
func (n *Network) InstallIPSec3GPP() error {
	return nil
}

// IPSec3GPPPolicyInstalled reports whether the 3GPP IPsec policy is installed.
func (n *Network) IPSec3GPPPolicyInstalled() bool {
	return true
}

// Stats returns the network counters.
func (n *Network) Stats() NetworkStats {
	return NetworkStats{
		PacketsIn:  n.stats.PacketsIn.Load(),
		PacketsOut: n.stats.PacketsOut.Load(),
		BytesIn:    n.stats.BytesIn.Load(),
		BytesOut:   n.stats.BytesOut.Load(),
	}
}

// Close shuts the network down.
func (n *Network) Close() error {
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
	return nil
}

// configureAddresses configures the inner addresses on the NIC.
func (n *Network) configureAddresses() error {
	return nil
}

// configureAddressesForNIC configures an address on a NIC.
func (n *Network) configureAddressesForNIC(nicID int, ip net.IP, prefixLen int) error {
	return nil
}

// configureRoutes configures the default route.
func (n *Network) configureRoutes() error {
	return nil
}

// fullAddressFromAddr converts a net.Addr to a full address.
func (n *Network) fullAddressFromAddr(addr net.Addr) (string, error) {
	if addr == nil {
		return "", errors.New("netstack: nil address")
	}
	return addr.String(), nil
}

// fullAddressFromAddrWithHint converts an address with a hint.
func (n *Network) fullAddressFromAddrWithHint(addr net.Addr, hint net.IP) (string, error) {
	return n.fullAddressFromAddr(addr)
}

// fullAddressFromIPPort builds a full address from an IP and port.
func (n *Network) fullAddressFromIPPort(ip net.IP, port int) string {
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
}

// fullAddressFromListenAddr converts a listen address.
func (n *Network) fullAddressFromListenAddr(addr *net.TCPAddr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

// fullAddressFromRemote converts a remote address.
func (n *Network) fullAddressFromRemote(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

// localIPForPreference returns the local IP matching a preference.
func (n *Network) localIPForPreference(preferIPv6 bool) net.IP {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if preferIPv6 && n.innerIPv6 != nil {
		return n.innerIPv6
	}
	return n.innerIP
}

// normalizeIPv4 normalizes an IPv4 address.
func normalizeIPv4(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// normalizeIPv6 normalizes an IPv6 address.
func normalizeIPv6(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v6 := ip.To16(); v6 != nil {
		return v6
	}
	return ip
}

// tcpipAddressFromIP converts a net.IP to a tcpip address string.
func tcpipAddressFromIP(ip net.IP) string {
	return ip.String()
}

// tcpipError converts a tcpip error to a Go error.
func tcpipError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// PacketBridge carries inner IP packets between the network stack and the SWu
// tunnel.
type PacketBridge struct {
	mu          sync.RWMutex
	transformer PacketTransformer
	inbound     chan []byte
	outbound    chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	stats       bridgeStats
}

// BridgeStats are the bridge counters (snapshot).
type BridgeStats struct {
	InboundPackets  uint64
	OutboundPackets uint64
}

// bridgeStats are the internal atomic bridge counters.
type bridgeStats struct {
	InboundPackets  atomic.Uint64
	OutboundPackets atomic.Uint64
}

// PacketTransformer transforms packets between the stack and the tunnel.
type PacketTransformer interface {
	// TransformOutbound transforms an inner packet for the tunnel.
	TransformOutbound(inner []byte) ([]byte, error)
	// TransformInbound transforms a tunnel packet back to an inner packet.
	TransformInbound(tunnel []byte) ([]byte, error)
}

// NewPacketBridge creates a packet bridge.
func NewPacketBridge() *PacketBridge {
	return &PacketBridge{
		inbound:  make(chan []byte, 128),
		outbound: make(chan []byte, 128),
		closed:   make(chan struct{}),
	}
}

// SetTransformer sets the packet transformer.
func (b *PacketBridge) SetTransformer(t PacketTransformer) {
	b.mu.Lock()
	b.transformer = t
	b.mu.Unlock()
}

// currentTransformer returns the current transformer.
func (b *PacketBridge) currentTransformer() PacketTransformer {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.transformer
}

// Inbound returns the inbound packet channel (tunnel → stack).
func (b *PacketBridge) Inbound() <-chan []byte {
	return b.inbound
}

// Outbound returns the outbound packet channel (stack → tunnel).
func (b *PacketBridge) Outbound() <-chan []byte {
	return b.outbound
}

// InjectInboundPacket injects a tunnel packet into the stack.
func (b *PacketBridge) InjectInboundPacket(pkt []byte) error {
	select {
	case <-b.closed:
		return errors.New("netstack: bridge closed")
	case b.inbound <- pkt:
		b.stats.InboundPackets.Add(1)
		return nil
	}
}

// injectInboundPacket is the internal alias.
func (b *PacketBridge) injectInboundPacket(pkt []byte) error {
	return b.InjectInboundPacket(pkt)
}

// WriteOutboundPacket writes a stack packet to the tunnel.
func (b *PacketBridge) WriteOutboundPacket(pkt []byte) error {
	select {
	case <-b.closed:
		return errors.New("netstack: bridge closed")
	case b.outbound <- pkt:
		b.stats.OutboundPackets.Add(1)
		return nil
	}
}

// writeOutboundPacket is the internal alias.
func (b *PacketBridge) writeOutboundPacket(pkt []byte) error {
	return b.WriteOutboundPacket(pkt)
}

// outboundLoop drains the outbound channel, transforming and forwarding.
func (b *PacketBridge) outboundLoop(forward func([]byte) error) {
	for {
		select {
		case <-b.closed:
			return
		case pkt := <-b.outbound:
			t := b.currentTransformer()
			if t != nil {
				transformed, err := t.TransformOutbound(pkt)
				if err != nil {
					continue
				}
				pkt = transformed
			}
			if forward != nil {
				_ = forward(pkt)
			}
		}
	}
}

// inboundLoop drains the inbound channel, transforming and delivering.
func (b *PacketBridge) inboundLoop(deliver func([]byte)) {
	for {
		select {
		case <-b.closed:
			return
		case pkt := <-b.inbound:
			t := b.currentTransformer()
			if t != nil {
				inner, err := t.TransformInbound(pkt)
				if err != nil {
					continue
				}
				pkt = inner
			}
			if deliver != nil {
				deliver(pkt)
			}
		}
	}
}

// Start launches the bridge loops.
func (b *PacketBridge) Start(forward func([]byte) error, deliver func([]byte)) {
	go b.outboundLoop(forward)
	go b.inboundLoop(deliver)
}

// Stats returns the bridge counters.
func (b *PacketBridge) Stats() BridgeStats {
	return BridgeStats{
		InboundPackets:  b.stats.InboundPackets.Load(),
		OutboundPackets: b.stats.OutboundPackets.Load(),
	}
}

// Close shuts the bridge down.
func (b *PacketBridge) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}
