//go:build linux

package driver

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/iniwex5/netlink"
	"go.uber.org/multierr"
)

func (x *XFRMManager) AddSP(configuration any) error {
	switch value := configuration.(type) {
	case XFRMSPConfig:
		return x.addSPConfig(value)
	case *netlink.XfrmPolicy:
		if value == nil {
			return fmt.Errorf("XFRM policy is nil")
		}
		if err := netlink.XfrmPolicyAdd(value); err != nil {
			return err
		}
		x.undos = append(x.undos, func() error { return netlink.XfrmPolicyDel(value) })
		return nil
	default:
		return fmt.Errorf("AddSP: unsupported configuration type %T", configuration)
	}
}

func (x *XFRMManager) addSPConfig(config XFRMSPConfig) error {
	policy := &netlink.XfrmPolicy{
		Src: config.Src, Dst: config.Dst, Dir: config.Dir, Ifid: config.Ifid,
		Tmpls: []netlink.XfrmPolicyTmpl{{
			Src: normalizeIPv4(config.TmplSrc), Dst: normalizeIPv4(config.TmplDst),
			Proto: config.TmplProto, Mode: config.TmplMode, Spi: config.TmplSPI,
		}},
	}
	if err := netlink.XfrmPolicyUpdate(policy); err != nil {
		return fmt.Errorf(
			"添加/更新 XFRM SP (dir=%s src=%v dst=%v) 失败: %v", config.Dir, config.Src, config.Dst, err,
		)
	}
	x.undos = append(x.undos, func() error { return x.DelSP(config) })
	return nil
}

func normalizeIPv4(ip net.IP) net.IP {
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4
	}
	return ip
}

func (x *XFRMManager) DelSP(configuration any) error {
	policy, description, err := deletePolicyArgument(configuration)
	if err != nil {
		return err
	}
	if err := netlink.XfrmPolicyDel(policy); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("删除 XFRM SP (%s) 失败: %v", description, err)
	}
	return nil
}

func deletePolicyArgument(configuration any) (*netlink.XfrmPolicy, string, error) {
	switch value := configuration.(type) {
	case XFRMSPConfig:
		return &netlink.XfrmPolicy{
			Src: value.Src, Dst: value.Dst, Dir: value.Dir, Ifid: value.Ifid,
		}, fmt.Sprintf("dir=%s", value.Dir), nil
	case *netlink.XfrmPolicy:
		if value == nil {
			return nil, "", fmt.Errorf("DelSP: XFRM policy is nil")
		}
		return value, fmt.Sprintf("dir=%s", value.Dir), nil
	default:
		return nil, "", fmt.Errorf("DelSP: unsupported configuration type %T", configuration)
	}
}

func (x *XFRMManager) UpdateSP(configuration any) error {
	var policy *netlink.XfrmPolicy
	switch value := configuration.(type) {
	case XFRMSPConfig:
		policy = x.buildXfrmPolicy(value)
	case *netlink.XfrmPolicy:
		policy = value
	default:
		return fmt.Errorf("UpdateSP: unsupported configuration type %T", configuration)
	}
	if policy == nil {
		return fmt.Errorf("UpdateSP: XFRM policy is nil")
	}
	if err := netlink.XfrmPolicyUpdate(policy); err != nil {
		return fmt.Errorf("更新 XFRM Policy 失败: %v", err)
	}
	return nil
}

func (x *XFRMManager) buildXfrmPolicy(config XFRMSPConfig) *netlink.XfrmPolicy {
	return &netlink.XfrmPolicy{
		Src: config.Src, Dst: config.Dst, Dir: config.Dir, Ifid: config.Ifid,
		Tmpls: []netlink.XfrmPolicyTmpl{{
			Src: config.TmplSrc, Dst: config.TmplDst, Proto: config.TmplProto,
			Mode: config.TmplMode, Spi: config.TmplSPI,
		}},
	}
}

func (x *XFRMManager) Cleanup() {
	_ = x.cleanup()
}

func (x *XFRMManager) CleanupChecked() error { return x.cleanup() }

func (x *XFRMManager) cleanup() error {
	var result error
	for index := len(x.undos) - 1; index >= 0; index-- {
		result = multierr.Append(result, x.undos[index]())
	}
	x.undos = nil
	return result
}

func (x *XFRMManager) UndoFuncs() []func() error { return x.undos }
