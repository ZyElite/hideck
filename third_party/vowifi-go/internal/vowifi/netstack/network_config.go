package netstack

import (
	"errors"
	"net"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

func transportProtocols() []stack.TransportProtocolFactory {
	return []stack.TransportProtocolFactory{
		tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6,
	}
}

func (n *Network) configureAddresses(prefixLen int) error {
	if err := n.configureAddressesForNIC(tcpip.NICID(n.nicID), prefixLen); err != nil {
		return err
	}
	return n.configureAddressesForNIC(loopbackNICID, prefixLen)
}

func (n *Network) configureAddressesForNIC(nicID tcpip.NICID, prefixLen int) error {
	if n.ipv4 != nil {
		address := tcpip.ProtocolAddress{
			Protocol: ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{
				Address: tcpip.AddrFrom4Slice(n.ipv4), PrefixLen: net.IPv4len * 8,
			},
		}
		if err := n.stack.AddProtocolAddress(nicID, address, stack.AddressProperties{}); err != nil {
			return tcpipError(err)
		}
	}
	if n.ipv6 == nil {
		return nil
	}
	if prefixLen < 1 || prefixLen > net.IPv6len*8 {
		prefixLen = net.IPv6len * 8
	}
	address := tcpip.ProtocolAddress{
		Protocol: ipv6.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address: tcpip.AddrFrom16Slice(n.ipv6), PrefixLen: prefixLen,
		},
	}
	return tcpipError(n.stack.AddProtocolAddress(nicID, address, stack.AddressProperties{}))
}

func (n *Network) configureRoutes() {
	routes := make([]tcpip.Route, 0, 4)
	if n.ipv4 != nil {
		address := tcpip.AddressWithPrefix{
			Address: tcpip.AddrFrom4Slice(n.ipv4), PrefixLen: net.IPv4len * 8,
		}
		routes = append(routes, tcpip.Route{Destination: address.Subnet(), NIC: loopbackNICID})
	}
	if n.ipv6 != nil {
		address := tcpip.AddressWithPrefix{
			Address: tcpip.AddrFrom16Slice(n.ipv6), PrefixLen: net.IPv6len * 8,
		}
		routes = append(routes, tcpip.Route{Destination: address.Subnet(), NIC: loopbackNICID})
	}
	routes = append(routes,
		tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: tcpip.NICID(n.nicID)},
		tcpip.Route{Destination: header.IPv6EmptySubnet, NIC: tcpip.NICID(n.nicID)},
	)
	n.stack.SetRouteTable(routes)
}

func tcpipError(err tcpip.Error) error {
	if err == nil {
		return nil
	}
	return errors.New(err.String())
}
