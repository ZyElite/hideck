package swu

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/iniwex5/vowifi-go/engine/ikev2"
)

type dataPlaneRoute struct {
	cidr string
	ipv6 bool
}

func (s *Session) dataPlaneRoutes() ([]dataPlaneRoute, bool, error) {
	routes := make([]dataPlaneRoute, 0)
	for _, server := range s.pcscfServers {
		if ipv4 := server.To4(); ipv4 != nil {
			routes = append(routes, dataPlaneRoute{cidr: ipv4.String() + "/32"})
			continue
		}
		if ipv6 := server.To16(); ipv6 != nil {
			routes = append(routes, dataPlaneRoute{cidr: ipv6.String() + "/128", ipv6: true})
		}
	}
	for _, selector := range trafficSelectors(s.childTSr) {
		prefixes, err := selectorPrefixes(selector)
		if err != nil {
			return nil, false, err
		}
		routes = append(routes, prefixes...)
	}
	routes = uniqueRoutes(routes)
	for _, route := range routes {
		if route.ipv6 {
			return routes, true, nil
		}
	}
	return routes, false, nil
}

func trafficSelectors(payload *ikev2.EncryptedPayloadTS) []*ikev2.TrafficSelector {
	if payload == nil {
		return nil
	}
	if payload.TrafficSelectors != nil {
		return payload.TrafficSelectors
	}
	return payload.Selectors
}

func selectorPrefixes(selector *ikev2.TrafficSelector) ([]dataPlaneRoute, error) {
	if selector == nil {
		return nil, fmt.Errorf("swu: nil traffic selector")
	}
	isIPv6 := selector.TSType == ikev2.TS_IPV6_ADDR_RANGE
	if selector.TSType != ikev2.TS_IPV4_ADDR_RANGE && !isIPv6 {
		return nil, nil
	}
	prefixes, err := ipRangePrefixes(net.IP(selector.StartAddr), net.IP(selector.EndAddr), isIPv6)
	if err != nil {
		return nil, fmt.Errorf("swu: invalid traffic selector range: %w", err)
	}
	routes := make([]dataPlaneRoute, 0, len(prefixes))
	for _, prefix := range prefixes {
		routes = append(routes, dataPlaneRoute{cidr: prefix.String(), ipv6: isIPv6})
	}
	return routes, nil
}

func ipRangePrefixes(startIP, endIP net.IP, ipv6 bool) ([]netip.Prefix, error) {
	start, end, err := rangeAddresses(startIP, endIP, ipv6)
	if err != nil {
		return nil, err
	}
	if start.Compare(end) > 0 {
		return nil, fmt.Errorf("start %s follows end %s", start, end)
	}
	var prefixes []netip.Prefix
	for current := start; current.Compare(end) <= 0; {
		prefix := largestRangePrefix(current, end)
		prefixes = append(prefixes, prefix)
		last := prefixLastAddress(prefix)
		if last == end {
			break
		}
		current = last.Next()
	}
	return prefixes, nil
}

func rangeAddresses(startIP, endIP net.IP, ipv6 bool) (netip.Addr, netip.Addr, error) {
	if !ipv6 {
		start, end := startIP.To4(), endIP.To4()
		if start == nil || end == nil {
			return netip.Addr{}, netip.Addr{}, fmt.Errorf("invalid IPv4 range %v..%v", startIP, endIP)
		}
		return netip.AddrFrom4([4]byte(start)), netip.AddrFrom4([4]byte(end)), nil
	}
	start, end := startIP.To16(), endIP.To16()
	if start == nil || end == nil || startIP.To4() != nil || endIP.To4() != nil {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("invalid IPv6 range %v..%v", startIP, endIP)
	}
	return netip.AddrFrom16([16]byte(start)), netip.AddrFrom16([16]byte(end)), nil
}

func largestRangePrefix(start, end netip.Addr) netip.Prefix {
	prefix := netip.PrefixFrom(start, start.BitLen())
	for bits := prefix.Bits() - 1; bits >= 0; bits-- {
		candidate := netip.PrefixFrom(start, bits).Masked()
		if candidate.Addr() != start || prefixLastAddress(candidate).Compare(end) > 0 {
			break
		}
		prefix = candidate
	}
	return prefix
}

func prefixLastAddress(prefix netip.Prefix) netip.Addr {
	if prefix.Addr().Is4() {
		bytes := prefix.Masked().Addr().As4()
		setHostBits(bytes[:], prefix.Bits())
		return netip.AddrFrom4(bytes)
	}
	bytes := prefix.Masked().Addr().As16()
	setHostBits(bytes[:], prefix.Bits())
	return netip.AddrFrom16(bytes)
}

func setHostBits(address []byte, prefixBits int) {
	for bit := prefixBits; bit < len(address)*8; bit++ {
		address[bit/8] |= byte(1 << (7 - bit%8))
	}
}

func uniqueRoutes(routes []dataPlaneRoute) []dataPlaneRoute {
	seen := make(map[string]struct{}, len(routes))
	result := make([]dataPlaneRoute, 0, len(routes))
	for _, route := range routes {
		if _, ok := seen[route.cidr]; ok {
			continue
		}
		seen[route.cidr] = struct{}{}
		result = append(result, route)
	}
	return result
}
