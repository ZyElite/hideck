package netstack

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"

	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
)

func (n *Network) DialContext(
	ctx context.Context,
	network string,
	localAddr net.Addr,
	remoteAddr string,
	options imscore.DialOptions,
) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(options.Timeout))
		defer cancel()
	}
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		network = "tcp"
	}
	preferIPv6 := strings.HasSuffix(network, "6")
	remote, protocol, err := n.fullAddressFromRemote(ctx, remoteAddr, preferIPv6)
	if err != nil {
		return nil, err
	}
	local, err := n.fullAddressFromAddr(localAddr, remote)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(network, "udp") {
		return gonet.DialUDP(n.stack, &local, &remote, protocol)
	}
	if !strings.HasPrefix(network, "tcp") {
		return nil, fmt.Errorf("netstack: unsupported dial network %q", network)
	}
	return n.dialTCP(ctx, tcpDialConfig{
		local: local, remote: remote, protocol: protocol,
	}, options)
}

func (n *Network) dialTCP(
	ctx context.Context,
	config tcpDialConfig,
	options imscore.DialOptions,
) (net.Conn, error) {
	if options.TCPMSS > 0 {
		config.mss = options.TCPMSS
		return dialTCPWithMSS(ctx, n.stack, config)
	}
	return gonet.DialTCPWithBind(ctx, n.stack, config.local, config.remote, config.protocol)
}

func (n *Network) ListenTCP(_ context.Context, addr *net.TCPAddr) (net.Listener, error) {
	full, protocol, err := n.fullAddressFromListenAddr(addr)
	if err != nil {
		return nil, err
	}
	return gonet.ListenTCP(n.stack, full, protocol)
}

func (n *Network) ListenPacket(
	_ context.Context,
	network string,
	addr net.Addr,
) (net.PacketConn, error) {
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		network = "udp"
	}
	if !strings.HasPrefix(network, "udp") {
		return nil, fmt.Errorf("netstack: unsupported packet network %q", network)
	}
	local, protocol, err := n.fullAddressFromAddrWithHint(addr, strings.HasSuffix(network, "6"))
	if err != nil {
		return nil, err
	}
	return gonet.DialUDP(n.stack, &local, nil, protocol)
}

func (n *Network) fullAddressFromRemote(
	ctx context.Context,
	remote string,
	preferIPv6 bool,
) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	host, portText, err := net.SplitHostPort(remote)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port > 65535 {
		return tcpip.FullAddress{}, 0, fmt.Errorf("netstack: invalid port %q", portText)
	}
	ip, err := n.ResolveIP(ctx, host, preferIPv6)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	return n.fullAddressFromIPPort(ip, port, preferIPv6)
}

func (n *Network) fullAddressFromListenAddr(addr *net.TCPAddr) (
	tcpip.FullAddress,
	tcpip.NetworkProtocolNumber,
	error,
) {
	if addr == nil {
		return n.fullAddressFromIPPort(nil, 0, false)
	}
	return n.fullAddressFromIPPort(addr.IP, addr.Port, false)
}

func (n *Network) fullAddressFromAddr(
	addr net.Addr,
	hint tcpip.FullAddress,
) (tcpip.FullAddress, error) {
	preferIPv6 := hint.Addr.Len() == net.IPv6len
	if addr == nil {
		full, _, err := n.fullAddressFromIPPort(nil, 0, preferIPv6)
		return full, err
	}
	return n.localFullAddress(addr, preferIPv6)
}

func (n *Network) fullAddressFromAddrWithHint(
	addr net.Addr,
	preferIPv6 bool,
) (tcpip.FullAddress, tcpip.NetworkProtocolNumber, error) {
	if addr == nil {
		return n.fullAddressFromIPPort(nil, 0, preferIPv6)
	}
	full, err := n.localFullAddress(addr, preferIPv6)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	protocol := ipv4.ProtocolNumber
	if full.Addr.Len() == net.IPv6len {
		protocol = ipv6.ProtocolNumber
	}
	return full, protocol, nil
}

func (n *Network) localFullAddress(addr net.Addr, preferIPv6 bool) (tcpip.FullAddress, error) {
	var ip net.IP
	var port int
	switch value := addr.(type) {
	case *net.TCPAddr:
		ip, port = value.IP, value.Port
	case *net.UDPAddr:
		ip, port = value.IP, value.Port
	default:
		return tcpip.FullAddress{}, fmt.Errorf("netstack: unsupported local addr %T", addr)
	}
	full, _, err := n.fullAddressFromIPPort(ip, port, preferIPv6)
	return full, err
}

func (n *Network) fullAddressFromIPPort(ip net.IP, port int, preferIPv6 bool) (
	tcpip.FullAddress,
	tcpip.NetworkProtocolNumber,
	error,
) {
	if ip == nil || ip.IsUnspecified() {
		ip = n.localIPForPreference(preferIPv6)
	}
	address, protocol, err := tcpipAddressFromIP(ip)
	if err != nil {
		return tcpip.FullAddress{}, 0, err
	}
	return tcpip.FullAddress{NIC: tcpip.NICID(n.nicID), Addr: address, Port: uint16(port)}, protocol, nil
}

func (n *Network) localIPForPreference(preferIPv6 bool) net.IP {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if preferIPv6 && n.ipv6 != nil {
		return append(net.IP(nil), n.ipv6...)
	}
	if !preferIPv6 && n.ipv4 != nil {
		return append(net.IP(nil), n.ipv4...)
	}
	if n.ipv4 != nil {
		return append(net.IP(nil), n.ipv4...)
	}
	return append(net.IP(nil), n.ipv6...)
}

func tcpipAddressFromIP(ip net.IP) (tcpip.Address, tcpip.NetworkProtocolNumber, error) {
	if ip == nil {
		return tcpip.Address{}, 0, errors.New("netstack: nil IP")
	}
	if address := ip.To4(); address != nil {
		return tcpip.AddrFrom4Slice(address), ipv4.ProtocolNumber, nil
	}
	if address := ip.To16(); address != nil {
		return tcpip.AddrFrom16Slice(address), ipv6.ProtocolNumber, nil
	}
	return tcpip.Address{}, 0, fmt.Errorf("netstack: invalid IP %v", ip)
}
