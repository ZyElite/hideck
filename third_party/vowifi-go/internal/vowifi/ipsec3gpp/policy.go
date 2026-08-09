package ipsec3gpp

import (
	"errors"
	"net"
)

const (
	AuthHMACSHA196 = "hmac-sha-1-96"
	EncryptionAES  = "aes-cbc"
	Encryption3DES = "des-ede3-cbc"
	EncryptionNull = "null"
	ProtocolESP    = "esp"
	ModeTransport  = "trans"
)

type Flow struct {
	OutboundSPI uint32
	InboundSPI  uint32
	LocalPort   int
	RemotePort  int
	AuthAlg     string
	EncAlg      string
	CK          []byte
	IK          []byte
}

type Policy struct {
	LocalIP     net.IP
	RemoteIP    net.IP
	LocalPortC  int
	LocalPortS  int
	RemotePortC int
	RemotePortS int
	FlowC       Flow
	FlowS       Flow

	// These fields preserve the policy shape introduced by the reconstructed
	// tree. NewPolicy projects it into FlowC/FlowS when the original fields are
	// absent.
	LocalClientPort, LocalServerPort   uint16
	RemoteClientPort, RemoteServerPort uint16
	LocalClientSPI, LocalServerSPI     uint32
	RemoteClientSPI, RemoteServerSPI   uint32
	Authentication, Encryption         string
	Protocol, Mode                     string
	CK, IK                             []byte
}

type SecurityMechanism struct {
	Alg   string
	EAlg  string
	Prot  string
	Mode  string
	SPIc  uint32
	SPIs  uint32
	PortC int
	PortS int
	Raw   string
}

func NewPolicy(policy Policy) (Policy, error) {
	if len(policy.LocalIP) == 0 || len(policy.RemoteIP) == 0 {
		return Policy{}, errors.New("ipsec3gpp: local/remote IP must not be nil")
	}
	projectCompatibilityPolicy(&policy)
	return clonePolicy(policy), nil
}

func projectCompatibilityPolicy(policy *Policy) {
	if policy.LocalPortC == 0 && policy.LocalPortS == 0 && policy.RemotePortC == 0 && policy.RemotePortS == 0 &&
		(policy.LocalClientPort != 0 || policy.LocalServerPort != 0 || policy.RemoteClientPort != 0 || policy.RemoteServerPort != 0) {
		policy.LocalPortC = int(policy.LocalClientPort)
		policy.LocalPortS = int(policy.LocalServerPort)
		policy.RemotePortC = int(policy.RemoteClientPort)
		policy.RemotePortS = int(policy.RemoteServerPort)
	}
	if flowConfigured(policy.FlowC) || flowConfigured(policy.FlowS) {
		return
	}
	policy.FlowC = compatibilityFlow(
		policy.RemoteServerSPI, policy.LocalClientSPI,
		policy.LocalClientPort, policy.RemoteServerPort, policy,
	)
	policy.FlowS = compatibilityFlow(
		policy.RemoteClientSPI, policy.LocalServerSPI,
		policy.LocalServerPort, policy.RemoteClientPort, policy,
	)
}

func flowConfigured(flow Flow) bool {
	return flow.OutboundSPI != 0 || flow.InboundSPI != 0 || flow.LocalPort != 0 || flow.RemotePort != 0 ||
		flow.AuthAlg != "" || flow.EncAlg != "" || len(flow.CK) != 0 || len(flow.IK) != 0
}

func compatibilityFlow(outboundSPI, inboundSPI uint32, localPort, remotePort uint16, policy *Policy) Flow {
	return Flow{
		OutboundSPI: outboundSPI, InboundSPI: inboundSPI,
		LocalPort: int(localPort), RemotePort: int(remotePort),
		AuthAlg: policy.Authentication, EncAlg: policy.Encryption,
		CK: policy.CK, IK: policy.IK,
	}
}

func clonePolicy(policy Policy) Policy {
	policy.LocalIP = append(net.IP(nil), policy.LocalIP...)
	policy.RemoteIP = append(net.IP(nil), policy.RemoteIP...)
	policy.FlowC = cloneFlow(policy.FlowC)
	policy.FlowS = cloneFlow(policy.FlowS)
	policy.CK = append([]byte(nil), policy.CK...)
	policy.IK = append([]byte(nil), policy.IK...)
	return policy
}

func cloneFlow(flow Flow) Flow {
	flow.CK = append([]byte(nil), flow.CK...)
	flow.IK = append([]byte(nil), flow.IK...)
	return flow
}
