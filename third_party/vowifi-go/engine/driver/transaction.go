package driver

import (
	"errors"

	"go.uber.org/multierr"
)

type NetTxn struct {
	net   *NetTools
	undos []func() error
}

func (n *NetTools) Begin() *NetTxn {
	return &NetTxn{net: n}
}

func (tx *NetTxn) Commit() {
	tx.undos = nil
}

func (tx *NetTxn) Rollback() error {
	var result error
	for index := len(tx.undos) - 1; index >= 0; index-- {
		result = multierr.Append(result, tx.undos[index]())
	}
	tx.undos = nil
	return result
}

func (tx *NetTxn) SetLinkUp(iface string) error {
	if err := tx.net.SetLinkUp(iface); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.SetLinkDown(iface)
	})
	return nil
}

func (tx *NetTxn) SetMTU(iface string, mtu int) error {
	return tx.net.SetMTU(iface, mtu)
}

func (tx *NetTxn) AddAddress(iface, cidr string) error {
	if err := tx.net.AddAddress(iface, cidr); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.DelAddress(iface, cidr)
	})
	return nil
}

func (tx *NetTxn) AddRoute(cidr, gateway, iface string) error {
	if err := tx.net.AddRoute(cidr, gateway, iface); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.DelRoute(cidr, gateway, iface)
	})
	return nil
}

func (tx *NetTxn) AddAddress6(iface, cidr string) error {
	if err := tx.net.AddAddress6(iface, cidr); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.DelAddress(iface, cidr)
	})
	return nil
}

func (tx *NetTxn) AddRoute6(cidr, gateway, iface string) error {
	if err := tx.net.AddRoute6(cidr, gateway, iface); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.DelRoute(cidr, gateway, iface)
	})
	return nil
}

// AddRouteTable adds a route to an isolated routing table and records its
// inverse for rollback. This extends the recovered transaction with the route
// operation already exposed by NetTools.
func (tx *NetTxn) AddRouteTable(cidr, iface string, table int) error {
	if err := tx.net.AddRouteTable(cidr, iface, table); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.DelRouteTable(cidr, iface, table)
	})
	return nil
}

// AddRule adds a source policy rule and records its inverse for rollback.
func (tx *NetTxn) AddRule(sourceCIDR string, table int) error {
	if err := tx.net.AddRule(sourceCIDR, table); err != nil {
		return err
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.DelRule(sourceCIDR, table)
	})
	return nil
}

// AddInputRule adds an input-interface policy rule and records its inverse.
func (tx *NetTxn) AddInputRule(iface string, table int) error {
	if err := tx.net.AddInputRule(iface, table); err != nil {
		return errors.Join(err, tx.net.FlushRulesChecked(table, iface))
	}
	tx.undos = append(tx.undos, func() error {
		return tx.net.FlushRulesChecked(table, iface)
	})
	return nil
}

// EnsureIPv6Enabled enables the recovered IPv6 sysctls and records restoration
// of every value that changed from 1 to 0.
func (tx *NetTxn) EnsureIPv6Enabled(iface string) error {
	changed, err := tx.net.EnsureIPv6Enabled(iface)
	for _, key := range changed {
		key := key
		tx.undos = append(tx.undos, func() error {
			return tx.net.SetSysctl(key, "1")
		})
	}
	return err
}
