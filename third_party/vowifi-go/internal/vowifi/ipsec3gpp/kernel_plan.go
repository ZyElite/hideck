package ipsec3gpp

import (
	"fmt"
	"net"
)

type kernelAlgorithm struct {
	name        string
	key         []byte
	truncateLen int
}

type kernelState struct {
	source, destination                 net.IP
	selectorSource, selectorDestination *net.IPNet
	sourcePort, destinationPort         int
	spi                                 uint32
	auth                                kernelAlgorithm
	crypt                               kernelAlgorithm
}

type kernelPolicy struct {
	source, destination                 *net.IPNet
	templateSource, templateDestination net.IP
	sourcePort, destinationPort         int
	spi                                 uint32
	inbound                             bool
}

type kernelPlan struct {
	states   []kernelState
	policies []kernelPolicy
}

type kernelOperations interface {
	deleteState(kernelState) error
	addState(kernelState) error
	deletePolicy(kernelPolicy) error
	addPolicy(kernelPolicy) error
}

func buildKernelPlan(policy Policy) (kernelPlan, error) {
	policy, err := NewPolicy(policy)
	if err != nil {
		return kernelPlan{}, err
	}
	localIP, remoteIP, prefixBits, err := normalizeIPPair(policy.LocalIP, policy.RemoteIP)
	if err != nil {
		return kernelPlan{}, err
	}
	flowC, err := buildKernelFlow(policy.FlowC)
	if err != nil {
		return kernelPlan{}, err
	}
	localNet := &net.IPNet{IP: localIP, Mask: net.CIDRMask(prefixBits, prefixBits)}
	remoteNet := &net.IPNet{IP: remoteIP, Mask: net.CIDRMask(prefixBits, prefixBits)}
	return kernelPlan{
		states:   append(flowStates(localNet, remoteNet, policy.FlowC, flowC), flowStates(localNet, remoteNet, policy.FlowS, flowC)...),
		policies: append(flowPolicies(localNet, remoteNet, policy.FlowC), flowPolicies(localNet, remoteNet, policy.FlowS)...),
	}, nil
}

type flowAlgorithms struct {
	auth  kernelAlgorithm
	crypt kernelAlgorithm
}

func buildKernelFlow(flow Flow) (flowAlgorithms, error) {
	auth, err := buildAuthAlgo(flow.AuthAlg, flow.IK)
	if err != nil {
		return flowAlgorithms{}, err
	}
	crypt, err := buildCryptAlgo(flow.EncAlg, flow.CK)
	if err != nil {
		return flowAlgorithms{}, err
	}
	return flowAlgorithms{auth: auth, crypt: crypt}, nil
}

func buildAuthAlgo(name string, ik []byte) (kernelAlgorithm, error) {
	switch normalizedAlgorithm(name) {
	case "hmac(md5)", "hmac-md5-96":
		key, err := deriveAuthKey(ik, name)
		if err != nil {
			return kernelAlgorithm{}, err
		}
		return kernelAlgorithm{name: "hmac(md5)", key: key, truncateLen: 96}, nil
	case "hmac(sha1)", "hmac-sha-1-96":
		key, err := deriveAuthKey(ik, name)
		if err != nil {
			return kernelAlgorithm{}, err
		}
		return kernelAlgorithm{name: "hmac(sha1)", key: key, truncateLen: 96}, nil
	default:
		return kernelAlgorithm{}, fmt.Errorf("ipsec3gpp: 不支持的认证算法 %s", name)
	}
}

func buildCryptAlgo(name string, ck []byte) (kernelAlgorithm, error) {
	switch normalizedAlgorithm(name) {
	case "", "aes-cbc", "cbc(aes)":
		key, err := deriveEncKey(ck, name)
		if err != nil {
			return kernelAlgorithm{}, err
		}
		return kernelAlgorithm{name: "cbc(aes)", key: key}, nil
	case "des3-cbc", "des-ede3-cbc", "cbc(des3_ede)":
		key, err := deriveEncKey(ck, name)
		if err != nil {
			return kernelAlgorithm{}, err
		}
		return kernelAlgorithm{name: "cbc(des3_ede)", key: key}, nil
	case "null", "cipher_null", "ecb(cipher_null)":
		return kernelAlgorithm{name: "ecb(cipher_null)"}, nil
	default:
		return kernelAlgorithm{}, fmt.Errorf("ipsec3gpp: 不支持的加密算法 %s", name)
	}
}

func normalizeIPPair(local, remote net.IP) (net.IP, net.IP, int, error) {
	if local4 := local.To4(); local4 != nil {
		remote4 := remote.To4()
		if remote4 == nil {
			return nil, nil, 0, fmt.Errorf("ipsec3gpp: local 为 IPv4 但 remote 不是 IPv4")
		}
		return append(net.IP(nil), local4...), append(net.IP(nil), remote4...), 32, nil
	}
	local16, remote16 := local.To16(), remote.To16()
	if local16 == nil || remote16 == nil {
		return nil, nil, 0, fmt.Errorf("ipsec3gpp: 无法识别IP地址族")
	}
	if remote.To4() != nil {
		return nil, nil, 0, fmt.Errorf("ipsec3gpp: local/remote 地址族不一致")
	}
	return append(net.IP(nil), local16...), append(net.IP(nil), remote16...), 128, nil
}

func flowStates(localNet, remoteNet *net.IPNet, flow Flow, algorithms flowAlgorithms) []kernelState {
	return []kernelState{
		{
			source: localNet.IP, destination: remoteNet.IP,
			selectorSource: localNet, selectorDestination: remoteNet,
			sourcePort: flow.LocalPort, destinationPort: flow.RemotePort,
			spi: flow.OutboundSPI, auth: algorithms.auth, crypt: algorithms.crypt,
		},
		{
			source: remoteNet.IP, destination: localNet.IP,
			selectorSource: remoteNet, selectorDestination: localNet,
			sourcePort: flow.RemotePort, destinationPort: flow.LocalPort,
			spi: flow.InboundSPI, auth: algorithms.auth, crypt: algorithms.crypt,
		},
	}
}

func flowPolicies(localNet, remoteNet *net.IPNet, flow Flow) []kernelPolicy {
	return []kernelPolicy{
		{
			source: localNet, destination: remoteNet, templateSource: localNet.IP, templateDestination: remoteNet.IP,
			sourcePort: flow.LocalPort, destinationPort: flow.RemotePort, spi: flow.OutboundSPI,
		},
		{
			source: remoteNet, destination: localNet, templateSource: remoteNet.IP, templateDestination: localNet.IP,
			sourcePort: flow.RemotePort, destinationPort: flow.LocalPort, spi: flow.InboundSPI, inbound: true,
		},
	}
}

func installKernelPlan(operations kernelOperations, plan kernelPlan) (func() error, error) {
	deleteStates(operations, plan.states)
	for index, state := range plan.states {
		if err := operations.addState(state); err != nil {
			deleteStates(operations, plan.states[:index])
			return nil, err
		}
	}
	deletePolicies(operations, plan.policies)
	for index, policy := range plan.policies {
		if err := operations.addPolicy(policy); err != nil {
			deletePolicies(operations, plan.policies[:index])
			deleteStates(operations, plan.states)
			return nil, err
		}
	}
	return func() error {
		deletePolicies(operations, plan.policies)
		deleteStates(operations, plan.states)
		return nil
	}, nil
}

func deleteStates(operations kernelOperations, states []kernelState) {
	for _, state := range states {
		_ = operations.deleteState(state)
	}
}

func deletePolicies(operations kernelOperations, policies []kernelPolicy) {
	for _, policy := range policies {
		_ = operations.deletePolicy(policy)
	}
}
