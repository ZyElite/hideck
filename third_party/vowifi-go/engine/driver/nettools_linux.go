//go:build linux

package driver

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
)

// NetToolError wraps a netlink operation failure with the operation context.
type NetToolError struct {
	Op  string
	Err error
}

func (e *NetToolError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("driver: %s: %v", e.Op, e.Err)
}

func (e *NetToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NetTools is the Linux netlink configuration surface.
type NetTools struct {
	linkName string
}

// NewNetTools creates a NetTools bound to the given link name.
func NewNetTools(linkName string) *NetTools {
	return &NetTools{linkName: linkName}
}

// GetLink returns the netlink link by name.
func (t *NetTools) GetLink() (netlink.Link, error) {
	link, err := netlink.LinkByName(t.linkName)
	if err != nil {
		return nil, &NetToolError{Op: "get link " + t.linkName, Err: err}
	}
	return link, nil
}

// SetLinkUp brings the link up.
func (t *NetTools) SetLinkUp() error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return &NetToolError{Op: "set link up", Err: err}
	}
	return nil
}

// SetLinkDown brings the link down.
func (t *NetTools) SetLinkDown() error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	if err := netlink.LinkSetDown(link); err != nil {
		return &NetToolError{Op: "set link down", Err: err}
	}
	return nil
}

// SetMTU sets the link MTU.
func (t *NetTools) SetMTU(mtu int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return &NetToolError{Op: "set mtu", Err: err}
	}
	return nil
}

// DeleteLink removes the link.
func (t *NetTools) DeleteLink() error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	if err := netlink.LinkDel(link); err != nil {
		return &NetToolError{Op: "delete link", Err: err}
	}
	return nil
}

// AddAddress assigns an IPv4 address to the link.
func (t *NetTools) AddAddress(ip net.IP, prefixLen int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	addr := &netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: net.CIDRMask(prefixLen, 32)}}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return &NetToolError{Op: "add address", Err: err}
	}
	return nil
}

// AddAddress6 assigns an IPv6 address to the link.
func (t *NetTools) AddAddress6(ip net.IP, prefixLen int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	addr := &netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: net.CIDRMask(prefixLen, 128)}}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return &NetToolError{Op: "add address6", Err: err}
	}
	return nil
}

// DelAddress removes an IPv4 address from the link.
func (t *NetTools) DelAddress(ip net.IP, prefixLen int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	addr := &netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: net.CIDRMask(prefixLen, 32)}}
	if err := netlink.AddrDel(link, addr); err != nil {
		return &NetToolError{Op: "del address", Err: err}
	}
	return nil
}

// DelAddress6 removes an IPv6 address from the link.
func (t *NetTools) DelAddress6(ip net.IP, prefixLen int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	addr := &netlink.Addr{IPNet: &net.IPNet{IP: ip, Mask: net.CIDRMask(prefixLen, 128)}}
	if err := netlink.AddrDel(link, addr); err != nil {
		return &NetToolError{Op: "del address6", Err: err}
	}
	return nil
}

// AddRoute installs an IPv4 route via the link.
func (t *NetTools) AddRoute(dst *net.IPNet, gw net.IP) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Gw: gw}
	if err := netlink.RouteAdd(route); err != nil {
		return &NetToolError{Op: "add route", Err: err}
	}
	return nil
}

// AddRoute6 installs an IPv6 route via the link.
func (t *NetTools) AddRoute6(dst *net.IPNet, gw net.IP) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Gw: gw}
	if err := netlink.RouteAdd(route); err != nil {
		return &NetToolError{Op: "add route6", Err: err}
	}
	return nil
}

// DelRoute removes an IPv4 route.
func (t *NetTools) DelRoute(dst *net.IPNet, gw net.IP) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Gw: gw}
	if err := netlink.RouteDel(route); err != nil {
		return &NetToolError{Op: "del route", Err: err}
	}
	return nil
}

// DelRoute6 removes an IPv6 route.
func (t *NetTools) DelRoute6(dst *net.IPNet, gw net.IP) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Gw: gw}
	if err := netlink.RouteDel(route); err != nil {
		return &NetToolError{Op: "del route6", Err: err}
	}
	return nil
}

// AddRouteTable installs a route in a specific routing table.
func (t *NetTools) AddRouteTable(dst *net.IPNet, gw net.IP, table int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Gw: gw, Table: table}
	if err := netlink.RouteAdd(route); err != nil {
		return &NetToolError{Op: "add route table", Err: err}
	}
	return nil
}

// DelRouteTable removes a route from a specific routing table.
func (t *NetTools) DelRouteTable(dst *net.IPNet, gw net.IP, table int) error {
	link, err := t.GetLink()
	if err != nil {
		return err
	}
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: dst, Gw: gw, Table: table}
	if err := netlink.RouteDel(route); err != nil {
		return &NetToolError{Op: "del route table", Err: err}
	}
	return nil
}

// AddRule installs an ip rule.
func (t *NetTools) AddRule(rule *netlink.Rule) error {
	if err := netlink.RuleAdd(rule); err != nil {
		return &NetToolError{Op: "add rule", Err: err}
	}
	return nil
}

// DelRule removes an ip rule.
func (t *NetTools) DelRule(rule *netlink.Rule) error {
	if err := netlink.RuleDel(rule); err != nil {
		return &NetToolError{Op: "del rule", Err: err}
	}
	return nil
}

// FlushRules removes all ip rules.
func (t *NetTools) FlushRules() error {
	rules, err := netlink.RuleList(netlink.FAMILY_ALL)
	if err != nil {
		return &NetToolError{Op: "list rules", Err: err}
	}
	for _, r := range rules {
		if r.Priority < 1000 {
			continue // keep the kernel's default rules
		}
		if err := netlink.RuleDel(&r); err != nil {
			return &NetToolError{Op: "flush rule", Err: err}
		}
	}
	return nil
}

// AddInputRule installs an input (fwmark) rule for the tunnel.
func (t *NetTools) AddInputRule(mark, table int) error {
	rule := netlink.NewRule()
	rule.Mark = uint32(mark)
	rule.Table = table
	rule.Priority = 1000
	return t.AddRule(rule)
}

// DelInputRule removes the input rule.
func (t *NetTools) DelInputRule(mark, table int) error {
	rule := netlink.NewRule()
	rule.Mark = uint32(mark)
	rule.Table = table
	rule.Priority = 1000
	return t.DelRule(rule)
}

// EnsureIPv6Enabled enables IPv6 on the link.
func (t *NetTools) EnsureIPv6Enabled() error {
	return t.SetSysctl("disable_ipv6", "0")
}

// SetSysctl writes a sysctl value under /proc/sys/net/ipv6/conf/<link>/.
func (t *NetTools) SetSysctl(key, value string) error {
	path := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/%s", t.linkName, key)
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		return &NetToolError{Op: "set sysctl " + path, Err: err}
	}
	return nil
}

// CleanConflictRoutes removes routes that conflict with the given destination.
func (t *NetTools) CleanConflictRoutes(dst *net.IPNet) error {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return &NetToolError{Op: "list routes", Err: err}
	}
	for _, r := range routes {
		if r.Dst != nil && r.Dst.String() == dst.String() {
			if err := netlink.RouteDel(&r); err != nil {
				return &NetToolError{Op: "clean conflict route", Err: err}
			}
		}
	}
	return nil
}

// Begin starts a network transaction.
func (t *NetTools) Begin() *NetTxn {
	return &NetTxn{tools: t, ops: nil}
}

// NetTxn batches netlink operations and applies them on Commit.
type NetTxn struct {
	tools *NetTools
	ops   []func() error
}

// AddAddress queues an IPv4 address assignment.
func (tx *NetTxn) AddAddress(ip net.IP, prefixLen int) {
	tx.ops = append(tx.ops, func() error { return tx.tools.AddAddress(ip, prefixLen) })
}

// AddAddress6 queues an IPv6 address assignment.
func (tx *NetTxn) AddAddress6(ip net.IP, prefixLen int) {
	tx.ops = append(tx.ops, func() error { return tx.tools.AddAddress6(ip, prefixLen) })
}

// AddRoute queues an IPv4 route.
func (tx *NetTxn) AddRoute(dst *net.IPNet, gw net.IP) {
	tx.ops = append(tx.ops, func() error { return tx.tools.AddRoute(dst, gw) })
}

// AddRoute6 queues an IPv6 route.
func (tx *NetTxn) AddRoute6(dst *net.IPNet, gw net.IP) {
	tx.ops = append(tx.ops, func() error { return tx.tools.AddRoute6(dst, gw) })
}

// SetLinkUp queues a link-up.
func (tx *NetTxn) SetLinkUp() {
	tx.ops = append(tx.ops, func() error { return tx.tools.SetLinkUp() })
}

// SetMTU queues an MTU change.
func (tx *NetTxn) SetMTU(mtu int) {
	tx.ops = append(tx.ops, func() error { return tx.tools.SetMTU(mtu) })
}

// Commit applies all queued operations in order.
func (tx *NetTxn) Commit() error {
	for _, op := range tx.ops {
		if err := op(); err != nil {
			return err
		}
	}
	tx.ops = nil
	return nil
}

// Rollback discards the queued operations.
func (tx *NetTxn) Rollback() {
	tx.ops = nil
}

// normalizeLinkName trims whitespace from a link name.
func normalizeLinkName(name string) string {
	return strings.TrimSpace(name)
}
