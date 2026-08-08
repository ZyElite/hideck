//go:build linux

package driver

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/iniwex5/netlink"
	"go.uber.org/multierr"
)

const defaultAddReplayWindow = 32
const defaultUpdateReplayWindow = 128

func (x *XFRMManager) FlushAll() {
	_ = netlink.XfrmStateFlush(0)
	_ = netlink.XfrmPolicyFlush()
}

func (x *XFRMManager) FlushAllChecked() error {
	return multierr.Combine(netlink.XfrmStateFlush(0), netlink.XfrmPolicyFlush())
}

func (x *XFRMManager) AddXFRMInterface(name string, ifID uint32, underlyingIndex ...int) error {
	if len(underlyingIndex) > 1 {
		return fmt.Errorf("AddXFRMInterface: expected at most one underlying index")
	}
	if err := deleteLinkIfExists(name); err != nil {
		return err
	}
	link := &netlink.Xfrmi{LinkAttrs: netlink.LinkAttrs{Name: name}, Ifid: ifID}
	if len(underlyingIndex) == 1 && underlyingIndex[0] > 0 {
		link.LinkAttrs.ParentIndex = underlyingIndex[0]
	}
	if err := netlink.LinkAdd(link); err != nil {
		return fmt.Errorf("创建 XFRM 接口 %s 失败: %v", name, err)
	}
	x.undos = append(x.undos, func() error { return x.DelXFRMInterface(name) })
	return nil
}

func (x *XFRMManager) DelXFRMInterface(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("查找 XFRM 接口 %s 失败: %w", name, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("删除 XFRM 接口 %s 失败: %v", name, err)
	}
	return nil
}

func (x *XFRMManager) AddSA(configuration any) error {
	switch value := configuration.(type) {
	case XFRMSAConfig:
		return x.addSAConfig(value)
	case *netlink.XfrmState:
		if value == nil {
			return fmt.Errorf("XFRM state is nil")
		}
		if err := x.addStateCompat(value); err != nil {
			return err
		}
		x.undos = append(x.undos, func() error { return netlink.XfrmStateDel(value) })
		return nil
	default:
		return fmt.Errorf("AddSA: unsupported configuration type %T", configuration)
	}
}

func (x *XFRMManager) addSAConfig(config XFRMSAConfig) error {
	state := buildAddXfrmState(config)
	if err := x.addStateCompat(state); err != nil {
		return fmt.Errorf(
			"添加 XFRM SA (spi=0x%x src=%v dst=%v) 失败: %v", config.SPI, config.Src, config.Dst, err,
		)
	}
	x.undos = append(x.undos, func() error {
		return x.DelSA(config.SPI, config.Src, config.Dst, config.Proto)
	})
	return nil
}

func buildAddXfrmState(config XFRMSAConfig) *netlink.XfrmState {
	replayWindow := config.ReplayWindow
	if replayWindow <= 0 {
		replayWindow = defaultAddReplayWindow
	}
	state := &netlink.XfrmState{
		Src: config.Src, Dst: config.Dst, Proto: config.Proto, Mode: config.Mode,
		Spi: int(config.SPI), ReplayWindow: replayWindow, Ifid: config.Ifid,
		AFUnspec: config.Mode == netlink.XFRM_MODE_TUNNEL, ESN: config.ESN, SADir: config.SADir,
		Limits: netlink.XfrmStateLimits{TimeSoft: config.TimeLimitSoft, TimeHard: config.TimeLimitHard},
	}
	applyAddStateAlgorithms(state, config)
	applyStateEncapsulation(state, config)
	return state
}

func (x *XFRMManager) addStateCompat(state *netlink.XfrmState) error {
	err := netlink.XfrmStateAdd(state)
	if err == nil || !errors.Is(err, syscall.EINVAL) {
		return err
	}
	attempts := compatibleStates(state)
	lastErr := err
	for _, attempt := range attempts {
		lastErr = netlink.XfrmStateAdd(attempt)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func compatibleStates(state *netlink.XfrmState) []*netlink.XfrmState {
	var attempts []*netlink.XfrmState
	if state.SADir != 0 {
		withoutDirection := *state
		withoutDirection.SADir = 0
		attempts = append(attempts, &withoutDirection)
	}
	if state.AFUnspec {
		withoutFamilyFlag := *state
		withoutFamilyFlag.AFUnspec = false
		attempts = append(attempts, &withoutFamilyFlag)
	}
	if state.SADir != 0 && state.AFUnspec {
		withoutExtensions := *state
		withoutExtensions.SADir = 0
		withoutExtensions.AFUnspec = false
		attempts = append(attempts, &withoutExtensions)
	}
	return attempts
}

func (x *XFRMManager) DelSA(arguments ...any) error {
	state, description, err := deleteStateArguments(arguments)
	if err != nil {
		return err
	}
	if err := netlink.XfrmStateDel(state); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("删除 XFRM SA (%s) 失败: %v", description, err)
	}
	return nil
}

func deleteStateArguments(arguments []any) (*netlink.XfrmState, string, error) {
	if len(arguments) == 1 {
		state, ok := arguments[0].(*netlink.XfrmState)
		if !ok || state == nil {
			return nil, "", fmt.Errorf("DelSA: expected *netlink.XfrmState")
		}
		return state, fmt.Sprintf("spi=0x%x", state.Spi), nil
	}
	if len(arguments) != 4 {
		return nil, "", fmt.Errorf("DelSA: expected SPI, source, destination, and protocol")
	}
	spi, spiOK := arguments[0].(uint32)
	source, sourceOK := arguments[1].(net.IP)
	destination, destinationOK := arguments[2].(net.IP)
	protocol, protocolOK := arguments[3].(netlink.Proto)
	if !spiOK || !sourceOK || !destinationOK || !protocolOK {
		return nil, "", fmt.Errorf("DelSA: invalid arguments")
	}
	return &netlink.XfrmState{
		Src: source, Dst: destination, Proto: protocol, Spi: int(spi),
	}, fmt.Sprintf("spi=0x%x", spi), nil
}

func (x *XFRMManager) FlushByIP(ip net.IP) {
	_ = x.flushByIP(ip)
}

func (x *XFRMManager) FlushByIPChecked(ip net.IP) error { return x.flushByIP(ip) }

func (x *XFRMManager) flushByIP(ip net.IP) error {
	if ip == nil {
		return nil
	}
	states, err := netlink.XfrmStateList(netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	var result error
	for index := range states {
		if states[index].Src.Equal(ip) || states[index].Dst.Equal(ip) {
			result = multierr.Append(result, netlink.XfrmStateDel(&states[index]))
		}
	}
	return result
}

func (x *XFRMManager) GetSALastUsed(
	spi uint32,
	source, destination net.IP,
	protocol netlink.Proto,
) (uint64, error) {
	state := &netlink.XfrmState{Src: source, Dst: destination, Proto: protocol, Spi: int(spi)}
	result, err := netlink.XfrmStateGet(state)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取 XFRM SA (spi=0x%x) 状态失败: %v", spi, err)
	}
	return result.Statistics.UseTime, nil
}

func (x *XFRMManager) GetStateLastUsed(state *netlink.XfrmState) (time.Time, error) {
	result, err := netlink.XfrmStateGet(state)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(result.Statistics.UseTime), 0), nil
}
