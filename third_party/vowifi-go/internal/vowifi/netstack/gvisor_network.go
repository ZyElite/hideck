package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const (
	imsNICID        tcpip.NICID = 1
	imsMTU                      = 1400
	packetQueueSize             = 1024
)

type gvisorNetwork struct {
	stack       *stack.Stack
	link        *channel.Endpoint
	protocol    tcpip.NetworkProtocolNumber
	packetIO    PacketIO
	dns         []string
	stats       *networkStats
	cancel      context.CancelFunc
	done        sync.WaitGroup
	mu          sync.RWMutex
	closed      bool
	transformer PacketTransformer
	terminalErr error
}

func newGVisorNetwork(innerIP net.IP, prefixLen int, dns []string, packetIO PacketIO, stats *networkStats) (*gvisorNetwork, error) {
	if packetIO == nil {
		return nil, errors.New("netstack: SWu packet IO is required")
	}
	address, protocol, route, prefixLen, err := localNetworkConfig(innerIP, prefixLen)
	if err != nil {
		return nil, err
	}
	g := &gvisorNetwork{protocol: protocol, packetIO: packetIO, dns: append([]string(nil), dns...), stats: stats}
	g.stack = stack.New(stack.Options{
		NetworkProtocols: []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{
			tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6,
		},
	})
	g.link = channel.New(packetQueueSize, imsMTU, "")
	if err := g.stack.CreateNIC(imsNICID, g.link); err != nil {
		g.stack.Close()
		return nil, gvisorError("create IMS NIC", err)
	}
	protocolAddress := tcpip.ProtocolAddress{
		Protocol: protocol, AddressWithPrefix: tcpip.AddressWithPrefix{Address: address, PrefixLen: prefixLen},
	}
	if err := g.stack.AddProtocolAddress(imsNICID, protocolAddress, stack.AddressProperties{}); err != nil {
		g.stack.Close()
		return nil, gvisorError("add IMS address", err)
	}
	g.stack.SetRouteTable([]tcpip.Route{{Destination: route, NIC: imsNICID}})
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.done.Add(2)
	go g.outboundLoop(ctx)
	go g.inboundLoop(ctx)
	return g, nil
}

func localNetworkConfig(innerIP net.IP, prefixLen int) (tcpip.Address, tcpip.NetworkProtocolNumber, tcpip.Subnet, int, error) {
	if ipv4Address := innerIP.To4(); ipv4Address != nil {
		if prefixLen <= 0 || prefixLen > net.IPv4len*8 {
			prefixLen = net.IPv4len * 8
		}
		return tcpip.AddrFrom4Slice(ipv4Address), ipv4.ProtocolNumber, header.IPv4EmptySubnet, prefixLen, nil
	}
	if ipv6Address := innerIP.To16(); ipv6Address != nil {
		if prefixLen <= 0 || prefixLen > net.IPv6len*8 {
			prefixLen = net.IPv6len * 8
		}
		return tcpip.AddrFrom16Slice(ipv6Address), ipv6.ProtocolNumber, header.IPv6EmptySubnet, prefixLen, nil
	}
	return tcpip.Address{}, 0, tcpip.Subnet{}, 0, errors.New("netstack: negotiated IP address is required")
}

func (g *gvisorNetwork) ready() error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.terminalErr != nil {
		return g.terminalErr
	}
	if g.closed {
		return errors.New("netstack: network closed")
	}
	return nil
}

func (g *gvisorNetwork) setTransformer(transformer PacketTransformer) {
	g.mu.Lock()
	g.transformer = transformer
	g.mu.Unlock()
}

func (g *gvisorNetwork) transformOutbound(packet []byte) ([]byte, error) {
	g.mu.RLock()
	transformer := g.transformer
	g.mu.RUnlock()
	if transformer == nil {
		return packet, nil
	}
	return transformer.TransformOutbound(packet)
}

func (g *gvisorNetwork) transformInbound(packet []byte) ([]byte, error) {
	g.mu.RLock()
	transformer := g.transformer
	g.mu.RUnlock()
	if transformer == nil {
		return packet, nil
	}
	return transformer.TransformInbound(packet)
}

func (g *gvisorNetwork) fail(operation string, err error) {
	g.mu.Lock()
	if g.terminalErr == nil {
		g.terminalErr = fmt.Errorf("netstack: %s: %w", operation, err)
	}
	g.mu.Unlock()
	g.cancel()
}

func (g *gvisorNetwork) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	remote, protocol, err := g.resolveAddress(ctx, addr)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.ToLower(network), "udp") {
		return gonet.DialUDP(g.stack, nil, &remote, protocol)
	}
	if strings.HasPrefix(strings.ToLower(network), "tcp") {
		return gonet.DialContextTCP(ctx, g.stack, remote, protocol)
	}
	return nil, fmt.Errorf("netstack: unsupported network %q", network)
}

func (g *gvisorNetwork) DialTCPContext(ctx context.Context, local, remote *net.TCPAddr) (net.Conn, error) {
	localAddress, localProtocol, err := fullAddress(local.IP, local.Port)
	if err != nil {
		return nil, err
	}
	remoteAddress, remoteProtocol, err := fullAddress(remote.IP, remote.Port)
	if err != nil {
		return nil, err
	}
	if localProtocol != remoteProtocol {
		return nil, errors.New("netstack: TCP local and remote address families differ")
	}
	return gonet.DialTCPWithBind(ctx, g.stack, localAddress, remoteAddress, localProtocol)
}

func (g *gvisorNetwork) ListenTCP(addr *net.TCPAddr) (net.Listener, error) {
	full, protocol, err := fullAddress(addr.IP, addr.Port)
	if err != nil {
		return nil, err
	}
	return gonet.ListenTCP(g.stack, full, protocol)
}

func (g *gvisorNetwork) ListenPacket(network string, addr *net.UDPAddr) (net.PacketConn, error) {
	if !strings.HasPrefix(strings.ToLower(network), "udp") {
		return nil, fmt.Errorf("netstack: unsupported packet network %q", network)
	}
	full, protocol, err := fullAddress(addr.IP, addr.Port)
	if err != nil {
		return nil, err
	}
	return gonet.DialUDP(g.stack, &full, nil, protocol)
}

func (g *gvisorNetwork) ResolveIP(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		return g.addressForTunnel([]net.IP{ip}, host)
	}
	return g.resolveViaServers(ctx, host, g.dns)
}

func (g *gvisorNetwork) resolveViaServers(ctx context.Context, host string, servers []string) (net.IP, error) {
	if len(servers) == 0 {
		return nil, errors.New("netstack: no DNS servers assigned by ePDG")
	}
	var lastErr error
	for _, server := range servers {
		resolver := g.resolver(server)
		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err == nil {
			if selected, selectErr := g.addressForTunnel(ips, host); selectErr == nil {
				return selected, nil
			} else {
				lastErr = selectErr
				continue
			}
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("no address records")
		}
	}
	return nil, fmt.Errorf("netstack: resolve %s through SWu DNS: %w", host, lastErr)
}

func (g *gvisorNetwork) addressForTunnel(ips []net.IP, host string) (net.IP, error) {
	for _, ip := range ips {
		if g.protocol == ipv4.ProtocolNumber {
			if ipv4Address := ip.To4(); ipv4Address != nil {
				return ipv4Address, nil
			}
			continue
		}
		if ip.To4() == nil {
			if ipv6Address := ip.To16(); ipv6Address != nil {
				return ipv6Address, nil
			}
		}
	}
	family := "IPv6"
	if g.protocol == ipv4.ProtocolNumber {
		family = "IPv4"
	}
	return nil, fmt.Errorf("netstack: %s has no %s address for the negotiated tunnel", host, family)
}

func (g *gvisorNetwork) LookupSRV(ctx context.Context, service, proto, name string) (string, uint16, error) {
	if len(g.dns) == 0 {
		return "", 0, errors.New("netstack: no DNS servers assigned by ePDG")
	}
	var lastErr error
	for _, server := range g.dns {
		_, records, err := g.resolver(server).LookupSRV(ctx, service, proto, name)
		if err == nil && len(records) > 0 {
			return strings.TrimSuffix(records[0].Target, "."), records[0].Port, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("no SRV records")
		}
	}
	return "", 0, fmt.Errorf("netstack: resolve SRV through SWu DNS: %w", lastErr)
}

func (g *gvisorNetwork) resolver(server string) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: g.dnsDialer(server)}
}

func (g *gvisorNetwork) dnsDialer(server string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		if _, _, err := net.SplitHostPort(server); err != nil {
			server = net.JoinHostPort(server, "53")
		}
		return g.dialResolved(ctx, "udp", server)
	}
}

func (g *gvisorNetwork) resolveAddress(ctx context.Context, addr string) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return tcpip.FullAddress{}, 0, fmt.Errorf("netstack: parse address %q: %w", addr, err)
	}
	ip, err := g.ResolveIP(ctx, host)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return tcpip.FullAddress{}, 0, fmt.Errorf("netstack: parse port %q: %w", portText, err)
	}
	return fullAddress(ip, port)
}

func (g *gvisorNetwork) dialResolved(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil {
		return nil, fmt.Errorf("netstack: DNS server must be an IP address: %q", addr)
	}
	remote, protocol, err := resolvedFullAddress(addr)
	if err != nil {
		return nil, err
	}
	if network == "udp" {
		return gonet.DialUDP(g.stack, nil, &remote, protocol)
	}
	return gonet.DialContextTCP(ctx, g.stack, remote, protocol)
}

func resolvedFullAddress(addr string) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	return fullAddress(net.ParseIP(host), port)
}

func fullAddress(ip net.IP, port int) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	if v4 := ip.To4(); v4 != nil {
		return tcpip.FullAddress{NIC: imsNICID, Addr: tcpip.AddrFrom4Slice(v4), Port: uint16(port)}, ipv4.ProtocolNumber, nil
	}
	if v6 := ip.To16(); v6 != nil {
		return tcpip.FullAddress{NIC: imsNICID, Addr: tcpip.AddrFrom16Slice(v6), Port: uint16(port)}, ipv6.ProtocolNumber, nil
	}
	return tcpip.FullAddress{}, 0, errors.New("netstack: invalid IP address")
}

func (g *gvisorNetwork) outboundLoop(ctx context.Context) {
	defer g.done.Done()
	for {
		packet := g.link.ReadContext(ctx)
		if packet == nil {
			return
		}
		view := packet.ToView()
		data := append([]byte(nil), view.AsSlice()...)
		view.Release()
		packet.DecRef()
		data, err := g.transformOutbound(data)
		if err != nil {
			g.fail("transform outbound IPsec packet", err)
			return
		}
		if err := g.packetIO.WritePacketContext(ctx, data); err != nil {
			if ctx.Err() == nil {
				g.fail("write SWu packet", err)
			}
			return
		}
		g.stats.PacketsOut.Add(1)
		g.stats.BytesOut.Add(uint64(len(data)))
	}
}

func (g *gvisorNetwork) inboundLoop(ctx context.Context) {
	defer g.done.Done()
	for {
		data, err := g.packetIO.ReadPacketContext(ctx)
		if err != nil {
			if ctx.Err() == nil {
				g.fail("read SWu packet", err)
			}
			return
		}
		data, err = g.transformInbound(data)
		if err != nil {
			g.fail("transform inbound IPsec packet", err)
			return
		}
		protocol, err := networkProtocol(data)
		if err != nil {
			continue
		}
		packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(data)})
		g.link.InjectInbound(protocol, packet)
		packet.DecRef()
		g.stats.PacketsIn.Add(1)
		g.stats.BytesIn.Add(uint64(len(data)))
	}
}

func networkProtocol(packet []byte) (tcpip.NetworkProtocolNumber, error) {
	if len(packet) == 0 {
		return 0, errors.New("netstack: empty packet")
	}
	switch packet[0] >> 4 {
	case 4:
		return ipv4.ProtocolNumber, nil
	case 6:
		return ipv6.ProtocolNumber, nil
	default:
		return 0, fmt.Errorf("netstack: unsupported IP version %d", packet[0]>>4)
	}
}

func (g *gvisorNetwork) Close() {
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return
	}
	g.closed = true
	g.mu.Unlock()
	g.cancel()
	g.link.Close()
	g.stack.Close()
	g.done.Wait()
}

func gvisorError(operation string, err tcpip.Error) error {
	return fmt.Errorf("netstack: %s: %s", operation, err.String())
}
