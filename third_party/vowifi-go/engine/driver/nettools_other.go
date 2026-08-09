//go:build !linux

package driver

import (
	"errors"
	"net"

	"github.com/iniwex5/netlink"
)

var errUnsupportedPlatform = errors.New("driver: netlink operations require linux")

func getLink(string) (netlink.Link, error) { return nil, errUnsupportedPlatform }

func (n *NetTools) GetLink(string) (netlink.Link, error)    { return nil, errUnsupportedPlatform }
func (n *NetTools) SetLinkUp(string) error                  { return errUnsupportedPlatform }
func (n *NetTools) SetLinkDown(string) error                { return errUnsupportedPlatform }
func (n *NetTools) DeleteLink(string) error                 { return errUnsupportedPlatform }
func (n *NetTools) SetMTU(string, int) error                { return errUnsupportedPlatform }
func (n *NetTools) AddAddress(string, string) error         { return errUnsupportedPlatform }
func (n *NetTools) DelAddress(string, string) error         { return errUnsupportedPlatform }
func (n *NetTools) AddAddress6(string, string) error        { return errUnsupportedPlatform }
func (n *NetTools) DelAddress6(string, string) error        { return errUnsupportedPlatform }
func (n *NetTools) AddRoute(string, string, string) error   { return errUnsupportedPlatform }
func (n *NetTools) DelRoute(string, string, string) error   { return errUnsupportedPlatform }
func (n *NetTools) AddRoute6(string, string, string) error  { return errUnsupportedPlatform }
func (n *NetTools) DelRoute6(string, string, string) error  { return errUnsupportedPlatform }
func (n *NetTools) AddRouteTable(string, string, int) error { return errUnsupportedPlatform }
func (n *NetTools) DelRouteTable(string, string, int) error { return errUnsupportedPlatform }
func (n *NetTools) AddRule(string, int) error               { return errUnsupportedPlatform }
func (n *NetTools) DelRule(string, int) error               { return errUnsupportedPlatform }
func (n *NetTools) AddInputRule(string, int) error          { return errUnsupportedPlatform }
func (n *NetTools) DelInputRule(string, int) error          { return errUnsupportedPlatform }
func (n *NetTools) FlushRules(int, string) error            { return errUnsupportedPlatform }
func (n *NetTools) FlushRulesChecked(int, string) error     { return errUnsupportedPlatform }

func (n *NetTools) CleanConflictRoutes([]string, string, int) {
	panic(errUnsupportedPlatform)
}

func (n *NetTools) CleanConflictRoute(*net.IPNet) error { return errUnsupportedPlatform }
func (n *NetTools) SetSysctl(string, string) error      { return errUnsupportedPlatform }

func (n *NetTools) EnsureIPv6Enabled(string) ([]string, error) {
	return nil, errUnsupportedPlatform
}
