package swu

import (
	"errors"
	"strings"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/driver"
)

type xfrmManager interface {
	AddXFRMInterface(string, uint32, ...int) error
	DelXFRMInterface(string) error
	AddSA(any) error
	UpdateSA(any) error
	DelSA(...any) error
	AddSP(any) error
	UpdateSP(any) error
	DelSP(any) error
	CleanupChecked() error
}

func configuredNetTools(config *Config) NetTools {
	if config != nil && config.NetTools != nil {
		return config.NetTools
	}
	return driver.NewNetTools()
}

func configuredDataplaneMode(config *Config) (string, error) {
	if config == nil {
		return DataplaneModeUserspace, nil
	}
	if strings.TrimSpace(config.DataplaneMode) == "" && config.EnableDriver {
		return DataplaneModeTUN, nil
	}
	return normalizeDataplaneMode(config.DataplaneMode)
}

func configuredXFRMESN(config *Config) bool {
	if config == nil || !config.EnableESN {
		return false
	}
	mode, err := configuredDataplaneMode(config)
	return err == nil && mode == DataplaneModeXFRMI
}

type legacyNetTxn struct {
	mu    sync.Mutex
	net   NetTools
	undos []func() error
}

func newLegacyNetTxn(network NetTools) *legacyNetTxn {
	return &legacyNetTxn{net: network}
}

func (tx *legacyNetTxn) SetLinkUp(iface string) error {
	if err := tx.net.SetLinkUp(iface); err != nil {
		return err
	}
	if down, ok := tx.net.(interface{ SetLinkDown(string) error }); ok {
		tx.addUndo(func() error { return down.SetLinkDown(iface) })
	}
	return nil
}

func (tx *legacyNetTxn) SetMTU(iface string, mtu int) error {
	return tx.net.SetMTU(iface, mtu)
}

func (tx *legacyNetTxn) AddAddress(iface, cidr string) error {
	if err := tx.net.AddAddress(iface, cidr); err != nil {
		return err
	}
	if deleter, ok := tx.net.(interface{ DelAddress(string, string) error }); ok {
		tx.addUndo(func() error { return deleter.DelAddress(iface, cidr) })
	}
	return nil
}

func (tx *legacyNetTxn) AddAddress6(iface, cidr string) error {
	if err := tx.net.AddAddress6(iface, cidr); err != nil {
		return err
	}
	if deleter, ok := tx.net.(interface{ DelAddress6(string, string) error }); ok {
		tx.addUndo(func() error { return deleter.DelAddress6(iface, cidr) })
	}
	return nil
}

func (tx *legacyNetTxn) AddRoute(cidr, gateway, iface string, ipv6 bool) error {
	add := tx.net.AddRoute
	if ipv6 {
		add = tx.net.AddRoute6
	}
	if err := add(cidr, gateway, iface); err != nil {
		return err
	}
	tx.addRouteUndo(cidr, gateway, iface, ipv6)
	return nil
}

func (tx *legacyNetTxn) addRouteUndo(cidr, gateway, iface string, ipv6 bool) {
	if ipv6 {
		if deleter, ok := tx.net.(interface {
			DelRoute6(string, string, string) error
		}); ok {
			tx.addUndo(func() error { return deleter.DelRoute6(cidr, gateway, iface) })
		}
		return
	}
	if deleter, ok := tx.net.(interface {
		DelRoute(string, string, string) error
	}); ok {
		tx.addUndo(func() error { return deleter.DelRoute(cidr, gateway, iface) })
	}
}

func (tx *legacyNetTxn) EnsureIPv6Enabled(iface string) error {
	if enabler, ok := tx.net.(interface {
		EnsureIPv6Enabled(string) ([]string, error)
	}); ok {
		_, err := enabler.EnsureIPv6Enabled(iface)
		return err
	}
	if enabler, ok := tx.net.(interface{ EnsureIPv6Enabled(string) error }); ok {
		return enabler.EnsureIPv6Enabled(iface)
	}
	return nil
}

func (tx *legacyNetTxn) Rollback() error {
	tx.mu.Lock()
	undos := tx.undos
	tx.undos = nil
	tx.mu.Unlock()
	var result error
	for index := len(undos) - 1; index >= 0; index-- {
		result = errors.Join(result, undos[index]())
	}
	return result
}

func (tx *legacyNetTxn) addUndo(undo func() error) {
	tx.mu.Lock()
	tx.undos = append(tx.undos, undo)
	tx.mu.Unlock()
}
