//go:build !linux

package driver

import (
	"errors"
	"net"

	"github.com/iniwex5/netlink"
)

var errUnsupportedPlatform = errors.New("driver: netlink operations require linux")

func getLink(string) (netlink.Link, error) { return nil, errUnsupportedPlatform }

func (n *NetTools) GetLink(...string) (netlink.Link, error) { return nil, errUnsupportedPlatform }
func (n *NetTools) SetLinkUp(...string) error               { return errUnsupportedPlatform }
func (n *NetTools) SetLinkDown(...string) error             { return errUnsupportedPlatform }
func (n *NetTools) DeleteLink(...string) error              { return errUnsupportedPlatform }
func (n *NetTools) SetMTU(...any) error                     { return errUnsupportedPlatform }
func (n *NetTools) AddAddress(...any) error                 { return errUnsupportedPlatform }
func (n *NetTools) DelAddress(...any) error                 { return errUnsupportedPlatform }
func (n *NetTools) AddAddress6(...any) error                { return errUnsupportedPlatform }
func (n *NetTools) DelAddress6(...any) error                { return errUnsupportedPlatform }
func (n *NetTools) AddRoute(...any) error                   { return errUnsupportedPlatform }
func (n *NetTools) DelRoute(...any) error                   { return errUnsupportedPlatform }
func (n *NetTools) AddRoute6(...any) error                  { return errUnsupportedPlatform }
func (n *NetTools) DelRoute6(...any) error                  { return errUnsupportedPlatform }
func (n *NetTools) AddRouteTable(...any) error              { return errUnsupportedPlatform }
func (n *NetTools) DelRouteTable(...any) error              { return errUnsupportedPlatform }
func (n *NetTools) AddRule(...any) error                    { return errUnsupportedPlatform }
func (n *NetTools) DelRule(...any) error                    { return errUnsupportedPlatform }
func (n *NetTools) AddInputRule(any, int) error             { return errUnsupportedPlatform }
func (n *NetTools) DelInputRule(any, int) error             { return errUnsupportedPlatform }
func (n *NetTools) FlushRules(...any) error                 { return errUnsupportedPlatform }
func (n *NetTools) FlushRulesChecked(int, string) error     { return errUnsupportedPlatform }

func (n *NetTools) CleanConflictRoutes([]string, string, int) {
	panic(errUnsupportedPlatform)
}

func (n *NetTools) CleanConflictRoute(*net.IPNet) error { return errUnsupportedPlatform }
func (n *NetTools) SetSysctl(string, string) error      { return errUnsupportedPlatform }

func (n *NetTools) EnsureIPv6Enabled(string) ([]string, error) {
	return nil, errUnsupportedPlatform
}
