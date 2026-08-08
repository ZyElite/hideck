//go:build linux

package driver

import (
	"fmt"
	"net"
	"time"

	"github.com/iniwex5/netlink"
	"golang.org/x/sys/unix"
)

const (
	addressAddAttempts = 5
	addressRetryDelay  = 80 * time.Millisecond
)

func (n *NetTools) AddAddress(arguments ...any) error {
	iface, cidr, err := n.addressArguments(arguments, false)
	if err != nil {
		return err
	}
	return addAddress(iface, cidr, false)
}

func (n *NetTools) DelAddress(arguments ...any) error {
	iface, cidr, err := n.addressArguments(arguments, false)
	if err != nil {
		return err
	}
	return deleteAddress(iface, cidr, false)
}

func (n *NetTools) AddAddress6(arguments ...any) error {
	iface, cidr, err := n.addressArguments(arguments, true)
	if err != nil {
		return err
	}
	return addAddress(iface, cidr, true)
}

func (n *NetTools) DelAddress6(arguments ...any) error {
	return n.DelAddress(arguments...)
}

func (n *NetTools) addressArguments(arguments []any, ipv6 bool) (string, string, error) {
	if len(arguments) != 2 {
		return "", "", fmt.Errorf("address operation expects two arguments")
	}
	if iface, ok := arguments[0].(string); ok {
		cidr, cidrOK := arguments[1].(string)
		if cidrOK {
			return iface, cidr, nil
		}
	}
	ip, ipOK := arguments[0].(net.IP)
	prefix, prefixOK := arguments[1].(int)
	if !ipOK || !prefixOK {
		return "", "", fmt.Errorf("address operation expects (interface, CIDR) or (IP, prefix)")
	}
	iface, err := n.interfaceName(nil)
	if err != nil {
		return "", "", err
	}
	bits := net.IPv4len * 8
	if ipv6 {
		bits = net.IPv6len * 8
	}
	if prefix < 0 || prefix > bits {
		return "", "", fmt.Errorf("invalid prefix length %d for %d-bit address", prefix, bits)
	}
	return iface, fmt.Sprintf("%s/%d", ip.String(), prefix), nil
}

func addAddress(iface, cidr string, ipv6 bool) error {
	operation := "addr add"
	if ipv6 {
		operation = "addr add -6"
	}
	arguments := fmt.Sprintf("%s dev %s", cidr, iface)
	link, err := getLink(iface)
	if err != nil {
		return wrapErr(operation, arguments, err)
	}
	address, err := netlink.ParseAddr(cidr)
	if err != nil {
		return wrapErr(operation, arguments, fmt.Errorf("解析地址失败: %v", err))
	}
	if !ipv6 {
		return wrapErr(operation, arguments, netlink.AddrAdd(link, address))
	}
	address.Flags |= unix.IFA_F_NODAD
	return addIPv6AddressWithRetry(link, address, operation, arguments)
}

func addIPv6AddressWithRetry(link netlink.Link, address *netlink.Addr, operation, arguments string) error {
	var lastErr error
	for attempt := 0; attempt < addressAddAttempts; attempt++ {
		lastErr = netlink.AddrAdd(link, address)
		if lastErr == nil {
			return nil
		}
		if attempt+1 < addressAddAttempts {
			time.Sleep(addressRetryDelay)
		}
	}
	return wrapErr(operation, arguments, lastErr)
}

func deleteAddress(iface, cidr string, _ bool) error {
	operation := "addr del"
	arguments := fmt.Sprintf("%s dev %s", cidr, iface)
	link, err := getLink(iface)
	if err != nil {
		return wrapErr(operation, arguments, err)
	}
	address, err := netlink.ParseAddr(cidr)
	if err != nil {
		return wrapErr(operation, arguments, fmt.Errorf("解析地址失败: %v", err))
	}
	return wrapErr(operation, arguments, netlink.AddrDel(link, address))
}
