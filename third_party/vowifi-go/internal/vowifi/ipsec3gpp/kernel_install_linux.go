//go:build linux

package ipsec3gpp

import "github.com/iniwex5/netlink"

type netlinkKernelOperations struct{}

func InstallPolicy(policy Policy) (func() error, error) {
	plan, err := buildKernelPlan(policy)
	if err != nil {
		return nil, err
	}
	return installKernelPlan(netlinkKernelOperations{}, plan)
}

func (netlinkKernelOperations) deleteState(state kernelState) error {
	return netlink.XfrmStateDel(toXFRMState(state))
}

func (netlinkKernelOperations) addState(state kernelState) error {
	return netlink.XfrmStateAdd(toXFRMState(state))
}

func (netlinkKernelOperations) deletePolicy(policy kernelPolicy) error {
	return netlink.XfrmPolicyDel(toXFRMPolicy(policy))
}

func (netlinkKernelOperations) addPolicy(policy kernelPolicy) error {
	return netlink.XfrmPolicyAdd(toXFRMPolicy(policy))
}

func toXFRMState(state kernelState) *netlink.XfrmState {
	return &netlink.XfrmState{
		Src: state.source, Dst: state.destination, Spi: int(state.spi),
		Proto: netlink.XFRM_PROTO_ESP, Mode: netlink.XFRM_MODE_TRANSPORT, ReplayWindow: 32,
		Auth: &netlink.XfrmStateAlgo{
			Name: state.auth.name, Key: append([]byte(nil), state.auth.key...), TruncateLen: state.auth.truncateLen,
		},
		Crypt: &netlink.XfrmStateAlgo{Name: state.crypt.name, Key: append([]byte(nil), state.crypt.key...)},
		Selector: &netlink.XfrmPolicy{
			Src: state.selectorSource, Dst: state.selectorDestination,
			SrcPort: state.sourcePort, DstPort: state.destinationPort,
		},
	}
}

func toXFRMPolicy(policy kernelPolicy) *netlink.XfrmPolicy {
	direction := netlink.XFRM_DIR_OUT
	if policy.inbound {
		direction = netlink.XFRM_DIR_IN
	}
	return &netlink.XfrmPolicy{
		Src: policy.source, Dst: policy.destination, Proto: netlink.Proto(protocolTCP),
		SrcPort: policy.sourcePort, DstPort: policy.destinationPort, Dir: direction,
		Tmpls: []netlink.XfrmPolicyTmpl{{
			Src: policy.templateSource, Dst: policy.templateDestination,
			Proto: netlink.XFRM_PROTO_ESP, Mode: netlink.XFRM_MODE_TRANSPORT, Spi: int(policy.spi),
		}},
	}
}
