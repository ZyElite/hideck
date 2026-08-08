//go:build linux

package driver

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/iniwex5/netlink"
)

func (n *NetTools) AddRoute(arguments ...any) error {
	cidr, gateway, iface, err := n.routeArguments(arguments)
	if err != nil {
		return err
	}
	return changeRoute("route add", cidr, gateway, iface, 0, true)
}

func (n *NetTools) DelRoute(arguments ...any) error {
	cidr, gateway, iface, err := n.routeArguments(arguments)
	if err != nil {
		return err
	}
	return changeRoute("route del", cidr, gateway, iface, 0, false)
}

func (n *NetTools) AddRoute6(arguments ...any) error { return n.AddRoute(arguments...) }
func (n *NetTools) DelRoute6(arguments ...any) error { return n.DelRoute(arguments...) }

func (n *NetTools) AddRouteTable(arguments ...any) error {
	cidr, gateway, iface, table, err := n.routeTableArguments(arguments)
	if err != nil {
		return err
	}
	return changeRoute("route add table", cidr, gateway, iface, table, true)
}

func (n *NetTools) DelRouteTable(arguments ...any) error {
	cidr, gateway, iface, table, err := n.routeTableArguments(arguments)
	if err != nil {
		return err
	}
	return changeRoute("route del table", cidr, gateway, iface, table, false)
}

func (n *NetTools) routeArguments(arguments []any) (string, string, string, error) {
	if len(arguments) == 3 {
		cidr, cidrOK := arguments[0].(string)
		gateway, gatewayOK := arguments[1].(string)
		iface, ifaceOK := arguments[2].(string)
		if cidrOK && gatewayOK && ifaceOK {
			return cidr, gateway, iface, nil
		}
	}
	if len(arguments) == 2 {
		destination, destinationOK := arguments[0].(*net.IPNet)
		gateway, gatewayOK := arguments[1].(net.IP)
		if !destinationOK || destination == nil || !gatewayOK {
			return "", "", "", fmt.Errorf("route operation expects (*net.IPNet, net.IP)")
		}
		iface, err := n.interfaceName(nil)
		return destination.String(), ipArgument(gateway), iface, err
	}
	return "", "", "", fmt.Errorf("route operation expects (CIDR, gateway, interface) or (destination, gateway)")
}

func (n *NetTools) routeTableArguments(arguments []any) (string, string, string, int, error) {
	if len(arguments) != 3 {
		return "", "", "", 0, fmt.Errorf("route table operation expects three arguments")
	}
	if cidr, ok := arguments[0].(string); ok {
		iface, ifaceOK := arguments[1].(string)
		table, tableOK := arguments[2].(int)
		if ifaceOK && tableOK {
			return cidr, "", iface, table, nil
		}
	}
	destination, destinationOK := arguments[0].(*net.IPNet)
	gateway, gatewayOK := arguments[1].(net.IP)
	table, tableOK := arguments[2].(int)
	if !destinationOK || destination == nil || !gatewayOK || !tableOK {
		return "", "", "", 0, fmt.Errorf("route table operation has invalid arguments")
	}
	iface, err := n.interfaceName(nil)
	return destination.String(), ipArgument(gateway), iface, table, err
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

func ipArgument(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
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
