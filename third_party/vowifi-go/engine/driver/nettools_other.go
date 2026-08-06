//go:build !linux

package driver

import (
	"errors"
	"net"
)

// errUnsupportedPlatform is returned by the non-Linux driver stubs.
var errUnsupportedPlatform = errors.New("driver: netlink operations require linux")

// NetToolError wraps a netlink operation failure with the operation context.
type NetToolError struct {
	Op  string
	Err error
}

func (e *NetToolError) Error() string {
	if e == nil {
		return ""
	}
	return "driver: " + e.Op + ": " + e.Err.Error()
}

func (e *NetToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NetTools is the (non-Linux) netlink configuration surface stub.
type NetTools struct {
	linkName string
}

// NewNetTools creates a NetTools bound to the given link name.
func NewNetTools(linkName string) *NetTools {
	return &NetTools{linkName: linkName}
}

// GetLink is unsupported off Linux.
func (t *NetTools) GetLink() (interface{}, error) { return nil, errUnsupportedPlatform }

// SetLinkUp is unsupported off Linux.
func (t *NetTools) SetLinkUp() error { return errUnsupportedPlatform }

// SetLinkDown is unsupported off Linux.
func (t *NetTools) SetLinkDown() error { return errUnsupportedPlatform }

// SetMTU is unsupported off Linux.
func (t *NetTools) SetMTU(mtu int) error { return errUnsupportedPlatform }

// DeleteLink is unsupported off Linux.
func (t *NetTools) DeleteLink() error { return errUnsupportedPlatform }

// AddAddress is unsupported off Linux.
func (t *NetTools) AddAddress(ip net.IP, prefixLen int) error { return errUnsupportedPlatform }

// AddAddress6 is unsupported off Linux.
func (t *NetTools) AddAddress6(ip net.IP, prefixLen int) error { return errUnsupportedPlatform }

// DelAddress is unsupported off Linux.
func (t *NetTools) DelAddress(ip net.IP, prefixLen int) error { return errUnsupportedPlatform }

// DelAddress6 is unsupported off Linux.
func (t *NetTools) DelAddress6(ip net.IP, prefixLen int) error { return errUnsupportedPlatform }

// AddRoute is unsupported off Linux.
func (t *NetTools) AddRoute(dst *net.IPNet, gw net.IP) error { return errUnsupportedPlatform }

// AddRoute6 is unsupported off Linux.
func (t *NetTools) AddRoute6(dst *net.IPNet, gw net.IP) error { return errUnsupportedPlatform }

// DelRoute is unsupported off Linux.
func (t *NetTools) DelRoute(dst *net.IPNet, gw net.IP) error { return errUnsupportedPlatform }

// DelRoute6 is unsupported off Linux.
func (t *NetTools) DelRoute6(dst *net.IPNet, gw net.IP) error { return errUnsupportedPlatform }

// AddRouteTable is unsupported off Linux.
func (t *NetTools) AddRouteTable(dst *net.IPNet, gw net.IP, table int) error {
	return errUnsupportedPlatform
}

// DelRouteTable is unsupported off Linux.
func (t *NetTools) DelRouteTable(dst *net.IPNet, gw net.IP, table int) error {
	return errUnsupportedPlatform
}

// AddRule is unsupported off Linux.
func (t *NetTools) AddRule(rule interface{}) error { return errUnsupportedPlatform }

// DelRule is unsupported off Linux.
func (t *NetTools) DelRule(rule interface{}) error { return errUnsupportedPlatform }

// FlushRules is unsupported off Linux.
func (t *NetTools) FlushRules() error { return errUnsupportedPlatform }

// AddInputRule is unsupported off Linux.
func (t *NetTools) AddInputRule(mark, table int) error { return errUnsupportedPlatform }

// DelInputRule is unsupported off Linux.
func (t *NetTools) DelInputRule(mark, table int) error { return errUnsupportedPlatform }

// EnsureIPv6Enabled is unsupported off Linux.
func (t *NetTools) EnsureIPv6Enabled() error { return errUnsupportedPlatform }

// SetSysctl is unsupported off Linux.
func (t *NetTools) SetSysctl(key, value string) error { return errUnsupportedPlatform }

// CleanConflictRoutes is unsupported off Linux.
func (t *NetTools) CleanConflictRoutes(dst *net.IPNet) error { return errUnsupportedPlatform }

// Begin starts a network transaction.
func (t *NetTools) Begin() *NetTxn { return &NetTxn{tools: t} }

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
