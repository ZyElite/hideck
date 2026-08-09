//go:build linux

package driver

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/iniwex5/netlink"
)

func (n *NetTools) AddRoute(cidr, gateway, iface string) error {
	return changeRoute("route add", cidr, gateway, iface, 0, true)
}

func (n *NetTools) DelRoute(cidr, gateway, iface string) error {
	return changeRoute("route del", cidr, gateway, iface, 0, false)
}

func (n *NetTools) AddRoute6(cidr, gateway, iface string) error {
	return n.AddRoute(cidr, gateway, iface)
}

func (n *NetTools) DelRoute6(cidr, gateway, iface string) error {
	return n.DelRoute(cidr, gateway, iface)
}

func (n *NetTools) AddRouteTable(cidr, iface string, table int) error {
	return changeRoute("route add table", cidr, "", iface, table, true)
}

func (n *NetTools) DelRouteTable(cidr, iface string, table int) error {
	return changeRoute("route del table", cidr, "", iface, table, false)
}

func changeRoute(operation, cidr, gateway, iface string, table int, add bool) error {
	_, destination, err := net.ParseCIDR(cidr)
	if err != nil {
		return wrapErr(operation, cidr, fmt.Errorf("解析目标地址失败: %v", err))
	}
	route := &netlink.Route{Dst: destination, Table: table}
	if gateway != "" {
		route.Gw = net.ParseIP(gateway)
		if add && route.Gw == nil {
			return wrapErr(operation, cidr, fmt.Errorf("无效的网关地址: %s", gateway))
		}
	}
	if iface != "" {
		link, linkErr := getLink(iface)
		if linkErr != nil {
			return wrapErr(operation, cidr, linkErr)
		}
		route.LinkIndex = link.Attrs().Index
	}
	if add {
		err = netlink.RouteAdd(route)
		if isRouteExists(err) {
			return nil
		}
	} else {
		err = netlink.RouteDel(route)
		if isRouteNotFound(err) {
			return nil
		}
	}
	return wrapErr(operation, cidr, err)
}

func isRouteExists(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.EEXIST
}

func isRouteNotFound(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.ESRCH
}

func isRuleExists(err error) bool { return isRouteExists(err) }
