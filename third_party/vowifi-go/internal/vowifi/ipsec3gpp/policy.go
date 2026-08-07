package ipsec3gpp

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	AuthHMACSHA196 = "hmac-sha-1-96"
	EncryptionAES  = "aes-cbc"
	Encryption3DES = "des-ede3-cbc"
	EncryptionNull = "null"
	ProtocolESP    = "esp"
	ModeTransport  = "trans"
)

// Policy describes the four unidirectional SAs negotiated by IMS sec-agree.
// Client/Server names are relative to LocalIP (the UE).
type Policy struct {
	LocalIP, RemoteIP                  net.IP
	LocalClientPort, LocalServerPort   uint16
	RemoteClientPort, RemoteServerPort uint16
	LocalClientSPI, LocalServerSPI     uint32
	RemoteClientSPI, RemoteServerSPI   uint32
	Authentication, Encryption         string
	Protocol, Mode                     string
	CK, IK                             []byte
}

func NewPolicy(policy Policy) (Policy, error) {
	policy = clonePolicy(policy)
	policy.Authentication = normalize(policy.Authentication, AuthHMACSHA196)
	policy.Encryption = normalize(policy.Encryption, EncryptionNull)
	policy.Protocol = normalize(policy.Protocol, ProtocolESP)
	policy.Mode = normalize(policy.Mode, ModeTransport)
	if err := policy.validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func clonePolicy(policy Policy) Policy {
	policy.LocalIP = append(net.IP(nil), policy.LocalIP...)
	policy.RemoteIP = append(net.IP(nil), policy.RemoteIP...)
	policy.CK = append([]byte(nil), policy.CK...)
	policy.IK = append([]byte(nil), policy.IK...)
	return policy
}

func normalize(value, defaultValue string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return defaultValue
	}
	return value
}

func (p Policy) validate() error {
	if p.LocalIP.To4() == nil || p.RemoteIP.To4() == nil {
		return errors.New("ipsec3gpp: transport mode requires IPv4 endpoints")
	}
	if p.Protocol != ProtocolESP || p.Mode != ModeTransport {
		return fmt.Errorf("ipsec3gpp: unsupported protocol/mode %s/%s", p.Protocol, p.Mode)
	}
	if p.Authentication != AuthHMACSHA196 {
		return fmt.Errorf("ipsec3gpp: unsupported authentication %q", p.Authentication)
	}
	if len(p.IK) < 16 {
		return errors.New("ipsec3gpp: IK must contain at least 16 bytes")
	}
	if err := validateEncryption(p.Encryption, p.CK); err != nil {
		return err
	}
	ports := []uint16{p.LocalClientPort, p.LocalServerPort, p.RemoteClientPort, p.RemoteServerPort}
	for _, port := range ports {
		if port == 0 {
			return errors.New("ipsec3gpp: protected ports must be non-zero")
		}
	}
	if p.LocalClientPort == p.LocalServerPort || p.RemoteClientPort == p.RemoteServerPort {
		return errors.New("ipsec3gpp: client and server ports must differ")
	}
	spis := []uint32{p.LocalClientSPI, p.LocalServerSPI, p.RemoteClientSPI, p.RemoteServerSPI}
	seen := make(map[uint32]struct{}, len(spis))
	for _, spi := range spis {
		if spi == 0 {
			return errors.New("ipsec3gpp: SPIs must be non-zero")
		}
		if _, exists := seen[spi]; exists {
			return errors.New("ipsec3gpp: SPIs must be unique")
		}
		seen[spi] = struct{}{}
	}
	return nil
}

func validateEncryption(algorithm string, ck []byte) error {
	switch algorithm {
	case EncryptionAES, Encryption3DES:
		if len(ck) < 16 {
			return errors.New("ipsec3gpp: CK must contain at least 16 bytes")
		}
		return nil
	case EncryptionNull:
		return nil
	default:
		return fmt.Errorf("ipsec3gpp: unsupported encryption %q", algorithm)
	}
}
