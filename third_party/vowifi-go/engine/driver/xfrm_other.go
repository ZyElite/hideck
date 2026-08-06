//go:build !linux

package driver

import (
	"net"
	"time"
)

// XFRMManager is a non-Linux XFRM manager stub.
type XFRMManager struct{}

// NewXFRMManager creates an empty XFRM manager.
func NewXFRMManager() *XFRMManager { return &XFRMManager{} }

// AddSA is unsupported off Linux.
func (m *XFRMManager) AddSA(sa interface{}) error { return errUnsupportedPlatform }

// UpdateSA is unsupported off Linux.
func (m *XFRMManager) UpdateSA(sa interface{}) error { return errUnsupportedPlatform }

// DelSA is unsupported off Linux.
func (m *XFRMManager) DelSA(sa interface{}) error { return errUnsupportedPlatform }

// AddSP is unsupported off Linux.
func (m *XFRMManager) AddSP(sp interface{}) error { return errUnsupportedPlatform }

// UpdateSP is unsupported off Linux.
func (m *XFRMManager) UpdateSP(sp interface{}) error { return errUnsupportedPlatform }

// DelSP is unsupported off Linux.
func (m *XFRMManager) DelSP(sp interface{}) error { return errUnsupportedPlatform }

// AddXFRMInterface is unsupported off Linux.
func (m *XFRMManager) AddXFRMInterface(name string, ifID uint32) error {
	return errUnsupportedPlatform
}

// DelXFRMInterface is unsupported off Linux.
func (m *XFRMManager) DelXFRMInterface(name string) error { return errUnsupportedPlatform }

// FlushAll is unsupported off Linux.
func (m *XFRMManager) FlushAll() error { return errUnsupportedPlatform }

// FlushByIP is unsupported off Linux.
func (m *XFRMManager) FlushByIP(ip net.IP) error { return errUnsupportedPlatform }

// GetSALastUsed is unsupported off Linux.
func (m *XFRMManager) GetSALastUsed(sa interface{}) (time.Time, error) {
	return time.Time{}, errUnsupportedPlatform
}

// Cleanup is a no-op off Linux.
func (m *XFRMManager) Cleanup() error { return nil }

// UndoFuncs returns nil off Linux.
func (m *XFRMManager) UndoFuncs() []func() error { return nil }

// addStateCompat is a no-op off Linux.
func (m *XFRMManager) addStateCompat(sa interface{}) error { return errUnsupportedPlatform }

// buildXfrmState is a no-op off Linux.
func (m *XFRMManager) buildXfrmState(src, dst net.IP, spi uint32, proto, mode interface{}, key []byte, alg string) interface{} {
	return nil
}
