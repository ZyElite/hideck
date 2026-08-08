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

func (n *NetTools) GetLink(iface ...string) (netlink.Link, error) {
	name, err := n.interfaceName(iface)
	if err != nil {
		return nil, err
	}
	return getLink(name)
}

func (n *NetTools) SetLinkUp(iface ...string) error {
	name, err := n.interfaceName(iface)
	if err != nil {
		return err
	}
	link, err := getLink(name)
	if err != nil {
		return wrapErr("link set up", name, err)
	}
	return wrapErr("link set up", name, netlink.LinkSetUp(link))
}

func (n *NetTools) SetLinkDown(iface ...string) error {
	name, err := n.interfaceName(iface)
	if err != nil {
		return err
	}
	link, err := getLink(name)
	if err != nil {
		return wrapErr("link set down", name, err)
	}
	return wrapErr("link set down", name, netlink.LinkSetDown(link))
}

func (n *NetTools) DeleteLink(iface ...string) error {
	name, err := n.interfaceName(iface)
	if err != nil {
		return err
	}
	link, err := getLink(name)
	if err != nil {
		return wrapErr("link del", name, err)
	}
	return wrapErr("link del", name, netlink.LinkDel(link))
}

func (n *NetTools) SetMTU(arguments ...any) error {
	iface, mtu, err := n.mtuArguments(arguments)
	if err != nil {
		return err
	}
	link, err := getLink(iface)
	args := fmt.Sprintf("%s %d", iface, mtu)
	if err != nil {
		return wrapErr("link set mtu", args, err)
	}
	return wrapErr("link set mtu", args, netlink.LinkSetMTU(link, mtu))
}

func (n *NetTools) mtuArguments(arguments []any) (string, int, error) {
	if len(arguments) == 2 {
		iface, ifaceOK := arguments[0].(string)
		mtu, mtuOK := arguments[1].(int)
		if ifaceOK && mtuOK {
			return iface, mtu, nil
		}
	}
	if len(arguments) == 1 {
		mtu, ok := arguments[0].(int)
		if !ok {
			return "", 0, fmt.Errorf("SetMTU: expected integer MTU")
		}
		iface, err := n.interfaceName(nil)
		return iface, mtu, err
	}
	return "", 0, fmt.Errorf("SetMTU: expected (interface, mtu) or (mtu)")
}
