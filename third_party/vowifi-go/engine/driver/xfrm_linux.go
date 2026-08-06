//go:build linux

package driver

import (
	"net"
	"time"

	"github.com/vishvananda/netlink"
)

// XFRMManager installs and removes kernel XFRM SAs/SPs for the SWu tunnel.
type XFRMManager struct {
	undo []func() error
}

// NewXFRMManager creates an empty XFRM manager.
func NewXFRMManager() *XFRMManager {
	return &XFRMManager{}
}

// AddSA installs an XFRM state (SA).
func (m *XFRMManager) AddSA(sa *netlink.XfrmState) error {
	if err := netlink.XfrmStateAdd(sa); err != nil {
		return &NetToolError{Op: "add xfrm state", Err: err}
	}
	m.undo = append(m.undo, func() error { return netlink.XfrmStateDel(sa) })
	return nil
}

// UpdateSA updates an existing XFRM state.
func (m *XFRMManager) UpdateSA(sa *netlink.XfrmState) error {
	if err := netlink.XfrmStateUpdate(sa); err != nil {
		return &NetToolError{Op: "update xfrm state", Err: err}
	}
	return nil
}

// DelSA removes an XFRM state.
func (m *XFRMManager) DelSA(sa *netlink.XfrmState) error {
	if err := netlink.XfrmStateDel(sa); err != nil {
		return &NetToolError{Op: "del xfrm state", Err: err}
	}
	return nil
}

// AddSP installs an XFRM policy (SP).
func (m *XFRMManager) AddSP(sp *netlink.XfrmPolicy) error {
	if err := netlink.XfrmPolicyAdd(sp); err != nil {
		return &NetToolError{Op: "add xfrm policy", Err: err}
	}
	m.undo = append(m.undo, func() error { return netlink.XfrmPolicyDel(sp) })
	return nil
}

// UpdateSP updates an existing XFRM policy.
func (m *XFRMManager) UpdateSP(sp *netlink.XfrmPolicy) error {
	if err := netlink.XfrmPolicyUpdate(sp); err != nil {
		return &NetToolError{Op: "update xfrm policy", Err: err}
	}
	return nil
}

// DelSP removes an XFRM policy.
func (m *XFRMManager) DelSP(sp *netlink.XfrmPolicy) error {
	if err := netlink.XfrmPolicyDel(sp); err != nil {
		return &NetToolError{Op: "del xfrm policy", Err: err}
	}
	return nil
}

// AddXFRMInterface creates an XFRM interface (ip link add xfrmif type xfrm).
func (m *XFRMManager) AddXFRMInterface(name string, ifID uint32) error {
	link := &netlink.Xfrmi{
		LinkAttrs: netlink.LinkAttrs{Name: name},
		Ifid:      ifID,
	}
	if err := netlink.LinkAdd(link); err != nil {
		return &NetToolError{Op: "add xfrm interface", Err: err}
	}
	m.undo = append(m.undo, func() error { return netlink.LinkDel(link) })
	return nil
}

// DelXFRMInterface removes an XFRM interface.
func (m *XFRMManager) DelXFRMInterface(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return &NetToolError{Op: "get xfrm interface", Err: err}
	}
	if err := netlink.LinkDel(link); err != nil {
		return &NetToolError{Op: "del xfrm interface", Err: err}
	}
	return nil
}

// FlushAll removes all XFRM states and policies.
func (m *XFRMManager) FlushAll() error {
	if err := netlink.XfrmStateFlush(netlink.XFRM_PROTO_ESP); err != nil {
		return &NetToolError{Op: "flush xfrm states", Err: err}
	}
	if err := netlink.XfrmPolicyFlush(); err != nil {
		return &NetToolError{Op: "flush xfrm policies", Err: err}
	}
	return nil
}

// FlushByIP removes all XFRM states/policies involving the given address.
func (m *XFRMManager) FlushByIP(ip net.IP) error {
	states, err := netlink.XfrmStateList(netlink.FAMILY_ALL)
	if err != nil {
		return &NetToolError{Op: "list xfrm states", Err: err}
	}
	for _, s := range states {
		if s.Src.Equal(ip) || s.Dst.Equal(ip) {
			if err := netlink.XfrmStateDel(&s); err != nil {
				return &NetToolError{Op: "del xfrm state by ip", Err: err}
			}
		}
	}
	policies, err := netlink.XfrmPolicyList(netlink.FAMILY_ALL)
	if err != nil {
		return &NetToolError{Op: "list xfrm policies", Err: err}
	}
	for _, p := range policies {
		if p.Src != nil && p.Src.IP.Equal(ip) || p.Dst != nil && p.Dst.IP.Equal(ip) {
			if err := netlink.XfrmPolicyDel(&p); err != nil {
				return &NetToolError{Op: "del xfrm policy by ip", Err: err}
			}
		}
	}
	return nil
}

// GetSALastUsed returns the last-used time of an XFRM state.
func (m *XFRMManager) GetSALastUsed(sa *netlink.XfrmState) (time.Time, error) {
	got, err := netlink.XfrmStateGet(sa)
	if err != nil {
		return time.Time{}, &NetToolError{Op: "get xfrm state", Err: err}
	}
	return time.Unix(int64(got.Statistics.UseTime), 0), nil
}

// Cleanup runs all recorded undo functions (removes installed SAs/SPs).
func (m *XFRMManager) Cleanup() error {
	for i := len(m.undo) - 1; i >= 0; i-- {
		if err := m.undo[i](); err != nil {
			return err
		}
	}
	m.undo = nil
	return nil
}

// UndoFuncs returns the recorded undo functions.
func (m *XFRMManager) UndoFuncs() []func() error {
	return m.undo
}

// addStateCompat is a compatibility helper for building an XfrmState.
func (m *XFRMManager) addStateCompat(sa *netlink.XfrmState) error {
	return m.AddSA(sa)
}

// buildXfrmState builds an XfrmState from the given parameters.
func (m *XFRMManager) buildXfrmState(src, dst net.IP, spi uint32, proto netlink.Proto, mode netlink.Mode, key []byte, alg string) *netlink.XfrmState {
	return &netlink.XfrmState{
		Src:   src,
		Dst:   dst,
		Spi:   int(spi),
		Proto: proto,
		Mode:  mode,
		Crypt: &netlink.XfrmStateAlgo{Name: alg, Key: key},
	}
}
