//go:build linux

package driver

import (
	"errors"
	"fmt"

	"github.com/iniwex5/netlink"
)

func deleteLinkIfExists(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("获取接口 %s 失败: %w", name, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("删除接口 %s 失败: %w", name, err)
	}
	return nil
}

func getLink(iface string) (netlink.Link, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, fmt.Errorf("获取接口 %s 失败: %v", iface, err)
	}
	return link, nil
}

func (n *NetTools) GetLink(iface string) (netlink.Link, error) { return getLink(iface) }

func (n *NetTools) SetLinkUp(iface string) error {
	link, err := getLink(iface)
	if err != nil {
		return wrapErr("link set up", iface, err)
	}
	return wrapErr("link set up", iface, netlink.LinkSetUp(link))
}

func (n *NetTools) SetLinkDown(iface string) error {
	link, err := getLink(iface)
	if err != nil {
		return wrapErr("link set down", iface, err)
	}
	return wrapErr("link set down", iface, netlink.LinkSetDown(link))
}

func (n *NetTools) DeleteLink(iface string) error {
	link, err := getLink(iface)
	if err != nil {
		return wrapErr("link del", iface, err)
	}
	return wrapErr("link del", iface, netlink.LinkDel(link))
}

func (n *NetTools) SetMTU(iface string, mtu int) error {
	link, err := getLink(iface)
	args := fmt.Sprintf("%s %d", iface, mtu)
	if err != nil {
		return wrapErr("link set mtu", args, err)
	}
	return wrapErr("link set mtu", args, netlink.LinkSetMTU(link, mtu))
}
