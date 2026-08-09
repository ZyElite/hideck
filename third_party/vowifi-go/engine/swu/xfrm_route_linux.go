//go:build linux

package swu

import (
	"errors"
	"net"

	"github.com/iniwex5/netlink"
)

func detectOutboundRoute(remoteIP net.IP) (net.IP, int, string, error) {
	if remoteIP == nil {
		return nil, 0, "", errors.New("swu: remote IP is nil")
	}
	routes, err := netlink.RouteGet(remoteIP)
	if err != nil {
		return nil, 0, "", err
	}
	for _, route := range routes {
		if !usableRouteSource(route.Src, remoteIP) {
			continue
		}
		return route.Src, route.LinkIndex, routeInterfaceName(route.LinkIndex), nil
	}
	return nil, 0, "", errors.New("swu: no usable route source found")
}

func usableRouteSource(source, remote net.IP) bool {
	if source == nil || source.IsUnspecified() {
		return false
	}
	return (source.To4() == nil) == (remote.To4() == nil)
}

func routeInterfaceName(index int) string {
	if index <= 0 {
		return ""
	}
	link, err := netlink.LinkByIndex(index)
	if err != nil || link == nil {
		return ""
	}
	return link.Attrs().Name
}
