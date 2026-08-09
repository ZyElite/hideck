//go:build linux

package driver

import (
	"fmt"
	"time"

	"github.com/iniwex5/netlink"
	"golang.org/x/sys/unix"
)

const (
	addressAddAttempts = 5
	addressRetryDelay  = 80 * time.Millisecond
)

func (n *NetTools) AddAddress(iface, cidr string) error {
	return addAddress(iface, cidr, false)
}

func (n *NetTools) DelAddress(iface, cidr string) error {
	return deleteAddress(iface, cidr, false)
}

func (n *NetTools) AddAddress6(iface, cidr string) error {
	return addAddress(iface, cidr, true)
}

func (n *NetTools) DelAddress6(iface, cidr string) error { return deleteAddress(iface, cidr, true) }

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
