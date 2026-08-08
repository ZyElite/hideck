//go:build linux

package driver

import (
	"fmt"

	"github.com/iniwex5/netlink"
)

func (x *XFRMManager) UpdateSA(configuration any) error {
	var state *netlink.XfrmState
	switch value := configuration.(type) {
	case XFRMSAConfig:
		state = x.buildXfrmState(value)
	case *netlink.XfrmState:
		state = value
	default:
		return fmt.Errorf("UpdateSA: unsupported configuration type %T", configuration)
	}
	if state == nil {
		return fmt.Errorf("UpdateSA: XFRM state is nil")
	}
	if err := netlink.XfrmStateUpdate(state); err != nil {
		return fmt.Errorf("更新 XFRM State 失败: %v", err)
	}
	return nil
}

func (x *XFRMManager) buildXfrmState(config XFRMSAConfig) *netlink.XfrmState {
	state := &netlink.XfrmState{
		Src: config.Src, Dst: config.Dst, Proto: config.Proto, Mode: config.Mode,
		Spi: int(config.SPI), Ifid: config.Ifid, ESN: config.ESN,
	}
	applyUpdateStateAlgorithms(state, config)
	applyStateEncapsulation(state, config)
	state.Limits.TimeHard = config.TimeLimitHard
	state.Limits.TimeSoft = config.TimeLimitSoft
	state.ReplayWindow = config.ReplayWindow
	if state.ReplayWindow <= 0 {
		state.ReplayWindow = defaultUpdateReplayWindow
	}
	if config.SADir != 0 {
		state.SADir = config.SADir
	}
	return state
}

func applyAddStateAlgorithms(state *netlink.XfrmState, config XFRMSAConfig) {
	if config.IsAEAD {
		state.Aead = &netlink.XfrmStateAlgo{
			Name: config.AeadAlgoName, Key: config.AeadKey, ICVLen: config.AeadICVLen,
		}
		return
	}
	if config.CryptAlgoName != "" {
		state.Crypt = &netlink.XfrmStateAlgo{Name: config.CryptAlgoName, Key: config.CryptKey}
	}
	if config.AuthAlgoName != "" {
		state.Auth = &netlink.XfrmStateAlgo{
			Name: config.AuthAlgoName, Key: config.AuthKey, TruncateLen: config.AuthTruncLen,
		}
	}
}

func applyUpdateStateAlgorithms(state *netlink.XfrmState, config XFRMSAConfig) {
	if config.IsAEAD {
		state.Aead = &netlink.XfrmStateAlgo{
			Name: config.AeadAlgoName, Key: config.AeadKey, ICVLen: config.AeadICVLen,
		}
		return
	}
	state.Auth = &netlink.XfrmStateAlgo{Name: config.AuthAlgoName, Key: config.AuthKey}
	state.Crypt = &netlink.XfrmStateAlgo{Name: config.CryptAlgoName, Key: config.CryptKey}
}

func applyStateEncapsulation(state *netlink.XfrmState, config XFRMSAConfig) {
	if config.EncapType == 0 {
		return
	}
	state.Encap = &netlink.XfrmStateEncap{
		Type: config.EncapType, SrcPort: config.EncapSrcPort, DstPort: config.EncapDstPort,
	}
}
