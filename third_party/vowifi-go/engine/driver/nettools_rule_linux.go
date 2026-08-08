//go:build linux

package driver

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/iniwex5/netlink"
	"golang.org/x/sys/unix"
)

func (n *NetTools) AddRule(arguments ...any) error {
	if len(arguments) == 1 {
		rule, ok := arguments[0].(*netlink.Rule)
		if !ok || rule == nil {
			return fmt.Errorf("AddRule: expected *netlink.Rule")
		}
		return wrapErr("rule add", "", netlink.RuleAdd(rule))
	}
	source, table, err := ruleArguments(arguments)
	if err != nil {
		return err
	}
	rule, err := sourceRule(source, table)
	if err != nil {
		return wrapErr("rule add", source, err)
	}
	removeRulesMatchingSource(rule.Family, rule.Src)
	err = netlink.RuleAdd(rule)
	if isRuleExists(err) {
		return nil
	}
	return wrapErr("rule add", fmt.Sprintf("from %s lookup %d", source, table), err)
}

func (n *NetTools) DelRule(arguments ...any) error {
	if len(arguments) == 1 {
		rule, ok := arguments[0].(*netlink.Rule)
		if !ok || rule == nil {
			return fmt.Errorf("DelRule: expected *netlink.Rule")
		}
		return wrapErr("rule del", "", netlink.RuleDel(rule))
	}
	source, table, err := ruleArguments(arguments)
	if err != nil {
		return err
	}
	rule, err := sourceRule(source, table)
	if err != nil {
		return wrapErr("rule del", source, err)
	}
	rule.Family = 0
	err = netlink.RuleDel(rule)
	if isRouteNotFound(err) {
		return nil
	}
	return wrapErr("rule del", fmt.Sprintf("from %s lookup %d", source, table), err)
}

func ruleArguments(arguments []any) (string, int, error) {
	if len(arguments) != 2 {
		return "", 0, fmt.Errorf("rule operation expects source CIDR and table")
	}
	source, sourceOK := arguments[0].(string)
	table, tableOK := arguments[1].(int)
	if !sourceOK || !tableOK {
		return "", 0, fmt.Errorf("rule operation expects (string, int)")
	}
	return source, table, nil
}

func sourceRule(source string, table int) (*netlink.Rule, error) {
	_, sourceNet, err := net.ParseCIDR(source)
	if err != nil {
		return nil, fmt.Errorf("解析源地址失败: %v", err)
	}
	rule := netlink.NewRule()
	rule.Src = sourceNet
	rule.Table = table
	rule.Family = netlink.FAMILY_V4
	if sourceNet.IP.To4() == nil {
		rule.Family = netlink.FAMILY_V6
	}
	return rule, nil
}

func removeRulesMatchingSource(family int, source *net.IPNet) {
	rules, err := netlink.RuleList(family)
	if err != nil {
		return
	}
	for index := range rules {
		if rules[index].Src != nil && rules[index].Src.String() == source.String() {
			_ = netlink.RuleDel(&rules[index])
		}
	}
}

func (n *NetTools) AddInputRule(first any, table int) error {
	if iface, ok := first.(string); ok {
		return addInputRules(iface, table)
	}
	mark, ok := first.(int)
	if !ok {
		return fmt.Errorf("AddInputRule: expected interface name or mark")
	}
	rule := netlink.NewRule()
	rule.Mark = uint32(mark)
	rule.Table = table
	rule.Priority = 1000
	return n.AddRule(rule)
}

func addInputRules(iface string, table int) error {
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		removeRulesMatchingInterface(family, iface)
		rule := netlink.NewRule()
		rule.IifName = iface
		rule.Table = table
		rule.Family = family
		if err := netlink.RuleAdd(rule); err != nil && !isRuleExists(err) {
			return wrapErr(
				fmt.Sprintf("rule add v%d", familyNumber(family)),
				fmt.Sprintf("iif %s lookup %d", iface, table), err,
			)
		}
	}
	return nil
}

func (n *NetTools) DelInputRule(first any, table int) error {
	if iface, ok := first.(string); ok {
		rule := netlink.NewRule()
		rule.IifName = iface
		rule.Table = table
		err := netlink.RuleDel(rule)
		if isRouteNotFound(err) {
			return nil
		}
		return wrapErr("rule del", fmt.Sprintf("iif %s lookup %d", iface, table), err)
	}
	mark, ok := first.(int)
	if !ok {
		return fmt.Errorf("DelInputRule: expected interface name or mark")
	}
	rule := netlink.NewRule()
	rule.Mark = uint32(mark)
	rule.Table = table
	rule.Priority = 1000
	return n.DelRule(rule)
}

func removeRulesMatchingInterface(family int, iface string) {
	rules, err := netlink.RuleList(family)
	if err != nil {
		return
	}
	for index := range rules {
		if rules[index].IifName == iface {
			_ = netlink.RuleDel(&rules[index])
		}
	}
}

func familyNumber(family int) int {
	if family == netlink.FAMILY_V6 {
		return 6
	}
	return 4
}

func (n *NetTools) FlushRules(arguments ...any) error {
	return n.flushRules(arguments, false)
}

// FlushRulesChecked reports every list/delete failure instead of preserving
// the original best-effort cleanup behavior.
func (n *NetTools) FlushRulesChecked(table int, iface string) error {
	return n.flushRules([]any{table, iface}, true)
}

func (n *NetTools) flushRules(arguments []any, checked bool) error {
	if len(arguments) == 0 {
		return flushInterimRules()
	}
	if len(arguments) != 2 {
		return fmt.Errorf("FlushRules: expected table and interface")
	}
	table, tableOK := arguments[0].(int)
	iface, ifaceOK := arguments[1].(string)
	if !tableOK || !ifaceOK {
		return fmt.Errorf("FlushRules: expected (int, string)")
	}
	var result error
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		rules, err := netlink.RuleList(family)
		if err != nil {
			if checked {
				result = errors.Join(result, err)
			}
			continue
		}
		for index := range rules {
			if rules[index].Table == table || rules[index].IifName == iface {
				deleteErr := netlink.RuleDel(&rules[index])
				if checked && !isRouteNotFound(deleteErr) {
					result = errors.Join(result, deleteErr)
				}
			}
		}
	}
	return result
}

func flushInterimRules() error {
	rules, err := netlink.RuleList(netlink.FAMILY_ALL)
	if err != nil {
		return wrapErr("list rules", "", err)
	}
	for index := range rules {
		if rules[index].Priority < 1000 {
			continue
		}
		if err := netlink.RuleDel(&rules[index]); err != nil {
			return wrapErr("flush rule", "", err)
		}
	}
	return nil
}

func (n *NetTools) CleanConflictRoutes(cidrs []string, keepIface string, family int) {
	keepLink, _ := getLink(keepIface)
	keepIndex := 0
	if keepLink != nil {
		keepIndex = keepLink.Attrs().Index
	}
	for _, cidr := range cidrs {
		if cidr == "::/0" || cidr == "0.0.0.0/0" {
			continue
		}
		destination := conflictDestination(cidr)
		if destination == nil {
			continue
		}
		filter := &netlink.Route{Dst: destination, Table: unix.RT_TABLE_MAIN}
		routes, err := netlink.RouteListFiltered(
			family, filter, netlink.RT_FILTER_DST|netlink.RT_FILTER_TABLE,
		)
		if err != nil {
			continue
		}
		for index := range routes {
			if routes[index].LinkIndex != keepIndex {
				_ = netlink.RouteDel(&routes[index])
			}
		}
	}
}

func conflictDestination(cidr string) *net.IPNet {
	_, destination, err := net.ParseCIDR(cidr)
	if err == nil {
		return destination
	}
	target := strings.TrimSuffix(strings.TrimSuffix(cidr, "/128"), "/32")
	ip := net.ParseIP(target)
	if ip == nil {
		return nil
	}
	bits := net.IPv6len * 8
	if ip.To4() != nil {
		bits = net.IPv4len * 8
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
}

func (n *NetTools) CleanConflictRoute(destination *net.IPNet) error {
	if destination == nil {
		return fmt.Errorf("destination is required")
	}
	routes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return wrapErr("list routes", "", err)
	}
	for index := range routes {
		if routes[index].Dst == nil || routes[index].Dst.String() != destination.String() {
			continue
		}
		if err := netlink.RouteDel(&routes[index]); err != nil {
			return wrapErr("clean conflict route", destination.String(), err)
		}
	}
	return nil
}
